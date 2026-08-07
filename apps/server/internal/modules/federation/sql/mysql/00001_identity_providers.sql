-- +goose Up
CREATE TABLE iam_identity_providers (
    id VARCHAR(128) PRIMARY KEY,
    tenant_id VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    provider_type VARCHAR(16) NOT NULL DEFAULT 'oidc',
    issuer VARCHAR(255) NOT NULL,
    client_id VARCHAR(128) NOT NULL,
    client_secret_ciphertext TEXT NOT NULL,
    scopes VARCHAR(255) NOT NULL DEFAULT 'openid profile email',
    auto_provision BOOLEAN NOT NULL DEFAULT TRUE,
    default_role_id VARCHAR(32) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE KEY uq_iam_identity_providers_tenant_issuer (tenant_id, issuer),
    KEY idx_iam_identity_providers_tenant (tenant_id, status, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE iam_identity_links (
    provider_id VARCHAR(128) NOT NULL,
    tenant_id VARCHAR(128) NOT NULL,
    principal_id VARCHAR(64) NOT NULL,
    external_subject VARCHAR(128) NOT NULL,
    email VARCHAR(255) NOT NULL DEFAULT '',
    display_name VARCHAR(128) NOT NULL DEFAULT '',
    last_login_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (provider_id, tenant_id, external_subject),
    UNIQUE KEY uq_iam_identity_links_provider_principal (provider_id, principal_id),
    KEY idx_iam_identity_links_principal (tenant_id, principal_id),
    CONSTRAINT fk_iam_identity_links_provider FOREIGN KEY (provider_id) REFERENCES iam_identity_providers (id) ON DELETE CASCADE,
    CONSTRAINT fk_iam_identity_links_principal FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE iam_identity_links;
DROP TABLE iam_identity_providers;
