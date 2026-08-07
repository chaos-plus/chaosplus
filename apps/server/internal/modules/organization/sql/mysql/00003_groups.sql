-- +goose Up
CREATE TABLE iam_groups (
    tenant_id VARCHAR(128) NOT NULL,
    id VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    name_key VARCHAR(128) NOT NULL,
    group_type VARCHAR(32) NOT NULL,
    description VARCHAR(1024) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 1,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE KEY uq_iam_groups_name (tenant_id, name_key),
    KEY idx_iam_groups_order (tenant_id, sort_order, name_key, id),
    CONSTRAINT chk_iam_groups_type CHECK (group_type = 'static'),
    CONSTRAINT chk_iam_groups_status CHECK (status IN ('active', 'disabled')),
    CONSTRAINT chk_iam_groups_sort_order CHECK (sort_order >= 0 AND sort_order <= 1000000),
    CONSTRAINT chk_iam_groups_version CHECK (version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_group_members (
    tenant_id VARCHAR(128) NOT NULL,
    group_id VARCHAR(128) NOT NULL,
    principal_id VARCHAR(255) NOT NULL,
    starts_at BIGINT NOT NULL DEFAULT 0,
    ends_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, group_id, principal_id),
    KEY idx_iam_group_members_principal (tenant_id, principal_id, starts_at, ends_at, group_id),
    CONSTRAINT fk_iam_group_members_group FOREIGN KEY (tenant_id, group_id) REFERENCES iam_groups (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_iam_group_members_principal FOREIGN KEY (tenant_id, principal_id) REFERENCES iam_tenant_members (tenant_id, user_subject) ON DELETE CASCADE,
    CONSTRAINT chk_iam_group_members_starts CHECK (starts_at >= 0),
    CONSTRAINT chk_iam_group_members_ends CHECK (ends_at >= 0 AND (ends_at = 0 OR starts_at = 0 OR ends_at > starts_at))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS iam_group_members;
DROP TABLE IF EXISTS iam_groups;
