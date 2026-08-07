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
    UNIQUE (tenant_id, issuer)
);
CREATE INDEX idx_iam_identity_providers_tenant ON iam_identity_providers (tenant_id, status, name);
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
    UNIQUE (provider_id, principal_id),
    CONSTRAINT fk_iam_identity_links_provider FOREIGN KEY (provider_id) REFERENCES iam_identity_providers (id) ON DELETE CASCADE,
    CONSTRAINT fk_iam_identity_links_principal FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_identity_links_principal ON iam_identity_links (tenant_id, principal_id);

-- +goose Down
DROP TABLE iam_identity_links;
DROP TABLE iam_identity_providers;
