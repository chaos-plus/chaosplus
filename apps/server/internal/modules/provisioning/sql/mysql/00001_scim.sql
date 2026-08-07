-- +goose Up
CREATE TABLE iam_scim_directories (
    id VARCHAR(128) NOT NULL PRIMARY KEY,
    tenant_id VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    name_key VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    CONSTRAINT chk_iam_scim_directories_status CHECK (status IN ('active', 'disabled')),
    CONSTRAINT chk_iam_scim_directories_version CHECK (version >= 1),
    CONSTRAINT uq_iam_scim_directories_name UNIQUE (tenant_id, name_key),
    CONSTRAINT fk_iam_scim_directories_tenant FOREIGN KEY (tenant_id) REFERENCES iam_tenants(id) ON DELETE CASCADE,
    KEY idx_iam_scim_directories_tenant (tenant_id, status, name_key, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_scim_credentials (
    id VARCHAR(128) NOT NULL PRIMARY KEY,
    directory_id VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    token_hash TEXT NOT NULL,
    expires_at BIGINT NOT NULL DEFAULT 0,
    last_used_at BIGINT NOT NULL DEFAULT 0,
    revoked_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    CONSTRAINT chk_iam_scim_credentials_times CHECK (expires_at >= 0 AND last_used_at >= 0 AND revoked_at >= 0),
    CONSTRAINT fk_iam_scim_credentials_directory FOREIGN KEY (directory_id) REFERENCES iam_scim_directories(id) ON DELETE CASCADE,
    KEY idx_iam_scim_credentials_directory (directory_id, revoked_at, expires_at, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_scim_resources (
    directory_id VARCHAR(128) NOT NULL,
    resource_type VARCHAR(16) NOT NULL,
    resource_id VARCHAR(128) NOT NULL,
    external_id VARCHAR(512) NOT NULL DEFAULT '',
    external_key VARCHAR(512) NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (directory_id, resource_type, resource_id),
    CONSTRAINT chk_iam_scim_resources_type CHECK (resource_type IN ('User', 'Group')),
    CONSTRAINT chk_iam_scim_resources_version CHECK (version >= 1 AND deleted_at >= 0),
    CONSTRAINT uq_iam_scim_resources_external UNIQUE (directory_id, resource_type, external_key),
    CONSTRAINT fk_iam_scim_resources_directory FOREIGN KEY (directory_id) REFERENCES iam_scim_directories(id) ON DELETE CASCADE,
    KEY idx_iam_scim_resources_list (directory_id, resource_type, deleted_at, resource_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE iam_scim_resources;
DROP TABLE iam_scim_credentials;
DROP TABLE iam_scim_directories;
