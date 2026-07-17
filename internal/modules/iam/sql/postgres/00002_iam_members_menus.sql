-- +goose Up
CREATE TABLE iam_tenant_members (
    tenant_id VARCHAR(128) NOT NULL,
    user_subject VARCHAR(255) NOT NULL,
    display_name VARCHAR(128) NOT NULL DEFAULT '',
    email VARCHAR(320) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    disabled_at BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, user_subject)
);
CREATE INDEX idx_iam_tenant_members_status ON iam_tenant_members (tenant_id, status, display_name, user_subject);

CREATE TABLE iam_menus (
    tenant_id VARCHAR(128) NOT NULL,
    id VARCHAR(32) NOT NULL,
    parent_id VARCHAR(32) NULL,
    label VARCHAR(128) NOT NULL,
    route VARCHAR(512) NULL,
    icon VARCHAR(64) NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    permission_code VARCHAR(128) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'disabled')),
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
