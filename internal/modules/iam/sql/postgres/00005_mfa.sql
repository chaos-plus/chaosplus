-- +goose Up
ALTER TABLE iam_credentials ADD COLUMN totp_last_used_step BIGINT NOT NULL DEFAULT 0;

CREATE TABLE iam_mfa_enrollments (
    principal_id VARCHAR(64) PRIMARY KEY REFERENCES iam_principals(id) ON DELETE CASCADE,
    secret_ciphertext TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL
);

CREATE TABLE iam_recovery_codes (
    principal_id VARCHAR(64) NOT NULL REFERENCES iam_principals(id) ON DELETE CASCADE,
    code_hash CHAR(64) NOT NULL,
    created_at BIGINT NOT NULL,
    used_at BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (principal_id, code_hash)
);
CREATE INDEX idx_iam_recovery_codes_available ON iam_recovery_codes (principal_id, used_at);

CREATE TABLE iam_mfa_challenges (
    id_hash CHAR(64) PRIMARY KEY,
    principal_id VARCHAR(64) NOT NULL REFERENCES iam_principals(id) ON DELETE CASCADE,
    return_url TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_iam_mfa_challenges_principal ON iam_mfa_challenges (principal_id, consumed_at, expires_at);

-- +goose Down
DROP TABLE iam_mfa_challenges;
DROP TABLE iam_recovery_codes;
DROP TABLE iam_mfa_enrollments;
ALTER TABLE iam_credentials DROP COLUMN totp_last_used_step;
