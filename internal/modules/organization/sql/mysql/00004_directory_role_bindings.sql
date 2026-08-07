-- +goose Up
CREATE TABLE iam_group_role_bindings (
    tenant_id VARCHAR(128) NOT NULL,
    role_id VARCHAR(32) NOT NULL,
    group_id VARCHAR(128) NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, role_id, group_id),
    KEY idx_iam_group_role_bindings_group (tenant_id, group_id, role_id),
    CONSTRAINT fk_iam_group_role_bindings_role FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_iam_group_role_bindings_group FOREIGN KEY (tenant_id, group_id) REFERENCES iam_groups (tenant_id, id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_position_role_bindings (
    tenant_id VARCHAR(128) NOT NULL,
    role_id VARCHAR(32) NOT NULL,
    position_id VARCHAR(128) NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, role_id, position_id),
    KEY idx_iam_position_role_bindings_position (tenant_id, position_id, role_id),
    CONSTRAINT fk_iam_position_role_bindings_role FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_iam_position_role_bindings_position FOREIGN KEY (tenant_id, position_id) REFERENCES iam_positions (tenant_id, id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS iam_position_role_bindings;
DROP TABLE IF EXISTS iam_group_role_bindings;
