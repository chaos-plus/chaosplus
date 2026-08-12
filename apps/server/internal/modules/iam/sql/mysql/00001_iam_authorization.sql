-- +goose Up
CREATE TABLE iam_roles (
    tenant_id BIGINT NOT NULL,
    id BIGINT  NOT NULL,
    name         VARCHAR(128) NOT NULL,
    description  TEXT         NOT NULL,
    created_at   BIGINT       NOT NULL,
    updated_at   BIGINT       NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE KEY uq_iam_roles_name (tenant_id, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_role_permissions (
    tenant_id BIGINT NOT NULL,
    role_id BIGINT  NOT NULL,
    permission_code VARCHAR(128) NOT NULL,
    created_at      BIGINT       NOT NULL,
    PRIMARY KEY (tenant_id, role_id, permission_code),
    CONSTRAINT fk_iam_role_permissions_role FOREIGN KEY (tenant_id, role_id)
        REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_role_members (
    tenant_id BIGINT NOT NULL,
    role_id BIGINT  NOT NULL,
    principal_id BIGINT NOT NULL,
    created_at   BIGINT       NOT NULL,
    PRIMARY KEY (tenant_id, role_id, principal_id),
    CONSTRAINT fk_iam_role_members_role FOREIGN KEY (tenant_id, role_id)
        REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE iam_role_members;
DROP TABLE iam_role_permissions;
DROP TABLE iam_roles;