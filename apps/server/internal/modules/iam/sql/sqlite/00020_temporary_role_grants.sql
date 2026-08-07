-- +goose Up
CREATE TABLE iam_temporary_role_grants (
    tenant_id   TEXT   NOT NULL,
    id          TEXT   NOT NULL,
    role_id     TEXT   NOT NULL,
    principal_id TEXT  NOT NULL,
    source_type TEXT   NOT NULL CHECK (source_type IN ('access_request')),
    source_id   TEXT   NOT NULL,
    starts_at   BIGINT NOT NULL,
    ends_at     BIGINT NOT NULL,
    created_by  TEXT   NOT NULL,
    created_at  BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, source_type, source_id),
    FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_temporary_role_grants_principal
    ON iam_temporary_role_grants (tenant_id, principal_id, starts_at, ends_at);

-- +goose Down
DROP TABLE IF EXISTS iam_temporary_role_grants;
