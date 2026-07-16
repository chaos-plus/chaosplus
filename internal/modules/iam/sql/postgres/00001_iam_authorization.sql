-- +goose Up
CREATE TABLE iam_roles (
    tenant_id    VARCHAR(128) NOT NULL,
    id           VARCHAR(32)  NOT NULL,
    name         VARCHAR(128) NOT NULL,
    description  TEXT         NOT NULL DEFAULT '',
    created_at   BIGINT       NOT NULL,
    updated_at   BIGINT       NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, name)
);

CREATE TABLE iam_role_permissions (
    tenant_id       VARCHAR(128) NOT NULL,
    role_id         VARCHAR(32)  NOT NULL,
    permission_code VARCHAR(128) NOT NULL,
    created_at      BIGINT       NOT NULL,
    PRIMARY KEY (tenant_id, role_id, permission_code),
    FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE iam_role_members (
    tenant_id    VARCHAR(128) NOT NULL,
    role_id      VARCHAR(32)  NOT NULL,
    user_subject VARCHAR(255) NOT NULL,
    created_at   BIGINT       NOT NULL,
    PRIMARY KEY (tenant_id, role_id, user_subject),
    FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE authz_outbox (
    id                  VARCHAR(32)   NOT NULL PRIMARY KEY,
    tenant_id           VARCHAR(128)  NOT NULL,
    relationship_key    CHAR(64)      NOT NULL,
    resource_type       VARCHAR(64)   NOT NULL,
    resource_id         VARCHAR(1024) NOT NULL,
    resource_relation   VARCHAR(128)  NOT NULL,
    subject_type        VARCHAR(64)   NOT NULL,
    subject_id          VARCHAR(1024) NOT NULL,
    subject_relation    VARCHAR(128)  NOT NULL DEFAULT '',
    operation           VARCHAR(16)   NOT NULL,
    version             BIGINT        NOT NULL,
    status              VARCHAR(16)   NOT NULL,
    attempts            INTEGER       NOT NULL,
    available_at        BIGINT        NOT NULL,
    locked_by           VARCHAR(64)   NOT NULL DEFAULT '',
    locked_at           BIGINT        NOT NULL DEFAULT 0,
    last_error          TEXT          NOT NULL DEFAULT '',
    zed_token           TEXT          NOT NULL DEFAULT '',
    created_at          BIGINT        NOT NULL,
    updated_at          BIGINT        NOT NULL,
    delivered_at        BIGINT        NOT NULL DEFAULT 0,
    UNIQUE (tenant_id, relationship_key)
);

CREATE INDEX idx_authz_outbox_pending ON authz_outbox (status, available_at);
CREATE INDEX idx_authz_outbox_locked ON authz_outbox (status, locked_at);

-- +goose Down
DROP TABLE authz_outbox;
DROP TABLE iam_role_members;
DROP TABLE iam_role_permissions;
DROP TABLE iam_roles;
