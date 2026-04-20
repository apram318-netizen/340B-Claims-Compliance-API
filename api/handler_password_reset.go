package main

import (
	"claims-system/internal/database"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// POST /v1/logout
// Adds the current token's JTI to the Redis blocklist so it cannot be reused
// before its natural expiry. No-ops gracefully if Redis is not configured.
func (apiCfg *apiConfig) handlerLogout(w http.ResponseWriter, r *http.Request) {
	jti, _ := r.Context().Value(jtiKey).(string)
	exp, _ := r.Context().Value(tokenExpKey).(time.Time)

	if jti != "" && apiCfg.Redis != nil {
		ttl := time.Until(exp)
		if ttl > 0 {
			if err := apiCfg.Redis.Set(r.Context(), "blocklist:"+jti, "1", ttl).Err(); err != nil {
				slog.Error("failed to blocklist token", "error", err)
				// Fail open — still report success to the client
			}
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/password-reset/request
// Generates a reset token and stores its SHA-256 hash.  In production the raw
// token would be delivered via email; here we return it directly so the flow
// can be exercised end-to-end without an email service.
func (apiCfg *apiConfig) handlerPasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Email string `json:"email"`
	}
	var req request
	if issues := decodeJSONStrict(r, &req); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "email", Message: "is required"},
		})
		return
	}

	genericOK := map[string]string{"message": "if the email is registered, a reset link has been issued"}

	if apiCfg.Limiter != nil {
		if !apiCfg.Limiter.Allow(r.Context(), "pwreset:ip:"+clientIP(r)) {
			respondWithJSON(w, http.StatusOK, genericOK)
			return
		}
		if !apiCfg.Limiter.Allow(r.Context(), "pwreset:email:"+req.Email) {
			respondWithJSON(w, http.StatusOK, genericOK)
			return
		}
	}

	user, err := apiCfg.DB.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		// Always respond 200 to avoid user enumeration
		respondWithJSON(w, http.StatusOK, genericOK)
		return
	}

	// Generate 32 cryptographically random bytes as the raw token.
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		slog.Error("generate reset token failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	rawToken := hex.EncodeToString(rawBytes)
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	_, err = apiCfg.DB.CreatePasswordResetToken(r.Context(), database.CreatePasswordResetTokenParams{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(30 * time.Minute),
	})
	if err != nil {
		slog.Error("create password reset token failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	apiCfg.recordAuditEventBestEffort(r, "password_reset_requested", "user", user.ID.String(), user.ID, map[string]string{
		"ip": clientIP(r),
	})
	appMetrics.passwordResetRequested.Add(1)

	out := map[string]string{
		"message": "if the email is registered, a reset link has been issued",
	}
	if passwordResetExposeToken() {
		out["token"] = rawToken
		respondWithJSON(w, http.StatusOK, out)
		return
	}

	if apiCfg.Mailer != nil {
		base := strings.TrimRight(strings.TrimSpace(os.Getenv("PASSWORD_RESET_PUBLIC_BASE_URL")), "/")
		if base == "" {
			port := os.Getenv("PORT")
			if port == "" {
				port = "8080"
			}
			base = "http://localhost:" + port
		}
		subject := "Password reset"
		body := fmt.Sprintf(
			"You requested a password reset. Use this token within 30 minutes:\n\n%s\n\nConfirm with POST %s/v1/password-reset/confirm and JSON {\"token\":\"...\",\"password\":\"...\"}.\n",
			rawToken, base,
		)
		sendCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := apiCfg.Mailer.SendPasswordReset(sendCtx, user.Email, subject, body); err != nil {
			slog.Error("password reset email failed", "error", err, "user_id", user.ID)
			appMetrics.passwordResetEmailFailed.Add(1)
		} else {
			appMetrics.passwordResetEmailed.Add(1)
		}
	}

	respondWithJSON(w, http.StatusOK, out)
}

// passwordResetExposeToken is true when PASSWORD_RESET_EXPOSE_TOKEN is true/1/yes (dev and tests).
// Default is false so production never returns the raw token in the API body.
func passwordResetExposeToken() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PASSWORD_RESET_EXPOSE_TOKEN")))
	switch v {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

// POST /v1/password-reset/confirm
func (apiCfg *apiConfig) handlerPasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	var req request
	if issues := decodeJSONStrict(r, &req); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	var issues []ValidationIssue
	if req.Token == "" {
		issues = append(issues, ValidationIssue{Field: "token", Message: "is required"})
	}
	if len(req.Password) < 8 {
		issues = append(issues, ValidationIssue{Field: "password", Message: "must be at least 8 characters"})
	} else if len(req.Password) > 256 {
		issues = append(issues, ValidationIssue{Field: "password", Message: "must be 256 characters or fewer"})
	} else if !passwordMeetsComplexity(req.Password) {
		issues = append(issues, ValidationIssue{Field: "password", Message: "must contain at least one uppercase letter, one digit, and one special character"})
	}
	if len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	hash := sha256.Sum256([]byte(req.Token))
	tokenHash := hex.EncodeToString(hash[:])

	record, err := apiCfg.DB.GetPasswordResetToken(r.Context(), tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondWithError(w, http.StatusUnprocessableEntity, "invalid or expired token")
			return
		}
		slog.Error("get password reset token failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	hashBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("bcrypt failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	newHash := string(hashBytes)

	if err := apiCfg.DB.UpdateUserPassword(r.Context(), database.UpdateUserPasswordParams{
		ID:           record.UserID,
		PasswordHash: newHash,
	}); err != nil {
		slog.Error("update user password failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := apiCfg.DB.MarkPasswordResetTokenUsed(r.Context(), record.ID); err != nil {
		slog.Error("mark token used failed", "error", err)
		// Non-fatal; password was already changed
	}

	apiCfg.recordAuditEventBestEffort(r, "password_reset_completed", "user", record.UserID.String(), record.UserID, map[string]string{
		"ip": clientIP(r),
	})

	respondWithJSON(w, http.StatusOK, map[string]string{"message": "password updated successfully"})
}
