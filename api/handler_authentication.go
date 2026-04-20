package main

import (
	"claims-system/internal/database"
	"claims-system/internal/envx"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

func (apiCfg *apiConfig) handlerRegister(w http.ResponseWriter, r *http.Request) {
	type requestBody struct {
		OrgID    string `json:"org_id"`
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}

	var body requestBody
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	// Input validation
	body.Email = strings.TrimSpace(strings.ToLower(body.Email))
	body.Name = strings.TrimSpace(body.Name)

	if body.Email == "" || !strings.Contains(body.Email, "@") {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "email", Message: "must be a valid email address"},
		})
		return
	}
	if len(body.Email) > 255 {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "email", Message: "must be 255 characters or fewer"},
		})
		return
	}
	if body.Name == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "name", Message: "is required"},
		})
		return
	}
	if len(body.Name) > 200 {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "name", Message: "must be 200 characters or fewer"},
		})
		return
	}
	if len(body.Password) < 8 {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "password", Message: "must be at least 8 characters"},
		})
		return
	}
	if len(body.Password) > 256 {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "password", Message: "must be 256 characters or fewer"},
		})
		return
	}
	if !passwordMeetsComplexity(body.Password) {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "password", Message: "must contain at least one uppercase letter, one digit, and one special character"},
		})
		return
	}
	if body.OrgID == "" {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "org_id", Message: "is required"},
		})
		return
	}

	orgID, err := uuid.Parse(body.OrgID)
	if err != nil {
		respondWithValidationIssues(w, r, "invalid request body", []ValidationIssue{
			{Field: "org_id", Message: "must be a valid UUID"},
		})
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("bcrypt failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	user, err := apiCfg.DB.CreateUser(r.Context(), database.CreateUserParams{
		OrgID:        orgID,
		Email:        body.Email,
		Name:         body.Name,
		Role:         "viewer",
		PasswordHash: string(passwordHash),
	})
	if err != nil {
		// Unique constraint on email
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			respondWithError(w, http.StatusConflict, "an account with this email already exists")
			return
		}
		slog.Error("create user failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	respondWithJSON(w, http.StatusCreated, databaseUserToUser(user))
}

func (apiCfg *apiConfig) handlerLogin(w http.ResponseWriter, r *http.Request) {
	type requestBody struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		TotpCode   string `json:"totp_code,omitempty"`
		BackupCode string `json:"backup_code,omitempty"`
	}

	var body requestBody
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	if body.Email == "" || body.Password == "" {
		issues := make([]ValidationIssue, 0, 2)
		if body.Email == "" {
			issues = append(issues, ValidationIssue{Field: "email", Message: "is required"})
		}
		if body.Password == "" {
			issues = append(issues, ValidationIssue{Field: "password", Message: "is required"})
		}
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	user, err := apiCfg.DB.GetUserByEmail(r.Context(), strings.ToLower(strings.TrimSpace(body.Email)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Constant-time response to avoid user-enumeration via timing
			bcrypt.CompareHashAndPassword([]byte("$2a$10$placeholder"), []byte(body.Password))
		}
		respondWithError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !user.Active {
		respondWithError(w, http.StatusForbidden, "account disabled")
		return
	}

	// Account lockout check
	if user.LockedUntil.Valid && user.LockedUntil.Time.After(time.Now()) {
		respondWithError(w, http.StatusTooManyRequests, "account temporarily locked due to too many failed attempts")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)); err != nil {
		// Increment failed attempts; lock after 10 failures for 15 minutes
		if apiCfg.Pool != nil {
			if _, dbErr := apiCfg.Pool.Exec(r.Context(),
				`UPDATE users SET failed_login_attempts = failed_login_attempts + 1,
				 locked_until = CASE WHEN failed_login_attempts + 1 >= 10
				   THEN NOW() + INTERVAL '15 minutes' ELSE locked_until END
				 WHERE id = $1`, user.ID,
			); dbErr != nil {
				slog.Error("failed to update login attempts", "error", dbErr)
			}
		}
		apiCfg.recordAuditEventBestEffort(r, "login_failure", "user", user.ID.String(), user.ID, map[string]string{
			"ip": clientIP(r),
		})
		respondWithError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if envx.RequireMFAForAdmin() && isAdminRole(user.Role) && !user.MfaEnabled {
		respondWithJSON(w, http.StatusForbidden, map[string]string{
			"error": "multi-factor authentication must be enabled for this account",
			"code":  "MFA_SETUP_REQUIRED",
		})
		return
	}

	// MFA: require TOTP or a one-time backup code when enabled
	if user.MfaEnabled && user.MfaSecret.Valid {
		totpCode := strings.TrimSpace(body.TotpCode)
		backupCode := strings.TrimSpace(body.BackupCode)
		if totpCode == "" && backupCode == "" {
			respondWithJSON(w, http.StatusForbidden, map[string]string{
				"error": "multi-factor authentication code required",
				"code":  "MFA_REQUIRED",
			})
			return
		}
		if totpCode != "" {
			if !totp.Validate(totpCode, user.MfaSecret.String) {
				appMetrics.mfaFailures.Add(1)
				apiCfg.recordAuditEventBestEffort(r, "login_failure", "user", user.ID.String(), user.ID, map[string]string{
					"ip": clientIP(r), "reason": "invalid_totp",
				})
				respondWithError(w, http.StatusUnauthorized, "invalid authenticator code")
				return
			}
		} else {
			if !apiCfg.tryConsumeBackupCode(r.Context(), user.ID, backupCode) {
				appMetrics.mfaFailures.Add(1)
				apiCfg.recordAuditEventBestEffort(r, "login_failure", "user", user.ID.String(), user.ID, map[string]string{
					"ip": clientIP(r), "reason": "invalid_backup_code",
				})
				respondWithError(w, http.StatusUnauthorized, "invalid backup code")
				return
			}
			apiCfg.recordAuditEventBestEffort(r, "mfa_backup_used", "user", user.ID.String(), user.ID, map[string]string{
				"ip": clientIP(r),
			})
		}
	}

	// Successful login — reset counter
	if apiCfg.Pool != nil {
		if _, dbErr := apiCfg.Pool.Exec(r.Context(),
			`UPDATE users SET failed_login_attempts = 0, locked_until = NULL WHERE id = $1`, user.ID,
		); dbErr != nil {
			slog.Error("failed to reset login attempts", "error", dbErr)
		}
	}

	token, err := createJWT(user.ID, apiCfg.JwtSecret)
	if err != nil {
		slog.Error("jwt creation failed", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}

	type loginResponse struct {
		Token string `json:"token"`
		User  User   `json:"user"`
	}

	apiCfg.recordAuditEventBestEffort(r, "login_success", "user", user.ID.String(), user.ID, map[string]string{
		"ip": clientIP(r),
	})
	respondWithJSON(w, http.StatusOK, loginResponse{
		Token: token,
		User:  databaseUserToUser(user),
	})
}

// passwordMeetsComplexity requires at least one uppercase letter, one digit,
// and one non-letter/non-digit (special) character.
func passwordMeetsComplexity(p string) bool {
	var hasUpper, hasDigit, hasSpecial bool
	for _, c := range p {
		switch {
		case unicode.IsUpper(c):
			hasUpper = true
		case unicode.IsDigit(c):
			hasDigit = true
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			hasSpecial = true
		}
	}
	return hasUpper && hasDigit && hasSpecial
}
