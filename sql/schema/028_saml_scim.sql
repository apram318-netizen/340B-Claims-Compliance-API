-- +goose Up
-- SAML IdP settings (SP key/cert via env; see docs/operator-platform.md).

ALTER TABLE org_sso_config
    ADD COLUMN IF NOT EXISTS saml_idp_entity_id TEXT,
    ADD COLUMN IF NOT EXISTS saml_idp_sso_url TEXT,
    ADD COLUMN IF NOT EXISTS saml_idp_cert_pem TEXT;

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS active BOOLEAN NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS active;
ALTER TABLE org_sso_config DROP COLUMN IF EXISTS saml_idp_cert_pem;
ALTER TABLE org_sso_config DROP COLUMN IF EXISTS saml_idp_sso_url;
ALTER TABLE org_sso_config DROP COLUMN IF EXISTS saml_idp_entity_id;
