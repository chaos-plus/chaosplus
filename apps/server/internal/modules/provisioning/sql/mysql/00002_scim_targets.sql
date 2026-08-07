-- +goose Up
CREATE TABLE iam_scim_targets (
    id VARCHAR(128) NOT NULL PRIMARY KEY,
    tenant_id VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    name_key VARCHAR(128) NOT NULL,
    base_url VARCHAR(1024) NOT NULL,
    bearer_token_ciphertext TEXT NOT NULL,
    status VARCHAR(16) NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    CONSTRAINT chk_iam_scim_targets_status CHECK (status IN ('active', 'disabled')),
    CONSTRAINT chk_iam_scim_targets_version CHECK (version >= 1),
    CONSTRAINT uq_iam_scim_targets_name UNIQUE (tenant_id, name_key),
    CONSTRAINT fk_iam_scim_targets_tenant FOREIGN KEY (tenant_id) REFERENCES iam_tenants(id) ON DELETE CASCADE,
    KEY idx_iam_scim_targets_tenant (tenant_id, status, name_key, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_scim_target_resources (
    target_id VARCHAR(128) NOT NULL,
    resource_type VARCHAR(16) NOT NULL,
    resource_id VARCHAR(128) NOT NULL,
    external_id VARCHAR(512) NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (target_id, resource_type, resource_id),
    CONSTRAINT chk_iam_scim_target_resources_type CHECK (resource_type IN ('User', 'Group')),
    CONSTRAINT chk_iam_scim_target_resources_version CHECK (version >= 1 AND deleted_at >= 0),
    CONSTRAINT fk_iam_scim_target_resources_target FOREIGN KEY (target_id) REFERENCES iam_scim_targets(id) ON DELETE CASCADE,
    KEY idx_iam_scim_target_resources_target (target_id, resource_type, deleted_at, resource_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE iam_scim_target_resources;
DROP TABLE iam_scim_targets;
