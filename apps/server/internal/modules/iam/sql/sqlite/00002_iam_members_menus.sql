-- +goose Up
CREATE TABLE iam_tenant_members (
    tenant_id BIGINT NOT NULL,
    principal_id BIGINT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    disabled_at BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, principal_id)
);
CREATE INDEX idx_iam_tenant_members_status ON iam_tenant_members (tenant_id, status, display_name, principal_id);

CREATE TABLE iam_menus (
    tenant_id BIGINT NOT NULL,
    id BIGINT NOT NULL,
    parent_id BIGINT NULL,
    label TEXT NOT NULL,
    route TEXT NULL,
    icon TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    permission_code TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, route),
    FOREIGN KEY (tenant_id, parent_id) REFERENCES iam_menus (tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_iam_menus_tree ON iam_menus (tenant_id, status, parent_id, sort_order, id);

-- +goose Down
DROP TABLE iam_menus;
DROP TABLE iam_tenant_members;