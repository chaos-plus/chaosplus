-- +goose Up
CREATE TABLE iam_groups (
    tenant_id TEXT NOT NULL,
    id TEXT NOT NULL,
    name TEXT NOT NULL,
    name_key TEXT NOT NULL,
    group_type TEXT NOT NULL CHECK (group_type = 'static'),
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0 AND sort_order <= 1000000),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT uq_iam_groups_name UNIQUE (tenant_id, name_key)
);
CREATE INDEX idx_iam_groups_order ON iam_groups (tenant_id, sort_order, name_key, id);

CREATE TABLE iam_group_members (
    tenant_id TEXT NOT NULL,
    group_id TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    starts_at BIGINT NOT NULL DEFAULT 0 CHECK (starts_at >= 0),
    ends_at BIGINT NOT NULL DEFAULT 0 CHECK (ends_at >= 0 AND (ends_at = 0 OR starts_at = 0 OR ends_at > starts_at)),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, group_id, principal_id),
    FOREIGN KEY (tenant_id, group_id) REFERENCES iam_groups (tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, principal_id) REFERENCES iam_tenant_members (tenant_id, user_subject) ON DELETE CASCADE
);
CREATE INDEX idx_iam_group_members_principal ON iam_group_members (tenant_id, principal_id, starts_at, ends_at, group_id);

-- +goose Down
DROP TABLE IF EXISTS iam_group_members;
DROP TABLE IF EXISTS iam_groups;
