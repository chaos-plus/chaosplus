-- +goose Up
CREATE TABLE iam_identity_providers (
    id BIGINT NOT NULL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    provider_type TEXT NOT NULL DEFAULT 'oidc',
    issuer TEXT NOT NULL,
    client_id TEXT NOT NULL,
    client_secret_ciphertext TEXT NOT NULL DEFAULT '',
    scopes TEXT NOT NULL DEFAULT 'openid profile email',
    auto_provision INTEGER NOT NULL DEFAULT 1,
    default_role_id BIGINT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE (tenant_id, issuer)
);
CREATE INDEX idx_iam_identity_providers_tenant ON iam_identity_providers (tenant_id, status, name);

CREATE TABLE iam_identity_links (
    provider_id BIGINT NOT NULL,
    tenant_id BIGINT NOT NULL,
    principal_id BIGINT NOT NULL,
    external_subject TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    last_login_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (provider_id, tenant_id, external_subject),
    UNIQUE (provider_id, principal_id),
    FOREIGN KEY (provider_id) REFERENCES iam_identity_providers (id) ON DELETE CASCADE,
    FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_identity_links_principal ON iam_identity_links (tenant_id, principal_id);

-- +goose Down
DROP TABLE iam_identity_links;
DROP TABLE iam_identity_providers;