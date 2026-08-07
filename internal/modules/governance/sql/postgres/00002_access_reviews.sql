-- +goose Up
CREATE TABLE iam_access_reviews (
    tenant_id    VARCHAR(128) NOT NULL,
    id           VARCHAR(64)  NOT NULL,
    name         VARCHAR(128) NOT NULL,
    owner_id     VARCHAR(255) NOT NULL,
    status       VARCHAR(16)  NOT NULL CHECK (status IN ('open', 'completed', 'cancelled')),
    due_at       BIGINT       NOT NULL,
    completed_at BIGINT       NOT NULL DEFAULT 0,
    created_at   BIGINT       NOT NULL,
    updated_at   BIGINT       NOT NULL,
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id) REFERENCES iam_tenants (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_access_reviews_status
    ON iam_access_reviews (tenant_id, status, due_at, created_at);

CREATE TABLE iam_access_review_items (
    tenant_id        VARCHAR(128) NOT NULL,
    review_id        VARCHAR(64)  NOT NULL,
    id               VARCHAR(64)  NOT NULL,
    principal_id     VARCHAR(255) NOT NULL,
    principal_name   VARCHAR(255) NOT NULL,
    role_id          VARCHAR(32)  NOT NULL,
    role_name        VARCHAR(128) NOT NULL,
    grant_type       VARCHAR(16)  NOT NULL CHECK (grant_type IN ('permanent', 'temporary')),
    grant_id         VARCHAR(64)  NOT NULL DEFAULT '',
    grant_created_at BIGINT       NOT NULL,
    grant_expires_at BIGINT       NOT NULL DEFAULT 0,
    decision         VARCHAR(16)  NOT NULL CHECK (decision IN ('pending', 'keep', 'revoke')),
    decided_by       VARCHAR(255) NOT NULL DEFAULT '',
    decision_note    VARCHAR(500) NOT NULL DEFAULT '',
    decided_at       BIGINT       NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, review_id, id),
    FOREIGN KEY (tenant_id, review_id) REFERENCES iam_access_reviews (tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_access_review_items_decision
    ON iam_access_review_items (tenant_id, review_id, decision, principal_id);

-- +goose Down
DROP TABLE IF EXISTS iam_access_review_items;
DROP TABLE IF EXISTS iam_access_reviews;
