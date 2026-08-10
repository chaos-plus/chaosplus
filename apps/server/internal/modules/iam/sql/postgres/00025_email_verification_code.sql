-- +goose Up
ALTER TABLE iam_email_verification_tokens ADD COLUMN code TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE iam_email_verification_tokens DROP COLUMN code;
