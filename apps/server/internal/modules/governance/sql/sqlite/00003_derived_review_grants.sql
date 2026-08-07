-- +goose Up
CREATE TABLE iam_access_review_items_v2 (
    tenant_id        TEXT   NOT NULL,
    review_id        TEXT   NOT NULL,
    id               TEXT   NOT NULL,
    principal_id     TEXT   NOT NULL,
    principal_name   TEXT   NOT NULL,
    role_id          TEXT   NOT NULL,
    role_name        TEXT   NOT NULL,
    grant_type       TEXT   NOT NULL CHECK (grant_type IN ('permanent', 'temporary', 'group', 'position', 'entity', 'dynamic_group')),
    grant_id         TEXT   NOT NULL DEFAULT '',
    grant_created_at BIGINT NOT NULL,
    grant_expires_at BIGINT NOT NULL DEFAULT 0,
    decision         TEXT   NOT NULL CHECK (decision IN ('pending', 'keep', 'revoke')),
    decided_by       TEXT   NOT NULL DEFAULT '',
    decision_note    TEXT   NOT NULL DEFAULT '',
    decided_at       BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, review_id, id),
    FOREIGN KEY (tenant_id, review_id) REFERENCES iam_access_reviews (tenant_id, id) ON DELETE CASCADE
);
INSERT INTO iam_access_review_items_v2
    SELECT tenant_id, review_id, id, principal_id, principal_name, role_id, role_name, grant_type,
           grant_id, grant_created_at, grant_expires_at, decision, decided_by, decision_note, decided_at
    FROM iam_access_review_items;
DROP TABLE iam_access_review_items;
ALTER TABLE iam_access_review_items_v2 RENAME TO iam_access_review_items;
CREATE INDEX idx_iam_access_review_items_decision
    ON iam_access_review_items (tenant_id, review_id, decision, principal_id);

-- +goose Down
CREATE TABLE iam_access_review_items_v1 (
    tenant_id        TEXT   NOT NULL,
    review_id        TEXT   NOT NULL,
    id               TEXT   NOT NULL,
    principal_id     TEXT   NOT NULL,
    principal_name   TEXT   NOT NULL,
    role_id          TEXT   NOT NULL,
    role_name        TEXT   NOT NULL,
    grant_type       TEXT   NOT NULL CHECK (grant_type IN ('permanent', 'temporary')),
    grant_id         TEXT   NOT NULL DEFAULT '',
    grant_created_at BIGINT NOT NULL,
    grant_expires_at BIGINT NOT NULL DEFAULT 0,
    decision         TEXT   NOT NULL CHECK (decision IN ('pending', 'keep', 'revoke')),
    decided_by       TEXT   NOT NULL DEFAULT '',
    decision_note    TEXT   NOT NULL DEFAULT '',
    decided_at       BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, review_id, id),
    FOREIGN KEY (tenant_id, review_id) REFERENCES iam_access_reviews (tenant_id, id) ON DELETE CASCADE
);
INSERT INTO iam_access_review_items_v1
    SELECT tenant_id, review_id, id, principal_id, principal_name, role_id, role_name, grant_type,
           grant_id, grant_created_at, grant_expires_at, decision, decided_by, decision_note, decided_at
    FROM iam_access_review_items;
DROP TABLE iam_access_review_items;
ALTER TABLE iam_access_review_items_v1 RENAME TO iam_access_review_items;
CREATE INDEX idx_iam_access_review_items_decision
    ON iam_access_review_items (tenant_id, review_id, decision, principal_id);
