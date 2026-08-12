-- +goose Up
CREATE TABLE iam_departments (
    tenant_id BIGINT NOT NULL,
    id BIGINT NOT NULL,
    parent_id BIGINT NOT NULL DEFAULT '',
    name VARCHAR(128) NOT NULL,
    name_key VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 1,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE KEY uq_iam_departments_sibling_name (tenant_id, parent_id, name_key),
    KEY idx_iam_departments_parent (tenant_id, parent_id, sort_order, name_key, id),
    CONSTRAINT chk_iam_departments_status CHECK (status IN ('active', 'disabled')),
    CONSTRAINT chk_iam_departments_sort_order CHECK (sort_order >= 0 AND sort_order <= 1000000),
    CONSTRAINT chk_iam_departments_version CHECK (version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_department_closure (
    tenant_id BIGINT NOT NULL,
    ancestor_id BIGINT NOT NULL,
    descendant_id BIGINT NOT NULL,
    depth INT NOT NULL,
    PRIMARY KEY (tenant_id, ancestor_id, descendant_id),
    KEY idx_iam_department_closure_descendant (tenant_id, descendant_id, depth, ancestor_id),
    CONSTRAINT fk_iam_department_closure_ancestor FOREIGN KEY (tenant_id, ancestor_id) REFERENCES iam_departments (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_iam_department_closure_descendant FOREIGN KEY (tenant_id, descendant_id) REFERENCES iam_departments (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT chk_iam_department_closure_depth CHECK (depth >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS iam_department_closure;
DROP TABLE IF EXISTS iam_departments;