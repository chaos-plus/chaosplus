-- +goose Up
CREATE TABLE iam_email_verification_tokens (
    token_hmac CHAR(64) PRIMARY KEY,
    principal_id BIGINT NOT NULL REFERENCES iam_principals (id) ON DELETE CASCADE,
    email VARCHAR(320) NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_iam_email_verification_principal
    ON iam_email_verification_tokens (principal_id, consumed_at, expires_at);

-- +goose Down
DROP TABLE IF EXISTS iam_email_verification_tokens;