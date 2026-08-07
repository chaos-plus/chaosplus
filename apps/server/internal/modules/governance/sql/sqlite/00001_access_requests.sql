-- +goose Up
CREATE TABLE iam_access_requests (
    tenant_id         TEXT   NOT NULL,
    id                TEXT   NOT NULL,
    requester_id      TEXT   NOT NULL,
    role_id           TEXT   NOT NULL,
    role_name         TEXT   NOT NULL,
    reason            TEXT   NOT NULL,
    snapshot_json     TEXT   NOT NULL,
    status            TEXT   NOT NULL CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled', 'revoked')),
    access_expires_at BIGINT NOT NULL,
    request_expires_at BIGINT NOT NULL,
    decided_by        TEXT   NOT NULL DEFAULT '',
    decision_note     TEXT   NOT NULL DEFAULT '',
    decided_at        BIGINT NOT NULL DEFAULT 0,
    revoked_by        TEXT   NOT NULL DEFAULT '',
    revoke_reason     TEXT   NOT NULL DEFAULT '',
    revoked_at        BIGINT NOT NULL DEFAULT 0,
    created_at        BIGINT NOT NULL,
    updated_at        BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id) REFERENCES iam_tenants (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_access_requests_queue
    ON iam_access_requests (tenant_id, status, created_at);
CREATE INDEX idx_iam_access_requests_requester
    ON iam_access_requests (tenant_id, requester_id, created_at);

CREATE TABLE iam_approval_steps (
    tenant_id  TEXT   NOT NULL,
    request_id TEXT   NOT NULL,
    step       INTEGER NOT NULL,
    decision   TEXT   NOT NULL CHECK (decision IN ('pending', 'approved', 'rejected')),
    decided_by TEXT   NOT NULL DEFAULT '',
    decided_at BIGINT NOT NULL DEFAULT 0,
    note       TEXT   NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, request_id, step),
    FOREIGN KEY (tenant_id, request_id) REFERENCES iam_access_requests (tenant_id, id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS iam_approval_steps;
DROP TABLE IF EXISTS iam_access_requests;
