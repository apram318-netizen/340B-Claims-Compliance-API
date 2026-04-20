package main

import (
	"claims-system/internal/database"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// POST /v1/mfa/setup — returns a new TOTP secret and otpauth URL (add to an authenticator app).
func (apiCfg *apiConfig) handlerMFASetup(w http.ResponseWriter, r *http.Request) {
	userID, err := getUserId(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := apiCfg.DB.GetUserByID(r.Context(), userID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "invalid user")
		return
	}
	if user.MfaEnabled {
		respondWithError(w, http.StatusConflict, "MFA is already enabled")
		return
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "claims-system",
		AccountName: user.Email,
		SecretSize:  20,
	})
	if err != nil {
		slog.Error("totp generate failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{
		"secret":      key.Secret(),
		"otpauth_url": key.URL(),
	})
}

// POST /v1/mfa/enable — verify a TOTP code against the enrollment secret and persist MFA.
func (apiCfg *apiConfig) handlerMFAEnable(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Secret string `json:"secret"`
		Code   string `json:"code"`
	}
	var req request
	if issues := decodeJSONStrict(r, &req); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	var issues []ValidationIssue
	if req.Secret == "" {
		issues = append(issues, ValidationIssue{Field: "secret", Message: "is required"})
	}
	if req.Code == "" {
		issues = append(issues, ValidationIssue{Field: "code", Message: "is required"})
	}
	if len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	userID, err := getUserId(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := apiCfg.DB.GetUserByID(r.Context(), userID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "invalid user")
		return
	}
	if user.MfaEnabled {
		respondWithError(w, http.StatusConflict, "MFA is already enabled")
		return
	}

	if !totp.Validate(req.Code, req.Secret) {
		respondWithError(w, http.StatusBadRequest, "invalid authenticator code")
		return
	}

	updated, err := apiCfg.DB.SetUserMFA(r.Context(), database.SetUserMFAParams{
		ID:        userID,
		MfaSecret: pgtype.Text{String: req.Secret, Valid: true},
	})
	if err != nil {
		slog.Error("set user MFA failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	codes, err := apiCfg.generateBackupCodes(r.Context(), userID)
	if err != nil {
		slog.Error("generate MFA backup codes failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	apiCfg.recordAuditEventBestEffort(r, "mfa_enabled", "user", userID.String(), userID, nil)
	type enableResp struct {
		User        User     `json:"user"`
		BackupCodes []string `json:"backup_codes"`
	}
	respondWithJSON(w, http.StatusOK, enableResp{
		User:        databaseUserToUser(updated),
		BackupCodes: codes,
	})
}

// POST /v1/mfa/disable — disable MFA after password confirmation.
func (apiCfg *apiConfig) handlerMFADisable(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Password string `json:"password"`
	}
	var req request
	if issues := decodeJSONStrict(r, &req); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	if req.Password == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "password", Message: "is required"},
		})
		return
	}

	userID, err := getUserId(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := apiCfg.DB.GetUserByID(r.Context(), userID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "invalid user")
		return
	}
	if !user.MfaEnabled {
		respondWithError(w, http.StatusConflict, "MFA is not enabled")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		respondWithError(w, http.StatusUnauthorized, "invalid password")
		return
	}

	if err := apiCfg.DB.DeleteMfaBackupCodesForUser(r.Context(), userID); err != nil {
		slog.Error("delete MFA backup codes failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	updated, err := apiCfg.DB.DisableUserMFA(r.Context(), userID)
	if err != nil {
		slog.Error("disable MFA failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	apiCfg.recordAuditEventBestEffort(r, "mfa_disabled", "user", userID.String(), userID, nil)
	respondWithJSON(w, http.StatusOK, databaseUserToUser(updated))
}

// POST /v1/mfa/backup-codes/regenerate — requires TOTP; returns new backup codes once.
func (apiCfg *apiConfig) handlerMFABackupCodesRegenerate(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Code string `json:"code"`
	}
	var req request
	if issues := decodeJSONStrict(r, &req); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "code", Message: "is required"},
		})
		return
	}

	userID, err := getUserId(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := apiCfg.DB.GetUserByID(r.Context(), userID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "invalid user")
		return
	}
	if !user.MfaEnabled || !user.MfaSecret.Valid {
		respondWithError(w, http.StatusConflict, "MFA is not enabled")
		return
	}
	if !totp.Validate(strings.TrimSpace(req.Code), user.MfaSecret.String) {
		appMetrics.mfaFailures.Add(1)
		respondWithError(w, http.StatusUnauthorized, "invalid authenticator code")
		return
	}

	codes, err := apiCfg.generateBackupCodes(r.Context(), userID)
	if err != nil {
		slog.Error("regenerate MFA backup codes failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	apiCfg.recordAuditEventBestEffort(r, "mfa_backup_codes_regenerated", "user", userID.String(), userID, nil)
	respondWithJSON(w, http.StatusOK, map[string]any{
		"backup_codes": codes,
	})
}

// POST /v1/mfa/step-up — verify TOTP or backup code; returns a short-lived token for X-MFA-Step-Up on sensitive admin routes.
func (apiCfg *apiConfig) handlerMFAStepUp(w http.ResponseWriter, r *http.Request) {
	type request struct {
		TotpCode   string `json:"totp_code,omitempty"`
		BackupCode string `json:"backup_code,omitempty"`
	}
	var req request
	if issues := decodeJSONStrict(r, &req); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	userID, err := getUserId(r)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := apiCfg.DB.GetUserByID(r.Context(), userID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "invalid user")
		return
	}
	if !user.MfaEnabled || !user.MfaSecret.Valid {
		respondWithError(w, http.StatusConflict, "MFA is not enabled")
		return
	}

	totpCode := strings.TrimSpace(req.TotpCode)
	backupCode := strings.TrimSpace(req.BackupCode)
	if totpCode == "" && backupCode == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "totp_code", Message: "totp_code or backup_code is required"},
		})
		return
	}

	ok := false
	if totpCode != "" {
		ok = totp.Validate(totpCode, user.MfaSecret.String)
	} else {
		ok = apiCfg.tryConsumeBackupCode(r.Context(), userID, backupCode)
	}
	if !ok {
		appMetrics.mfaFailures.Add(1)
		respondWithError(w, http.StatusUnauthorized, "invalid authenticator code")
		return
	}

	token, err := apiCfg.createStepUpToken(userID)
	if err != nil {
		slog.Error("step-up token failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	apiCfg.recordAuditEventBestEffort(r, "mfa_step_up", "user", userID.String(), userID, map[string]string{
		"ip": clientIP(r),
	})
	respondWithJSON(w, http.StatusOK, map[string]any{
		"step_up_token": token,
		"expires_in":    600,
	})
}
