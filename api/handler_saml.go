package main

import (
	"claims-system/internal/database"
	"claims-system/internal/feature"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/crewjam/saml"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	samlMaterialOnce sync.Once
	samlPriv         *rsa.PrivateKey
	samlLeaf         *x509.Certificate
	samlMaterialErr  error
)

func loadSAMLSPKeys() (*rsa.PrivateKey, *x509.Certificate, error) {
	samlMaterialOnce.Do(func() {
		keyPath := strings.TrimSpace(os.Getenv("SAML_SP_KEY_FILE"))
		certPath := strings.TrimSpace(os.Getenv("SAML_SP_CERT_FILE"))
		if keyPath == "" || certPath == "" {
			samlMaterialErr = errors.New("SAML_SP_KEY_FILE and SAML_SP_CERT_FILE must be set for SAML")
			return
		}
		certPEM, err := os.ReadFile(certPath)
		if err != nil {
			samlMaterialErr = err
			return
		}
		keyPEM, err := os.ReadFile(keyPath)
		if err != nil {
			samlMaterialErr = err
			return
		}
		keyPair, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			samlMaterialErr = err
			return
		}
		samlLeaf, err = x509.ParseCertificate(keyPair.Certificate[0])
		if err != nil {
			samlMaterialErr = err
			return
		}
		priv, ok := keyPair.PrivateKey.(*rsa.PrivateKey)
		if !ok {
			samlMaterialErr = fmt.Errorf("SAML SP key must be RSA")
			return
		}
		samlPriv = priv
	})
	return samlPriv, samlLeaf, samlMaterialErr
}

func samlEntityID() string {
	if s := strings.TrimSpace(os.Getenv("SAML_SP_ENTITY_ID")); s != "" {
		return s
	}
	b := strings.TrimRight(strings.TrimSpace(os.Getenv("SAML_PUBLIC_BASE_URL")), "/")
	if b == "" {
		b = "http://localhost:8080"
	}
	return b + "/v1/auth/saml/metadata"
}

func joinPublicURL(pathSuffix string) string {
	base := strings.TrimSpace(os.Getenv("SAML_PUBLIC_BASE_URL"))
	if base == "" {
		base = "http://localhost:8080"
	}
	return strings.TrimRight(base, "/") + pathSuffix
}

func mustParseURL(raw string) url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		return url.URL{}
	}
	return *u
}

