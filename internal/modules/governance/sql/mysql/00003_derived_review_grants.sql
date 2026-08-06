-- +goose Up
ALTER TABLE iam_access_review_items DROP CHECK chk_iam_access_review_items_grant_type;
ALTER TABLE iam_access_review_items ADD CONSTRAINT chk_iam_access_review_items_grant_type
    CHECK (grant_type IN ('permanent', 'temporary', 'group', 'position', 'entity', 'dynamic_group'));

-- +goose Down
ALTER TABLE iam_access_review_items DROP CHECK chk_iam_access_review_items_grant_type;
ALTER TABLE iam_access_review_items ADD CONSTRAINT chk_iam_access_review_items_grant_type
    CHECK (grant_type IN ('permanent', 'temporary'));
