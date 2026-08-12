-- +goose Up
CREATE TABLE iam_access_reviews (
    tenant_id BIGINT   NOT NULL,
    id BIGINT   NOT NULL,
    name        TEXT   NOT NULL,
    owner_id BIGINT   NOT NULL,
    status      TEXT   NOT NULL CHECK (status IN ('open', 'completed', 'cancelled')),
    due_at      BIGINT NOT NULL,
    completed_at BIGINT NOT NULL DEFAULT 0,
    created_at  BIGINT NOT NULL,
    updated_at  BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id) REFERENCES iam_tenants (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_access_reviews_status
    ON iam_access_reviews (tenant_id, status, due_at, created_at);

CREATE TABLE iam_access_review_items (
    tenant_id BIGINT   NOT NULL,
    review_id BIGINT   NOT NULL,
    id BIGINT   NOT NULL,
    principal_id BIGINT   NOT NULL,
    principal_name  TEXT   NOT NULL,
    role_id BIGINT   NOT NULL,
    role_name       TEXT   NOT NULL,
    grant_type      TEXT   NOT NULL CHECK (grant_type IN ('permanent', 'temporary', 'group', 'position', 'entity', 'dynamic_group', 'relationship')),
    grant_id BIGINT   NOT NULL DEFAULT '',
    grant_created_at BIGINT NOT NULL,
    grant_expires_at BIGINT NOT NULL DEFAULT 0,
    decision        TEXT   NOT NULL CHECK (decision IN ('pending', 'keep', 'revoke')),
    decided_by BIGINT   NOT NULL DEFAULT '',
    decision_note   TEXT   NOT NULL DEFAULT '',
    decided_at      BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, review_id, id),
    FOREIGN KEY (tenant_id, review_id) REFERENCES iam_access_reviews (tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_access_review_items_decision
    ON iam_access_review_items (tenant_id, review_id, decision, principal_id);

-- +goose Down
DROP TABLE IF EXISTS iam_access_review_items;
DROP TABLE IF EXISTS iam_access_reviews;