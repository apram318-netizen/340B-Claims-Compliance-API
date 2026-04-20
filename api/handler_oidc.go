package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"claims-system/internal/database"
	"claims-system/internal/feature"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/oauth2"
)

// GET /v1/auth/oidc/login?org_id=... — redirects to IdP (public).
func (apiCfg *apiConfig) handlerOIDCLogin(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("org_id")))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "org_id query required")
		return
	}
	if apiCfg.Features == nil || !apiCfg.Features.Enabled(r.Context(), orgID, feature.OIDCSSO) {
		respondWithError(w, http.StatusNotFound, "SSO not available")
		return
	}
	cfg, err := apiCfg.DB.GetOrgSSOConfig(r.Context(), orgID)
	if err != nil || !cfg.Enabled || !cfg.OidcIssuer.Valid || !cfg.OidcClientID.Valid {
		respondWithError(w, http.StatusBadRequest, "SSO not configured for organization")
		return
	}
	redirectURL := strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URL"))
	if redirectURL == "" {
		respondWithError(w, http.StatusInternalServerError, "OIDC_REDIRECT_URL is not configured")
		return
	}
	ctx := r.Context()
	provider, err := oidc.NewProvider(ctx, cfg.OidcIssuer.String)
	if err != nil {
		slog.Error("oidc provider", "error", err)
		respondWithError(w, http.StatusInternalServerError, "oidc configuration error")
		return
	}
	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.OidcClientID.String,
		ClientSecret: cfg.OidcClientSecret.String,
		RedirectURL:  redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}
	state, err := apiCfg.createOIDCStateToken(orgID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.Redirect(w, r, oauth2Cfg.AuthCodeURL(state), http.StatusFound)
}

// GET /v1/auth/oidc/callback?code=&state= — exchanges code, issues session JWT (public).
func (apiCfg *apiConfig) handlerOIDCCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		respondWithError(w, http.StatusBadRequest, "missing code or state")
		return
	}
	orgID, err := apiCfg.parseOIDCStateToken(state)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid state")
		return
	}
	cfg, err := apiCfg.DB.GetOrgSSOConfig(r.Context(), orgID)
	if err != nil || !cfg.Enabled {
		respondWithError(w, http.StatusBadRequest, "SSO not configured")
		return
	}
	redirectURL := strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URL"))
	if redirectURL == "" {
		respondWithError(w, http.StatusInternalServerError, "OIDC_REDIRECT_URL is not configured")
		return
	}
	ctx := r.Context()
	provider, err := oidc.NewProvider(ctx, cfg.OidcIssuer.String)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "oidc provider error")
		return
	}
	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.OidcClientID.String,
		ClientSecret: cfg.OidcClientSecret.String,
		RedirectURL:  redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}
	tok, err := oauth2Cfg.Exchange(ctx, code)
	if err != nil {
		slog.Error("oidc exchange", "error", err)
		respondWithError(w, http.StatusUnauthorized, "token exchange failed")
		return
	}
	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "id_token missing")
		return
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.OidcClientID.String})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		slog.Error("id token verify", "error", err)
		respondWithError(w, http.StatusUnauthorized, "invalid id token")
		return
	}
	var claims struct {
		Email string `json:"email"`
		Sub   string `json:"sub"`
	}
	if err := idToken.Claims(&claims); err != nil {
		respondWithError(w, http.StatusUnauthorized, "invalid claims")
		return
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		respondWithError(w, http.StatusUnauthorized, "email claim required")
		return
	}

	user, err := apiCfg.findOrCreateSSOUser(ctx, orgID, email)
	if err != nil {
		slog.Error("oidc user", "error", err)
		respondWithError(w, http.StatusForbidden, err.Error())
		return
	}
	if _, err := apiCfg.DB.InsertUserExternalIdentity(ctx, database.InsertUserExternalIdentityParams{
		UserID:   user.ID,
		Provider: "oidc",
		Issuer:   cfg.OidcIssuer.String,
		Subject:  claims.Sub,
		Email:    pgtype.Text{String: email, Valid: true},
	}); err != nil && !isPGUniqueViolation(err) {
		slog.Error("external identity", "error", err)
	}

	jwtStr, err := createJWT(user.ID, apiCfg.JwtSecret)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]any{
		"token": jwtStr,
		"user":  databaseUserToUser(user),
	})
}

func isPGUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

