-- +goose Up
CREATE TABLE iam_member_departments (
    tenant_id BIGINT NOT NULL,
    principal_id BIGINT NOT NULL,
    department_id BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, principal_id),
    FOREIGN KEY (tenant_id, principal_id) REFERENCES iam_tenant_members (tenant_id, principal_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, department_id) REFERENCES iam_departments (tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_iam_member_departments_department ON iam_member_departments (tenant_id, department_id, principal_id);

CREATE TABLE iam_role_data_scopes (
    tenant_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL,
    scope_type VARCHAR(32) NOT NULL CHECK (scope_type IN ('all', 'self', 'department', 'department_and_descendants', 'selected_departments')),
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, role_id),
    FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE iam_role_scope_departments (
    tenant_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL,
    department_id BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, role_id, department_id),
    FOREIGN KEY (tenant_id, role_id) REFERENCES iam_role_data_scopes (tenant_id, role_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, department_id) REFERENCES iam_departments (tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_iam_role_scope_departments_department ON iam_role_scope_departments (tenant_id, department_id, role_id);

-- +goose Down
DROP TABLE IF EXISTS iam_role_scope_departments;
DROP TABLE IF EXISTS iam_role_data_scopes;
DROP TABLE IF EXISTS iam_member_departments;