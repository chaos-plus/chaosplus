-- +goose Up
CREATE TABLE iam_access_requests (
    tenant_id          VARCHAR(128) NOT NULL,
    id                 VARCHAR(64)  NOT NULL,
    requester_id       VARCHAR(255) NOT NULL,
    role_id            VARCHAR(32)  NOT NULL,
    role_name          VARCHAR(128) NOT NULL,
    reason             VARCHAR(500) NOT NULL,
    snapshot_json      TEXT         NOT NULL,
    status             VARCHAR(16)  NOT NULL CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled', 'revoked')),
    access_expires_at  BIGINT       NOT NULL,
    request_expires_at BIGINT       NOT NULL,
    decided_by         VARCHAR(255) NOT NULL DEFAULT '',
    decision_note      VARCHAR(500) NOT NULL DEFAULT '',
    decided_at         BIGINT       NOT NULL DEFAULT 0,
    revoked_by         VARCHAR(255) NOT NULL DEFAULT '',
    revoke_reason      VARCHAR(500) NOT NULL DEFAULT '',
    revoked_at         BIGINT       NOT NULL DEFAULT 0,
    created_at         BIGINT       NOT NULL,
    updated_at         BIGINT       NOT NULL,
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id) REFERENCES iam_tenants (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_access_requests_queue
    ON iam_access_requests (tenant_id, status, created_at);
CREATE INDEX idx_iam_access_requests_requester
    ON iam_access_requests (tenant_id, requester_id, created_at);

CREATE TABLE iam_approval_steps (
    tenant_id  VARCHAR(128) NOT NULL,
    request_id VARCHAR(64)  NOT NULL,
    step       INTEGER      NOT NULL,
    decision   VARCHAR(16)  NOT NULL CHECK (decision IN ('pending', 'approved', 'rejected')),
    decided_by VARCHAR(255) NOT NULL DEFAULT '',
    decided_at BIGINT       NOT NULL DEFAULT 0,
    note       VARCHAR(500) NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, request_id, step),
    FOREIGN KEY (tenant_id, request_id) REFERENCES iam_access_requests (tenant_id, id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS iam_approval_steps;
DROP TABLE IF EXISTS iam_access_requests;
