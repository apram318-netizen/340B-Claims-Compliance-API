package main

import (
	"claims-system/internal/database"
	"claims-system/internal/feature"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// GET /v1/organizations/{id}/sso — org admin; does not return secrets (masked).
func (apiCfg *apiConfig) handlerGetOrgSSO(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	if !requireOrgAdmin(w, r, orgID) {
		return
	}
	cfg, err := apiCfg.DB.GetOrgSSOConfig(r.Context(), orgID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("get org sso", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		respondWithJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	out := map[string]any{"enabled": cfg.Enabled}
	if cfg.OidcIssuer.Valid {
		out["oidc_issuer"] = cfg.OidcIssuer.String
	}
	if cfg.OidcClientID.Valid {
		out["oidc_client_id"] = cfg.OidcClientID.String
	}
	out["oidc_client_secret_set"] = cfg.OidcClientSecret.Valid && cfg.OidcClientSecret.String != ""
	if cfg.SamlIdpEntityID.Valid {
		out["saml_idp_entity_id"] = cfg.SamlIdpEntityID.String
	}
	if cfg.SamlIdpSsoUrl.Valid {
		out["saml_idp_sso_url"] = cfg.SamlIdpSsoUrl.String
	}
	out["saml_idp_cert_pem_set"] = cfg.SamlIdpCertPem.Valid && strings.TrimSpace(cfg.SamlIdpCertPem.String) != ""
	respondWithJSON(w, http.StatusOK, out)
}

// PUT /v1/organizations/{id}/sso — OIDC and/or SAML fields; enabled toggles SSO overall.
func (apiCfg *apiConfig) handlerPutOrgSSO(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	if !requireOrgAdmin(w, r, orgID) {
		return
	}
	if apiCfg.Features == nil {
		respondWithError(w, http.StatusInternalServerError, "features not configured")
		return
	}
	ctx := r.Context()
	oidcOn := apiCfg.Features.Enabled(ctx, orgID, feature.OIDCSSO)
	samlOn := apiCfg.Features.Enabled(ctx, orgID, feature.SAMLSSO)
	if !oidcOn && !samlOn {
		respondWithError(w, http.StatusNotFound, "SSO features not enabled for organization")
		return
	}

	var body struct {
		OidcIssuer       string `json:"oidc_issuer"`
		OidcClientID     string `json:"oidc_client_id"`
		OidcClientSecret string `json:"oidc_client_secret"`
		SamlIDPEntityID  string `json:"saml_idp_entity_id"`
		SamlIDPSSOURL    string `json:"saml_idp_sso_url"`
		SamlIDPCertPEM   string `json:"saml_idp_cert_pem"`
		Enabled          bool   `json:"enabled"`
	}
	if issues := decodeJSONStrict(r, &body); len(issues) > 0 {
		respondWithValidationIssues(w, r, "invalid request body", issues)
		return
	}

	existing, _ := apiCfg.DB.GetOrgSSOConfig(ctx, orgID)

	secret := body.OidcClientSecret
	if secret == "" && existing.OidcClientSecret.Valid {
		secret = existing.OidcClientSecret.String
	}
	samlCert := body.SamlIDPCertPEM
	if strings.TrimSpace(samlCert) == "" && existing.SamlIdpCertPem.Valid {
		samlCert = existing.SamlIdpCertPem.String
	}

	cfg, err := apiCfg.DB.UpsertOrgSSOConfig(ctx, database.UpsertOrgSSOConfigParams{
		OrgID:            orgID,
		OidcIssuer:       mergeText(body.OidcIssuer, existing.OidcIssuer),
		OidcClientID:     mergeText(body.OidcClientID, existing.OidcClientID),
		OidcClientSecret: pgtype.Text{String: secret, Valid: secret != ""},
		Enabled:          body.Enabled,
		SamlIdpEntityID:  mergeText(body.SamlIDPEntityID, existing.SamlIdpEntityID),
		SamlIdpSsoUrl:    mergeText(body.SamlIDPSSOURL, existing.SamlIdpSsoUrl),
		SamlIdpCertPem:   pgtype.Text{String: samlCert, Valid: strings.TrimSpace(samlCert) != ""},
	})
	if err != nil {
		slog.Error("upsert org sso", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	apiCfg.recordAuditEventBestEffort(r, "org_sso_updated", "organization", orgID.String(), mustUserID(r), map[string]string{"ip": clientIP(r)})

	out := map[string]any{"enabled": cfg.Enabled}
	if cfg.OidcIssuer.Valid {
		out["oidc_issuer"] = cfg.OidcIssuer.String
	}
	if cfg.OidcClientID.Valid {
		out["oidc_client_id"] = cfg.OidcClientID.String
	}
	out["oidc_client_secret_set"] = cfg.OidcClientSecret.Valid && cfg.OidcClientSecret.String != ""
	if cfg.SamlIdpEntityID.Valid {
		out["saml_idp_entity_id"] = cfg.SamlIdpEntityID.String
	}
	if cfg.SamlIdpSsoUrl.Valid {
		out["saml_idp_sso_url"] = cfg.SamlIdpSsoUrl.String
	}
	out["saml_idp_cert_pem_set"] = cfg.SamlIdpCertPem.Valid && strings.TrimSpace(cfg.SamlIdpCertPem.String) != ""
	respondWithJSON(w, http.StatusOK, out)
}

func mergeText(in string, existing pgtype.Text) pgtype.Text {
	s := strings.TrimSpace(in)
	if s != "" {
		return pgtype.Text{String: s, Valid: true}
	}
	return existing
}
