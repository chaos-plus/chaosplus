-- +goose Up
ALTER TABLE iam_role_permissions ADD COLUMN condition_json VARCHAR(4096) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE iam_role_permissions DROP COLUMN condition_json;