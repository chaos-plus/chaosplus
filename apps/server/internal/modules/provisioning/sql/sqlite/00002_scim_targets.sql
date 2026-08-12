-- +goose Up
CREATE TABLE iam_scim_targets (
    id BIGINT NOT NULL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    name_key TEXT NOT NULL,
    base_url TEXT NOT NULL,
    bearer_token_ciphertext TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    CONSTRAINT uq_iam_scim_targets_name UNIQUE (tenant_id, name_key),
    FOREIGN KEY (tenant_id) REFERENCES iam_tenants (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_scim_targets_tenant ON iam_scim_targets (tenant_id, status, name_key, id);

CREATE TABLE iam_scim_target_resources (
    target_id BIGINT NOT NULL,
    resource_type TEXT NOT NULL CHECK (resource_type IN ('User', 'Group')),
    resource_id BIGINT NOT NULL,
    external_id TEXT NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT NOT NULL DEFAULT 0 CHECK (deleted_at >= 0),
    PRIMARY KEY (target_id, resource_type, resource_id),
    FOREIGN KEY (target_id) REFERENCES iam_scim_targets (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_scim_target_resources_target ON iam_scim_target_resources (target_id, resource_type, deleted_at, resource_id);

-- +goose Down
DROP TABLE iam_scim_target_resources;
DROP TABLE iam_scim_targets;