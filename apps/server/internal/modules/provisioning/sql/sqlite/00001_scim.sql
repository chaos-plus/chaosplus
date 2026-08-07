-- +goose Up
CREATE TABLE iam_scim_directories (
    id TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    name_key TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    CONSTRAINT uq_iam_scim_directories_name UNIQUE (tenant_id, name_key),
    FOREIGN KEY (tenant_id) REFERENCES iam_tenants (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_scim_directories_tenant ON iam_scim_directories (tenant_id, status, name_key, id);

CREATE TABLE iam_scim_credentials (
    id TEXT NOT NULL PRIMARY KEY,
    directory_id TEXT NOT NULL,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    expires_at BIGINT NOT NULL DEFAULT 0 CHECK (expires_at >= 0),
    last_used_at BIGINT NOT NULL DEFAULT 0 CHECK (last_used_at >= 0),
    revoked_at BIGINT NOT NULL DEFAULT 0 CHECK (revoked_at >= 0),
    created_at BIGINT NOT NULL,
    FOREIGN KEY (directory_id) REFERENCES iam_scim_directories (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_scim_credentials_directory ON iam_scim_credentials (directory_id, revoked_at, expires_at, created_at);

CREATE TABLE iam_scim_resources (
    directory_id TEXT NOT NULL,
    resource_type TEXT NOT NULL CHECK (resource_type IN ('User', 'Group')),
    resource_id TEXT NOT NULL,
    external_id TEXT NOT NULL DEFAULT '',
    external_key TEXT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT NOT NULL DEFAULT 0 CHECK (deleted_at >= 0),
    PRIMARY KEY (directory_id, resource_type, resource_id),
    CONSTRAINT uq_iam_scim_resources_external UNIQUE (directory_id, resource_type, external_key),
    FOREIGN KEY (directory_id) REFERENCES iam_scim_directories (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_scim_resources_list ON iam_scim_resources (directory_id, resource_type, deleted_at, resource_id);

-- +goose Down
DROP TABLE iam_scim_resources;
DROP TABLE iam_scim_credentials;
DROP TABLE iam_scim_directories;
