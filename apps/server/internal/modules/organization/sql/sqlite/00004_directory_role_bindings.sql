-- +goose Up
CREATE TABLE iam_group_role_bindings (
    tenant_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, role_id, group_id),
    FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, group_id) REFERENCES iam_groups (tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_iam_group_role_bindings_group ON iam_group_role_bindings (tenant_id, group_id, role_id);

CREATE TABLE iam_position_role_bindings (
    tenant_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL,
    position_id BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, role_id, position_id),
    FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, position_id) REFERENCES iam_positions (tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_iam_position_role_bindings_position ON iam_position_role_bindings (tenant_id, position_id, role_id);

-- +goose Down
DROP TABLE IF EXISTS iam_position_role_bindings;
DROP TABLE IF EXISTS iam_group_role_bindings;