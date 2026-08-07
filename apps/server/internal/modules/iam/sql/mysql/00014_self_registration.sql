-- +goose Up
ALTER TABLE iam_principals ADD COLUMN activation_required BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE iam_principals DROP COLUMN activation_required;
