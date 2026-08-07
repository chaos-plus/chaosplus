-- +goose Up
ALTER TABLE iam_relationships ADD COLUMN condition_json VARCHAR(4096) NOT NULL DEFAULT '';
ALTER TABLE iam_resource_relationships ADD COLUMN condition_json VARCHAR(4096) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE iam_resource_relationships DROP COLUMN condition_json;
ALTER TABLE iam_relationships DROP COLUMN condition_json;
