package main

import (
	"claims-system/internal/database"
	"claims-system/internal/email"
	"claims-system/internal/envx"
	"claims-system/internal/feature"
	"claims-system/internal/ratelimit"
	"claims-system/internal/storage"
	"claims-system/internal/telemetry"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// PasswordResetMailer sends out-of-band password reset messages (SMTP).
type PasswordResetMailer interface {
	SendPasswordReset(ctx context.Context, to, subject, body string) error
}

type apiConfig struct {
	DB                database.Store
	Pool              *pgxpool.Pool
	JwtSecret         string
	JwtSecretPrevious string // optional: verify tokens signed with previous secret during rotation
	Queue             QueuePublisher
	Redis             *redis.Client
	Storage           storage.ExportStorage
	Limiter           ratelimit.Limiter
	Mailer            PasswordResetMailer // optional; required in production unless PASSWORD_RESET_EXPOSE_TOKEN
	Features          *feature.Resolver
	shutdown          context.CancelFunc // cancels background workers
}

func main() {
	loadEnv()
	validateAPIStartup()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	shutdownOTel := telemetry.Init(context.Background())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdownOTel(ctx); err != nil {
			slog.Error("otel shutdown", "error", err)
		}
	}()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	pool := connectDB()
	defer pool.Close()

	queueConn, queueCh := connectQueue()
	defer queueConn.Close()
	defer queueCh.Close()

	db := database.New(pool)

	redisClient := connectRedis()

	var limiter ratelimit.Limiter
	if redisClient != nil {
		limiter = ratelimit.NewRedisLimiter(redisClient, 10, time.Minute)
	} else {
		limiter = ratelimit.NewInMemoryLimiter(10, time.Minute)
	}

	var mailer PasswordResetMailer
	if smtpCfg := email.ConfigFromEnv(); smtpCfg != nil {
		mailer = email.NewSMTP(smtpCfg)
	}

	var exportStorage storage.ExportStorage
	if bucket := os.Getenv("S3_EXPORT_BUCKET"); bucket != "" {
		s3Store, err := storage.NewS3Storage(context.Background(), bucket, os.Getenv("S3_EXPORT_PREFIX"))
		if err != nil {
			slog.Error("failed to init S3 storage", "error", err)
			os.Exit(1)
		}
		exportStorage = s3Store
	} else {
		exportDir := os.Getenv("EXPORT_DIR")
		if exportDir == "" {
			exportDir = "./exports"
		}
		ls, err := storage.NewLocalStorage(exportDir)
		if err != nil {
			slog.Error("failed to init local export storage", "error", err)
			os.Exit(1)
		}
		exportStorage = ls
	}

	bgCtx, bgCancel := context.WithCancel(context.Background())
	apiCfg := apiConfig{
		DB:                db,
		Pool:              pool,
		JwtSecret:         getJWTSecret(),
		JwtSecretPrevious: strings.TrimSpace(os.Getenv("JWT_SECRET_PREVIOUS")),
		Queue:             queueCh,
		Redis:             redisClient,
		Storage:           exportStorage,
		Limiter:           limiter,
		Mailer:            mailer,
		Features:          feature.NewResolver(db),
		shutdown:          bgCancel,
	}

	go apiCfg.runWebhookDeliveryWorker(bgCtx)

	slog.Info("api initialized", "port", port, "smtp_configured", mailer != nil, "production", envx.IsProduction())

	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(otelhttp.NewMiddleware("claims-system-api",
		otelhttp.WithFilter(func(r *http.Request) bool {
			return r.URL.Path != "/metrics"
		}),
	))
	router.Use(middlewareTrace)
	router.Use(middleware.RealIP)
	router.Use(securityHeadersMiddleware)
	router.Use(requestLogger)
	router.Use(middleware.Recoverer)
	router.Use(corsMiddleware)
	router.Use(middleware.RequestSize(5 * 1024 * 1024)) // 5 MB max

	router.Get("/health", apiCfg.handlerHealth)
	router.Get("/metrics", handlerMetrics)
	router.Get("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		respondWithJSON(w, http.StatusOK, map[string]any{"version": "1", "service": "claims-system-api"})
	})

	router.Get("/v1/auth/oidc/login", apiCfg.handlerOIDCLogin)
	router.Get("/v1/auth/oidc/callback", apiCfg.handlerOIDCCallback)
	router.Get("/v1/auth/saml/login", apiCfg.handlerSAMLLogin)
	router.Post("/v1/auth/saml/acs", apiCfg.handlerSAMLACS)
	router.Get("/v1/auth/saml/metadata", apiCfg.handlerSAMLMetadata)

	router.Route("/scim/v2", func(r chi.Router) {
		r.Use(apiCfg.middlewareSCIM)
		r.Get("/ServiceProviderConfig", apiCfg.handlerSCIMServiceProviderConfig)
		r.Get("/ResourceTypes", apiCfg.handlerSCIMResourceTypes)
		r.Get("/Schemas", apiCfg.handlerSCIMSchemas)
		r.Get("/Schemas/{id}", apiCfg.handlerSCIMSchemaByID)
		r.Get("/Users", apiCfg.handlerSCIMListUsers)
		r.Post("/Users", apiCfg.handlerSCIMCreateUser)
		r.Get("/Users/{id}", apiCfg.handlerSCIMGetUser)
		r.Patch("/Users/{id}", apiCfg.handlerSCIMPatchUser)
		r.Delete("/Users/{id}", apiCfg.handlerSCIMDeleteUser)
	})

	router.Post("/v1/register", apiCfg.handlerRegister)
	router.With(newLoginRateLimiter(apiCfg.Limiter)).Post("/v1/login", apiCfg.handlerLogin)
	router.Post("/v1/password-reset/request", apiCfg.handlerPasswordResetRequest)
	router.Post("/v1/password-reset/confirm", apiCfg.handlerPasswordResetConfirm)

	router.Group(func(r chi.Router) {
		r.Use(apiCfg.middlewareAuth)

		// Organizations
		r.Get("/v1/organizations/{id}", apiCfg.handlerGetOrganization)
		r.With(apiCfg.middlewareRequireAdmin, apiCfg.middlewareRequireMFAStepUp).Post("/v1/organizations", apiCfg.handlerCreateOrganization)
		r.Get("/v1/organizations/{id}/settings", apiCfg.handlerGetOrganizationSettings)
		r.Patch("/v1/organizations/{id}/settings", apiCfg.handlerPatchOrganizationSettings)
		r.Get("/v1/organizations/{id}/users", apiCfg.handlerListOrgUsers)
		r.Patch("/v1/organizations/{id}/users/{userId}", apiCfg.handlerPatchOrgUser)
		r.Get("/v1/organizations/{id}/feature-flags", apiCfg.handlerListOrgFeatureFlags)
		r.Put("/v1/organizations/{id}/feature-flags/{key}", apiCfg.handlerPutOrgFeatureFlag)
		r.Get("/v1/organizations/{id}/sso", apiCfg.handlerGetOrgSSO)
		r.Put("/v1/organizations/{id}/sso", apiCfg.handlerPutOrgSSO)
		r.Post("/v1/organizations/{id}/webhooks", apiCfg.handlerCreateWebhook)
		r.Get("/v1/organizations/{id}/webhooks", apiCfg.handlerListWebhooks)
		r.Patch("/v1/organizations/{id}/webhooks/{webhookId}", apiCfg.handlerPatchWebhook)
		r.Delete("/v1/organizations/{id}/webhooks/{webhookId}", apiCfg.handlerDeleteWebhook)
		r.Get("/v1/organizations/{id}/webhooks/{webhookId}/deliveries", apiCfg.handlerListWebhookDeliveries)
		r.Post("/v1/organizations/{id}/cases", apiCfg.handlerCreateCase)
		r.Get("/v1/organizations/{id}/cases", apiCfg.handlerListCases)
		r.Get("/v1/organizations/{id}/cases/{caseId}", apiCfg.handlerGetExceptionCase)
		r.Patch("/v1/organizations/{id}/cases/{caseId}", apiCfg.handlerPatchCase)
		r.Post("/v1/organizations/{id}/cases/{caseId}/comments", apiCfg.handlerAddCaseComment)
		r.Get("/v1/organizations/{id}/cases/{caseId}/comments", apiCfg.handlerListCaseComments)

		// Upload batches + claims
		r.Post("/v1/batches", apiCfg.handlerCreateBatch)
		r.Post("/v1/batches/upload", apiCfg.handlerUploadBatch)
		r.Get("/v1/batches/{id}", apiCfg.handlerGetBatch)
		r.Get("/v1/batches/{id}/claims", apiCfg.handlerGetBatchClaims)
		r.Get("/v1/batches/{id}/reconciliation-job", apiCfg.handlerGetBatchReconciliationJob)
		r.Get("/v1/claims/{id}", apiCfg.handlerGetClaim)
		r.Get("/v1/claims/{id}/decision", apiCfg.handlerGetClaimDecision)

		// Manufacturers + products
		r.Get("/v1/manufacturers", apiCfg.handlerListManufacturers)
		r.Get("/v1/manufacturers/{id}", apiCfg.handlerGetManufacturer)
		r.Get("/v1/manufacturers/{id}/products", apiCfg.handlerListManufacturerProducts)
		r.With(apiCfg.middlewareRequireAdmin, apiCfg.middlewareRequireMFAStepUp).Post("/v1/manufacturers", apiCfg.handlerCreateManufacturer)
		r.With(apiCfg.middlewareRequireAdmin, apiCfg.middlewareRequireMFAStepUp).Post("/v1/manufacturers/{id}/products", apiCfg.handlerCreateManufacturerProduct)

		// Policies
		r.Get("/v1/manufacturers/{id}/policies", apiCfg.handlerListPolicies)
		r.Get("/v1/policy-versions/{id}/rules", apiCfg.handlerListPolicyRules)
		r.With(apiCfg.middlewareRequireAdmin, apiCfg.middlewareRequireMFAStepUp).Post("/v1/manufacturers/{id}/policies", apiCfg.handlerCreatePolicy)
		r.With(apiCfg.middlewareRequireAdmin, apiCfg.middlewareRequireMFAStepUp).Post("/v1/policies/{id}/versions", apiCfg.handlerCreatePolicyVersion)
		r.With(apiCfg.middlewareRequireAdmin, apiCfg.middlewareRequireMFAStepUp).Post("/v1/policy-versions/{id}/rules", apiCfg.handlerCreatePolicyRule)

		// Rebate records
		r.With(apiCfg.middlewareRequireAdmin, apiCfg.middlewareRequireMFAStepUp).Post("/v1/rebate-records", apiCfg.handlerCreateRebateRecord)

		// Reconciliation
		r.Get("/v1/reconciliation-jobs/{id}", apiCfg.handlerGetReconciliationJob)
		r.Get("/v1/reconciliation-jobs/{id}/decisions", apiCfg.handlerGetJobDecisions)

		// Exports / reports
		r.Post("/v1/exports", apiCfg.handlerCreateExport)
		r.Get("/v1/exports/{id}", apiCfg.handlerGetExport)
		r.Get("/v1/exports/{id}/download", apiCfg.handlerDownloadExport)
		r.Post("/v1/exports/{id}/retry", apiCfg.handlerRetryExport)

		// Manual overrides
		r.Post("/v1/claims/{id}/override", apiCfg.handlerOverrideClaim)

		// Audit log
		r.Get("/v1/audit-events", apiCfg.handlerGetAuditEvents)

		// Auth management
		r.Post("/v1/logout", apiCfg.handlerLogout)
		r.Post("/v1/mfa/setup", apiCfg.handlerMFASetup)
		r.Post("/v1/mfa/enable", apiCfg.handlerMFAEnable)
		r.Post("/v1/mfa/disable", apiCfg.handlerMFADisable)
		r.Post("/v1/mfa/backup-codes/regenerate", apiCfg.handlerMFABackupCodesRegenerate)
		r.Post("/v1/mfa/step-up", apiCfg.handlerMFAStepUp)

		// Contract pharmacies
		r.Get("/v1/contract-pharmacies", apiCfg.handlerListContractPharmacies)
		r.Get("/v1/contract-pharmacies/{id}", apiCfg.handlerGetContractPharmacy)
		r.With(apiCfg.middlewareRequireAdmin, apiCfg.middlewareRequireMFAStepUp).Post("/v1/contract-pharmacies", apiCfg.handlerCreateContractPharmacy)
		r.With(apiCfg.middlewareRequireAdmin, apiCfg.middlewareRequireMFAStepUp).Patch("/v1/contract-pharmacies/{id}/status", apiCfg.handlerUpdateContractPharmacyStatus)
		r.With(apiCfg.middlewareRequireAdmin, apiCfg.middlewareRequireMFAStepUp).Post("/v1/manufacturers/{id}/contract-pharmacy-auths", apiCfg.handlerCreateContractPharmacyAuth)

		// Disputes
		r.Post("/v1/disputes", apiCfg.handlerCreateDispute)
		r.Get("/v1/disputes/{id}", apiCfg.handlerGetDispute)
		r.Get("/v1/claims/{id}/disputes", apiCfg.handlerListClaimDisputes)
		r.Post("/v1/disputes/{id}/submit", apiCfg.handlerSubmitDispute)
		r.With(apiCfg.middlewareRequireAdmin, apiCfg.middlewareRequireMFAStepUp).Post("/v1/disputes/{id}/resolve", apiCfg.handlerResolveDispute)
		r.Post("/v1/disputes/{id}/withdraw", apiCfg.handlerWithdrawDispute)
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%s", port),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down, draining in-flight requests")
	if apiCfg.shutdown != nil {
		apiCfg.shutdown()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("forced shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped cleanly")
}
