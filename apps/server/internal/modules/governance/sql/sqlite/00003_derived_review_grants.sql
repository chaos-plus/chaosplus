-- +goose Up
CREATE INDEX idx_iam_access_review_items_grant ON iam_access_review_items (tenant_id, review_id, grant_type, grant_id);

-- +goose Down
DROP INDEX idx_iam_access_review_items_grant;