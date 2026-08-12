-- +goose Up
CREATE TABLE iam_access_reviews (
    tenant_id BIGINT NOT NULL,
    id BIGINT  NOT NULL,
    name         VARCHAR(128) NOT NULL,
    owner_id BIGINT NOT NULL,
    status       VARCHAR(16)  NOT NULL,
    due_at       BIGINT       NOT NULL,
    completed_at BIGINT       NOT NULL DEFAULT 0,
    created_at   BIGINT       NOT NULL,
    updated_at   BIGINT       NOT NULL,
    PRIMARY KEY (tenant_id, id),
    KEY idx_iam_access_reviews_status (tenant_id, status, due_at, created_at),
    CONSTRAINT fk_iam_access_reviews_tenant FOREIGN KEY (tenant_id)
        REFERENCES iam_tenants (id) ON DELETE CASCADE,
    CONSTRAINT chk_iam_access_reviews_status CHECK (status IN ('open', 'completed', 'cancelled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_access_review_items (
    tenant_id BIGINT NOT NULL,
    review_id BIGINT  NOT NULL,
    id BIGINT  NOT NULL,
    principal_id BIGINT NOT NULL,
    principal_name   VARCHAR(255) NOT NULL,
    role_id BIGINT  NOT NULL,
    role_name        VARCHAR(128) NOT NULL,
    grant_type       VARCHAR(16)  NOT NULL,
    grant_id BIGINT  NOT NULL DEFAULT '',
    grant_created_at BIGINT       NOT NULL,
    grant_expires_at BIGINT       NOT NULL DEFAULT 0,
    decision         VARCHAR(16)  NOT NULL,
    decided_by BIGINT NOT NULL DEFAULT '',
    decision_note    VARCHAR(500) NOT NULL DEFAULT '',
    decided_at       BIGINT       NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, review_id, id),
    KEY idx_iam_access_review_items_decision (tenant_id, review_id, decision, principal_id),
    CONSTRAINT fk_iam_access_review_items_review FOREIGN KEY (tenant_id, review_id)
        REFERENCES iam_access_reviews (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT chk_iam_access_review_items_grant_type CHECK (grant_type IN ('permanent', 'temporary')),
    CONSTRAINT chk_iam_access_review_items_decision CHECK (decision IN ('pending', 'keep', 'revoke'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS iam_access_review_items;
DROP TABLE IF EXISTS iam_access_reviews;