-- +goose Up
CREATE TABLE iam_roles (
    tenant_id    VARCHAR(128) NOT NULL,
    id           VARCHAR(32)  NOT NULL,
    name         VARCHAR(128) NOT NULL,
    description  TEXT         NOT NULL,
    created_at   BIGINT       NOT NULL,
    updated_at   BIGINT       NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE KEY uq_iam_roles_name (tenant_id, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE iam_role_permissions (
    tenant_id       VARCHAR(128) NOT NULL,
    role_id         VARCHAR(32)  NOT NULL,
    permission_code VARCHAR(128) NOT NULL,
    created_at      BIGINT       NOT NULL,
    PRIMARY KEY (tenant_id, role_id, permission_code),
    CONSTRAINT fk_iam_role_permissions_role FOREIGN KEY (tenant_id, role_id)
        REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE iam_role_members (
    tenant_id    VARCHAR(128) NOT NULL,
    role_id      VARCHAR(32)  NOT NULL,
    user_subject VARCHAR(255) NOT NULL,
    created_at   BIGINT       NOT NULL,
    PRIMARY KEY (tenant_id, role_id, user_subject),
    CONSTRAINT fk_iam_role_members_role FOREIGN KEY (tenant_id, role_id)
        REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE authz_outbox (
    id                  VARCHAR(32)   NOT NULL,
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
    attempts            INT           NOT NULL,
    available_at        BIGINT        NOT NULL,
    locked_by           VARCHAR(64)   NOT NULL DEFAULT '',
    locked_at           BIGINT        NOT NULL DEFAULT 0,
    last_error          TEXT          NOT NULL,
    zed_token           TEXT          NOT NULL,
    created_at          BIGINT        NOT NULL,
    updated_at          BIGINT        NOT NULL,
    delivered_at        BIGINT        NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    UNIQUE KEY uq_authz_outbox_relationship (tenant_id, relationship_key),
    KEY idx_authz_outbox_pending (status, available_at),
    KEY idx_authz_outbox_locked (status, locked_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE authz_outbox;
DROP TABLE iam_role_members;
DROP TABLE iam_role_permissions;
DROP TABLE iam_roles;
