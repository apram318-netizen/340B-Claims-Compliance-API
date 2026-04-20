-- +goose Up
-- Tenant settings, webhooks, exception cases, SSO identities, feature overrides.

CREATE TABLE organization_settings (
    org_id    UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    settings  JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE webhook_subscriptions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    url         TEXT NOT NULL,
    secret_hmac TEXT NOT NULL,
    event_types TEXT[] NOT NULL DEFAULT '{}',
    active      BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhook_subscriptions_org_id ON webhook_subscriptions (org_id);

CREATE TABLE webhook_deliveries (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id  UUID NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    event_type       TEXT NOT NULL,
    payload          JSONB NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending',
    attempt_count    INTEGER NOT NULL DEFAULT 0,
    next_retry_at    TIMESTAMPTZ,
    last_error       TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhook_deliveries_status_retry ON webhook_deliveries (status, next_retry_at)
    WHERE status IN ('pending', 'retrying');

CREATE TABLE exception_cases (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    claim_id         UUID REFERENCES claims(id) ON DELETE SET NULL,
    batch_id         UUID REFERENCES upload_batches(id) ON DELETE SET NULL,
    title            TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'open',
    priority         TEXT NOT NULL DEFAULT 'normal',
    assignee_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_by       UUID NOT NULL REFERENCES users(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT exception_cases_status_check CHECK (status IN ('open', 'in_progress', 'waiting', 'resolved', 'closed')),
    CONSTRAINT exception_cases_priority_check CHECK (priority IN ('low', 'normal', 'high', 'urgent'))
);

CREATE INDEX idx_exception_cases_org_id ON exception_cases (org_id);
CREATE INDEX idx_exception_cases_claim_id ON exception_cases (claim_id) WHERE claim_id IS NOT NULL;

CREATE TABLE case_comments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id    UUID NOT NULL REFERENCES exception_cases(id) ON DELETE CASCADE,
    author_id  UUID NOT NULL REFERENCES users(id),
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_case_comments_case_id ON case_comments (case_id);

CREATE TABLE user_external_identities (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider  TEXT NOT NULL,
    issuer    TEXT NOT NULL,
    subject   TEXT NOT NULL,
    email     TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issuer, subject)
);

CREATE INDEX idx_user_external_identities_user_id ON user_external_identities (user_id);

CREATE TABLE org_sso_config (
    org_id              UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    oidc_issuer         TEXT,
    oidc_client_id      TEXT,
    oidc_client_secret  TEXT,
    enabled             BOOLEAN NOT NULL DEFAULT false,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE feature_flag_overrides (
    org_id   UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    flag_key TEXT NOT NULL,
    value    BOOLEAN NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, flag_key)
);

-- +goose Down
DROP TABLE IF EXISTS feature_flag_overrides;
DROP TABLE IF EXISTS org_sso_config;
DROP TABLE IF EXISTS user_external_identities;
DROP TABLE IF EXISTS case_comments;
DROP TABLE IF EXISTS exception_cases;
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhook_subscriptions;
DROP TABLE IF EXISTS organization_settings;
