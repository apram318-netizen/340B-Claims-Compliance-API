package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"claims-system/internal/envx"

	"github.com/google/uuid"
)

type contextKey string

const userIDKey contextKey = "userID"
const authUserKey contextKey = "authUser"
const jtiKey contextKey = "jti"
const tokenExpKey contextKey = "tokenExp"

type authUser struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	Role       string
	MfaEnabled bool
}

// middleware function
func (apiConfig *apiConfig) middlewareAuth(next http.Handler) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		authHeader := r.Header.Get("Authorization")

		if authHeader == "" {
			respondWithError(w, 401, "invalid authorization header provided")
			return
		}

		parts := strings.Split(authHeader, " ")

		if (len(parts) != 2) || parts[0] != "Bearer" {
			respondWithError(w, 401, "invalid authorization header format")
			return
		}

		tokenString := parts[1]

		claims, err := apiConfig.parseJWTClaims(tokenString)
		if err != nil {
			respondWithError(w, 401, "invalid or expired token")
			return
		}
		if claims["iss"] != tokenIssuer {
			respondWithError(w, 401, "invalid token issuer")
			return
		}
		if !hasAudience(claims["aud"], tokenAudience) {
			respondWithError(w, 401, "invalid token audience")
			return
		}

		userIDString, ok := claims["user_id"].(string)

		if !ok {
			respondWithError(w, 401, "invalid token claims")
			return
		}

		userId, err := uuid.Parse(userIDString)

		if err != nil {
			respondWithError(w, 401, "invalid user id format in token ")
			return
		}

		// Extract JTI for revocation support.
		jtiStr, _ := claims["jti"].(string)

		// Check Redis token blocklist when Redis is configured.
		if jtiStr != "" && apiConfig.Redis != nil {
			blocked, rErr := apiConfig.Redis.Exists(r.Context(), "blocklist:"+jtiStr).Result()
			if rErr == nil && blocked > 0 {
				respondWithError(w, 401, "token has been revoked")
				return
			}
		}

		// Extract expiry for use by the logout handler.
		var tokenExp time.Time
		if expVal, ok := claims["exp"].(float64); ok {
			tokenExp = time.Unix(int64(expVal), 0)
		}

		user, err := apiConfig.DB.GetUserByID(r.Context(), userId)
		if err != nil {
			respondWithError(w, 401, "invalid user")
			return
		}
		if !user.Active {
			respondWithError(w, 403, "account disabled")
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userId)
		ctx = context.WithValue(ctx, authUserKey, authUser{
			ID:         user.ID,
			OrgID:      user.OrgID,
			Role:       normalizeRole(user.Role),
			MfaEnabled: user.MfaEnabled,
		})
		ctx = context.WithValue(ctx, jtiKey, jtiStr)
		ctx = context.WithValue(ctx, tokenExpKey, tokenExp)

		next.ServeHTTP(w, r.WithContext(ctx))

	})
}

func getUserId(r *http.Request) (uuid.UUID, error) {
	userID, ok := r.Context().Value(userIDKey).(uuid.UUID)

	if !ok {
		return uuid.UUID{}, fmt.Errorf("no user id in context")
	}

	return userID, nil
}

func getAuthUser(r *http.Request) (authUser, error) {
	user, ok := r.Context().Value(authUserKey).(authUser)
	if !ok {
		return authUser{}, fmt.Errorf("no auth user in context")
	}
	return user, nil
}

func normalizeRole(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "", "viewer":
		return "member"
	default:
		return role
	}
}

func isAdminRole(role string) bool {
	return normalizeRole(role) == "admin"
}

func hasAudience(raw any, expected string) bool {
	switch v := raw.(type) {
	case string:
		return v == expected
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == expected {
				return true
			}
		}
	}
	return false
}

func (apiConfig *apiConfig) middlewareRequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := getAuthUser(r)
		if err != nil {
			respondWithError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !isAdminRole(user.Role) {
			respondWithError(w, http.StatusForbidden, "admin role required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// middlewareRequireMFAStepUp requires header X-MFA-Step-Up when REQUIRE_MFA_STEP_UP_FOR_ADMIN is set,
// the caller is an admin, and MFA is enabled on the account. Compose after middlewareRequireAdmin.
func (apiCfg *apiConfig) middlewareRequireMFAStepUp(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !envx.RequireMFAStepUpForAdmin() {
			next.ServeHTTP(w, r)
			return
		}
		au, err := getAuthUser(r)
		if err != nil {
			respondWithError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !isAdminRole(au.Role) || !au.MfaEnabled {
			next.ServeHTTP(w, r)
			return
		}
		raw := strings.TrimSpace(r.Header.Get("X-MFA-Step-Up"))
		if raw == "" {
			respondWithJSON(w, http.StatusForbidden, map[string]string{
				"error": "a fresh multi-factor step-up is required for this operation",
				"code":  "MFA_STEP_UP_REQUIRED",
			})
			return
		}
		if err := apiCfg.verifyStepUpToken(raw, au.ID); err != nil {
			respondWithError(w, http.StatusForbidden, "invalid or expired step-up token")
			return
		}
		next.ServeHTTP(w, r)
	})
}
