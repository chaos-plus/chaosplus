-- +goose Up
CREATE TABLE iam_temporary_role_grants (
    tenant_id    VARCHAR(128) NOT NULL,
    id           VARCHAR(64)  NOT NULL,
    role_id      VARCHAR(32)  NOT NULL,
    principal_id VARCHAR(255) NOT NULL,
    source_type  VARCHAR(32)  NOT NULL,
    source_id    VARCHAR(64)  NOT NULL,
    starts_at    BIGINT       NOT NULL,
    ends_at      BIGINT       NOT NULL,
    created_by   VARCHAR(255) NOT NULL,
    created_at   BIGINT       NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE KEY uq_iam_temporary_role_grants_source (tenant_id, source_type, source_id),
    KEY idx_iam_temporary_role_grants_principal (tenant_id, principal_id, starts_at, ends_at),
    CONSTRAINT fk_iam_temporary_role_grants_role FOREIGN KEY (tenant_id, role_id)
        REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT chk_iam_temporary_role_grants_source CHECK (source_type IN ('access_request'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS iam_temporary_role_grants;
