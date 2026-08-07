-- +goose Up
ALTER TABLE iam_access_review_items DROP CONSTRAINT iam_access_review_items_grant_type_check;
ALTER TABLE iam_access_review_items ADD CONSTRAINT iam_access_review_items_grant_type_check
    CHECK (grant_type IN ('permanent', 'temporary', 'group', 'position', 'entity', 'dynamic_group'));

-- +goose Down
ALTER TABLE iam_access_review_items DROP CONSTRAINT iam_access_review_items_grant_type_check;
ALTER TABLE iam_access_review_items ADD CONSTRAINT iam_access_review_items_grant_type_check
    CHECK (grant_type IN ('permanent', 'temporary'));
