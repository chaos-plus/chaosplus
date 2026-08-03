-- +goose Up
CREATE TABLE iam_member_departments (
    tenant_id VARCHAR(128) NOT NULL,
    principal_id VARCHAR(255) NOT NULL,
    department_id VARCHAR(128) NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, principal_id),
    KEY idx_iam_member_departments_department (tenant_id, department_id, principal_id),
    CONSTRAINT fk_iam_member_departments_member FOREIGN KEY (tenant_id, principal_id) REFERENCES iam_tenant_members (tenant_id, user_subject) ON DELETE CASCADE,
    CONSTRAINT fk_iam_member_departments_department FOREIGN KEY (tenant_id, department_id) REFERENCES iam_departments (tenant_id, id) ON DELETE RESTRICT
) ENGINE=InnoDB;

CREATE TABLE iam_role_data_scopes (
    tenant_id VARCHAR(128) NOT NULL,
    role_id VARCHAR(32) NOT NULL,
    scope_type VARCHAR(32) NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, role_id),
    CONSTRAINT chk_iam_role_data_scopes_type CHECK (scope_type IN ('all', 'self', 'department', 'department_and_descendants', 'selected_departments')),
    CONSTRAINT fk_iam_role_data_scopes_role FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE iam_role_scope_departments (
    tenant_id VARCHAR(128) NOT NULL,
    role_id VARCHAR(32) NOT NULL,
    department_id VARCHAR(128) NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, role_id, department_id),
    KEY idx_iam_role_scope_departments_department (tenant_id, department_id, role_id),
    CONSTRAINT fk_iam_role_scope_departments_scope FOREIGN KEY (tenant_id, role_id) REFERENCES iam_role_data_scopes (tenant_id, role_id) ON DELETE CASCADE,
    CONSTRAINT fk_iam_role_scope_departments_department FOREIGN KEY (tenant_id, department_id) REFERENCES iam_departments (tenant_id, id) ON DELETE RESTRICT
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE IF EXISTS iam_role_scope_departments;
DROP TABLE IF EXISTS iam_role_data_scopes;
DROP TABLE IF EXISTS iam_member_departments;