func idpMetadataFromConfig(cfg database.OrgSsoConfig) (*saml.EntityDescriptor, error) {
	block, _ := pem.Decode([]byte(cfg.SamlIdpCertPem.String))
	if block == nil {
		return nil, errors.New("invalid SAML IdP certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	b64 := base64.StdEncoding.EncodeToString(cert.Raw)
	return &saml.EntityDescriptor{
		EntityID: cfg.SamlIdpEntityID.String,
		IDPSSODescriptors: []saml.IDPSSODescriptor{{
			SSODescriptor: saml.SSODescriptor{
				RoleDescriptor: saml.RoleDescriptor{
					ProtocolSupportEnumeration: "urn:oasis:names:tc:SAML:2.0:protocol",
					KeyDescriptors: []saml.KeyDescriptor{{
						Use: "signing",
						KeyInfo: saml.KeyInfo{
							X509Data: saml.X509Data{
								X509Certificates: []saml.X509Certificate{{Data: b64}},
							},
						},
					}},
				},
			},
			SingleSignOnServices: []saml.Endpoint{
				{Binding: saml.HTTPRedirectBinding, Location: cfg.SamlIdpSsoUrl.String},
				{Binding: saml.HTTPPostBinding, Location: cfg.SamlIdpSsoUrl.String},
			},
		}},
	}, nil
}

func (apiCfg *apiConfig) samlServiceProviderForOrg(cfg database.OrgSsoConfig) (*saml.ServiceProvider, error) {
	key, cert, err := loadSAMLSPKeys()
	if err != nil {
		return nil, err
	}
	if !cfg.SamlIdpEntityID.Valid || !cfg.SamlIdpSsoUrl.Valid || !cfg.SamlIdpCertPem.Valid {
		return nil, errors.New("SAML IdP not fully configured")
	}
	idpMeta, err := idpMetadataFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	meta := mustParseURL(joinPublicURL("/v1/auth/saml/metadata"))
	acs := mustParseURL(joinPublicURL("/v1/auth/saml/acs"))
	slo := mustParseURL(joinPublicURL("/v1/auth/saml/slo"))
	return &saml.ServiceProvider{
		EntityID:          samlEntityID(),
		Key:               key,
		Certificate:       cert,
		MetadataURL:       meta,
		AcsURL:            acs,
		SloURL:            slo,
		IDPMetadata:       idpMeta,
		AuthnNameIDFormat: saml.EmailAddressNameIDFormat,
		SignatureMethod:   "",
		AllowIDPInitiated: false,
		DefaultRedirectURI: "/",
		LogoutBindings:    []string{saml.HTTPPostBinding},
	}, nil
}

func emailFromSAMLAssertion(a *saml.Assertion) string {
	if a.Subject != nil && a.Subject.NameID != nil {
		v := strings.TrimSpace(a.Subject.NameID.Value)
		if strings.Contains(v, "@") {
			return strings.ToLower(v)
		}
	}
	for _, stmt := range a.AttributeStatements {
		for _, attr := range stmt.Attributes {
			n := strings.ToLower(attr.Name)
			if n == "email" || strings.HasSuffix(n, "mail") || strings.Contains(n, "emailaddress") {
				for _, val := range attr.Values {
					if e := strings.TrimSpace(val.Value); strings.Contains(e, "@") {
						return strings.ToLower(e)
					}
				}
			}
		}
	}
	return ""
}

func samlNameIDSubject(a *saml.Assertion) string {
	if a.Subject != nil && a.Subject.NameID != nil {
		return strings.TrimSpace(a.Subject.NameID.Value)
	}
	return ""
}

// GET /v1/auth/saml/login?org_id=
func (apiCfg *apiConfig) handlerSAMLLogin(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("org_id")))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "org_id query required")
		return
	}
	if apiCfg.Features == nil || !apiCfg.Features.Enabled(r.Context(), orgID, feature.SAMLSSO) {
		respondWithError(w, http.StatusNotFound, "SAML SSO not available")
		return
	}
	cfg, err := apiCfg.DB.GetOrgSSOConfig(r.Context(), orgID)
	if err != nil || !cfg.Enabled {
		respondWithError(w, http.StatusBadRequest, "SSO not configured for organization")
		return
	}
	sp, err := apiCfg.samlServiceProviderForOrg(cfg)
	if err != nil {
		slog.Error("saml sp", "error", err)
		respondWithError(w, http.StatusServiceUnavailable, "SAML is not configured on this server")
		return
	}
	req, err := sp.MakeAuthenticationRequest(
		sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding,
		saml.HTTPPostBinding,
	)
	if err != nil {
		slog.Error("saml authn request", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	state, err := apiCfg.createSAMLStateToken(orgID, req.ID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	redirectURL, err := req.Redirect(state, sp)
	if err != nil {
		slog.Error("saml redirect", "error", err)
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

// POST /v1/auth/saml/acs — SAMLResponse (public).
func (apiCfg *apiConfig) handlerSAMLACS(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid form")
		return
	}
	relay := r.PostForm.Get("RelayState")
	if relay == "" {
		respondWithError(w, http.StatusBadRequest, "missing RelayState")
		return
	}
	orgID, reqID, err := apiCfg.parseSAMLStateToken(relay)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid RelayState")
		return
	}
	cfg, err := apiCfg.DB.GetOrgSSOConfig(r.Context(), orgID)
	if err != nil || !cfg.Enabled {
		respondWithError(w, http.StatusBadRequest, "SSO not configured")
		return
	}
	sp, err := apiCfg.samlServiceProviderForOrg(cfg)
	if err != nil {
		respondWithError(w, http.StatusServiceUnavailable, "SAML is not configured on this server")
		return
	}
	assertion, err := sp.ParseResponse(r, []string{reqID})
	if err != nil {
		slog.Error("saml parse response", "error", err)
		respondWithError(w, http.StatusUnauthorized, "invalid SAML response")
		return
	}
	email := emailFromSAMLAssertion(assertion)
	if email == "" {
		respondWithError(w, http.StatusUnauthorized, "email not found in SAML assertion")
		return
	}
	subj := samlNameIDSubject(assertion)
	if subj == "" {
		subj = email
	}
	user, err := apiCfg.findOrCreateSSOUser(r.Context(), orgID, email)
	if err != nil {
		respondWithError(w, http.StatusForbidden, err.Error())
		return
	}
	issuer := cfg.SamlIdpEntityID.String
	if _, err := apiCfg.DB.InsertUserExternalIdentity(r.Context(), database.InsertUserExternalIdentityParams{
		UserID:   user.ID,
		Provider: "saml",
		Issuer:   issuer,
		Subject:  subj,
		Email:    pgtype.Text{String: email, Valid: true},
	}); err != nil && !isPGUniqueViolation(err) {
		slog.Error("saml external identity", "error", err)
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

// GET /v1/auth/saml/metadata — SP metadata (public).
func (apiCfg *apiConfig) handlerSAMLMetadata(w http.ResponseWriter, r *http.Request) {
	key, cert, err := loadSAMLSPKeys()
	if err != nil {
		respondWithError(w, http.StatusNotFound, "SAML not configured on this server")
		return
	}
	meta := mustParseURL(joinPublicURL("/v1/auth/saml/metadata"))
	acs := mustParseURL(joinPublicURL("/v1/auth/saml/acs"))
	slo := mustParseURL(joinPublicURL("/v1/auth/saml/slo"))
	sp := &saml.ServiceProvider{
		EntityID:          samlEntityID(),
		Key:               key,
		Certificate:       cert,
		MetadataURL:       meta,
		AcsURL:            acs,
		SloURL:            slo,
		AuthnNameIDFormat: saml.EmailAddressNameIDFormat,
		SignatureMethod:   "",
		LogoutBindings:    []string{saml.HTTPPostBinding},
	}
	desc := sp.Metadata()
	out, err := xml.MarshalIndent(desc, "", "  ")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}
