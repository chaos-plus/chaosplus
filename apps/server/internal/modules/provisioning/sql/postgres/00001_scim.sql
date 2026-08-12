-- +goose Up
CREATE TABLE iam_scim_directories (
    id BIGINT PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES iam_tenants(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    name_key VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'disabled')),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    CONSTRAINT uq_iam_scim_directories_name UNIQUE (tenant_id, name_key)
);
CREATE INDEX idx_iam_scim_directories_tenant ON iam_scim_directories (tenant_id, status, name_key, id);

CREATE TABLE iam_scim_credentials (
    id BIGINT PRIMARY KEY,
    directory_id BIGINT NOT NULL REFERENCES iam_scim_directories(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    token_hash TEXT NOT NULL,
    expires_at BIGINT NOT NULL DEFAULT 0 CHECK (expires_at >= 0),
    last_used_at BIGINT NOT NULL DEFAULT 0 CHECK (last_used_at >= 0),
    revoked_at BIGINT NOT NULL DEFAULT 0 CHECK (revoked_at >= 0),
    created_at BIGINT NOT NULL
);
CREATE INDEX idx_iam_scim_credentials_directory ON iam_scim_credentials (directory_id, revoked_at, expires_at, created_at);

CREATE TABLE iam_scim_resources (
    directory_id BIGINT NOT NULL REFERENCES iam_scim_directories(id) ON DELETE CASCADE,
    resource_type VARCHAR(16) NOT NULL CHECK (resource_type IN ('User', 'Group')),
    resource_id BIGINT NOT NULL,
    external_id VARCHAR(512) NOT NULL DEFAULT '',
    external_key VARCHAR(512) NOT NULL,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT NOT NULL DEFAULT 0 CHECK (deleted_at >= 0),
    PRIMARY KEY (directory_id, resource_type, resource_id),
    CONSTRAINT uq_iam_scim_resources_external UNIQUE (directory_id, resource_type, external_key)
);
CREATE INDEX idx_iam_scim_resources_list ON iam_scim_resources (directory_id, resource_type, deleted_at, resource_id);

-- +goose Down
DROP TABLE iam_scim_resources;
DROP TABLE iam_scim_credentials;
DROP TABLE iam_scim_directories;