-- +goose Up
CREATE TABLE iam_service_accounts (
    principal_id BIGINT PRIMARY KEY REFERENCES iam_principals(id) ON DELETE RESTRICT,
    owner_tenant_id BIGINT NOT NULL,
    description VARCHAR(1000) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'deleted')),
    expires_at BIGINT NOT NULL DEFAULT 0,
    token_version BIGINT NOT NULL DEFAULT 1,
    version BIGINT NOT NULL DEFAULT 1,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_iam_service_accounts_tenant ON iam_service_accounts (owner_tenant_id, status, principal_id);

CREATE TABLE iam_service_account_credentials (
    id BIGINT PRIMARY KEY,
    principal_id BIGINT NOT NULL REFERENCES iam_service_accounts(principal_id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    secret_hash TEXT NOT NULL,
    scopes TEXT NOT NULL DEFAULT '',
    expires_at BIGINT NOT NULL DEFAULT 0,
    last_used_at BIGINT NOT NULL DEFAULT 0,
    revoked_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL
);
CREATE INDEX idx_iam_service_account_credentials_active ON iam_service_account_credentials (principal_id, revoked_at, expires_at);

-- +goose Down
DROP TABLE iam_service_account_credentials;
DROP TABLE iam_service_accounts;