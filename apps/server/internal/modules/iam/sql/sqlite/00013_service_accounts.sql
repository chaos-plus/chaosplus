-- +goose Up
CREATE TABLE iam_service_accounts (
    principal_id BIGINT NOT NULL PRIMARY KEY,
    owner_tenant_id BIGINT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'deleted')),
    expires_at BIGINT NOT NULL DEFAULT 0,
    token_version BIGINT NOT NULL DEFAULT 1,
    version BIGINT NOT NULL DEFAULT 1,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT NOT NULL DEFAULT 0,
    FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE RESTRICT
);
CREATE INDEX idx_iam_service_accounts_tenant ON iam_service_accounts (owner_tenant_id, status, principal_id);

CREATE TABLE iam_service_account_credentials (
    id BIGINT NOT NULL PRIMARY KEY,
    principal_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    secret_hash TEXT NOT NULL,
    scopes TEXT NOT NULL DEFAULT '',
    expires_at BIGINT NOT NULL DEFAULT 0,
    last_used_at BIGINT NOT NULL DEFAULT 0,
    revoked_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    FOREIGN KEY (principal_id) REFERENCES iam_service_accounts (principal_id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_service_account_credentials_active ON iam_service_account_credentials (principal_id, revoked_at, expires_at);

-- +goose Down
DROP TABLE iam_service_account_credentials;
DROP TABLE iam_service_accounts;