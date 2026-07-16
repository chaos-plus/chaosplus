-- +goose Up
CREATE TABLE iam_roles (
    tenant_id  TEXT   NOT NULL,
    id         TEXT   NOT NULL,
    name       TEXT   NOT NULL,
    description TEXT  NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, name)
);

CREATE TABLE iam_role_permissions (
    tenant_id      TEXT   NOT NULL,
    role_id        TEXT   NOT NULL,
    permission_code TEXT  NOT NULL,
    created_at     BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, role_id, permission_code),
    FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE iam_role_members (
    tenant_id   TEXT   NOT NULL,
    role_id     TEXT   NOT NULL,
    user_subject TEXT  NOT NULL,
    created_at  BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, role_id, user_subject),
    FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE authz_outbox (
    id                  TEXT    NOT NULL PRIMARY KEY,
    tenant_id           TEXT    NOT NULL,
    relationship_key    TEXT    NOT NULL,
    resource_type       TEXT    NOT NULL,
    resource_id         TEXT    NOT NULL,
    resource_relation   TEXT    NOT NULL,
    subject_type        TEXT    NOT NULL,
    subject_id          TEXT    NOT NULL,
    subject_relation    TEXT    NOT NULL DEFAULT '',
    operation           TEXT    NOT NULL,
    version             BIGINT  NOT NULL,
    status              TEXT    NOT NULL,
    attempts            INTEGER NOT NULL,
    available_at        BIGINT  NOT NULL,
    locked_by           TEXT    NOT NULL DEFAULT '',
    locked_at           BIGINT  NOT NULL DEFAULT 0,
    last_error          TEXT    NOT NULL DEFAULT '',
    zed_token           TEXT    NOT NULL DEFAULT '',
    created_at          BIGINT  NOT NULL,
    updated_at          BIGINT  NOT NULL,
    delivered_at        BIGINT  NOT NULL DEFAULT 0,
    UNIQUE (tenant_id, relationship_key)
);

CREATE INDEX idx_authz_outbox_pending ON authz_outbox (status, available_at);
CREATE INDEX idx_authz_outbox_locked ON authz_outbox (status, locked_at);

-- +goose Down
DROP TABLE authz_outbox;
DROP TABLE iam_role_members;
DROP TABLE iam_role_permissions;
DROP TABLE iam_roles;
