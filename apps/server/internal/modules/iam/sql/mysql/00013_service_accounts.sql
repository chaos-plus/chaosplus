-- +goose Up
CREATE TABLE iam_service_accounts (
    principal_id VARCHAR(64) PRIMARY KEY,
    owner_tenant_id VARCHAR(128) NOT NULL,
    description VARCHAR(1000) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    expires_at BIGINT NOT NULL DEFAULT 0,
    token_version BIGINT NOT NULL DEFAULT 1,
    version BIGINT NOT NULL DEFAULT 1,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT NOT NULL DEFAULT 0,
    KEY idx_iam_service_accounts_tenant (owner_tenant_id, status, principal_id),
    CONSTRAINT fk_iam_service_accounts_principal FOREIGN KEY (principal_id) REFERENCES iam_principals(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_service_account_credentials (
    id VARCHAR(64) PRIMARY KEY,
    principal_id VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    secret_hash TEXT NOT NULL,
    scopes TEXT NOT NULL,
    expires_at BIGINT NOT NULL DEFAULT 0,
    last_used_at BIGINT NOT NULL DEFAULT 0,
    revoked_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    KEY idx_iam_service_account_credentials_active (principal_id, revoked_at, expires_at),
    CONSTRAINT fk_iam_service_account_credentials_principal FOREIGN KEY (principal_id) REFERENCES iam_service_accounts(principal_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE iam_service_account_credentials;
DROP TABLE iam_service_accounts;
