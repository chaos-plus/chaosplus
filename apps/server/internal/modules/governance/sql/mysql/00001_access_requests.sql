-- +goose Up
CREATE TABLE iam_access_requests (
    tenant_id BIGINT NOT NULL,
    id BIGINT  NOT NULL,
    requester_id BIGINT NOT NULL,
    role_id BIGINT  NOT NULL,
    role_name          VARCHAR(128) NOT NULL,
    reason             VARCHAR(500) NOT NULL,
    snapshot_json      TEXT         NOT NULL,
    status             VARCHAR(16)  NOT NULL,
    access_expires_at  BIGINT       NOT NULL,
    request_expires_at BIGINT       NOT NULL,
    decided_by BIGINT NOT NULL DEFAULT '',
    decision_note      VARCHAR(500) NOT NULL DEFAULT '',
    decided_at         BIGINT       NOT NULL DEFAULT 0,
    revoked_by BIGINT NOT NULL DEFAULT '',
    revoke_reason      VARCHAR(500) NOT NULL DEFAULT '',
    revoked_at         BIGINT       NOT NULL DEFAULT 0,
    created_at         BIGINT       NOT NULL,
    updated_at         BIGINT       NOT NULL,
    PRIMARY KEY (tenant_id, id),
    KEY idx_iam_access_requests_queue (tenant_id, status, created_at),
    KEY idx_iam_access_requests_requester (tenant_id, requester_id, created_at),
    CONSTRAINT fk_iam_access_requests_tenant FOREIGN KEY (tenant_id)
        REFERENCES iam_tenants (id) ON DELETE CASCADE,
    CONSTRAINT chk_iam_access_requests_status CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled', 'revoked'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_approval_steps (
    tenant_id BIGINT NOT NULL,
    request_id BIGINT  NOT NULL,
    step       INTEGER      NOT NULL,
    decision   VARCHAR(16)  NOT NULL,
    decided_by BIGINT NOT NULL DEFAULT '',
    decided_at BIGINT       NOT NULL DEFAULT 0,
    note       VARCHAR(500) NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, request_id, step),
    CONSTRAINT fk_iam_approval_steps_request FOREIGN KEY (tenant_id, request_id)
        REFERENCES iam_access_requests (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT chk_iam_approval_steps_decision CHECK (decision IN ('pending', 'approved', 'rejected'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS iam_approval_steps;
DROP TABLE IF EXISTS iam_access_requests;