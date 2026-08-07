-- +goose Up
ALTER TABLE iam_principals ADD COLUMN activation_required INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE iam_principals DROP COLUMN activation_required;
