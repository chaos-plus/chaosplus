-- +goose Up
ALTER TABLE iam_credentials ADD COLUMN totp_last_used_step BIGINT NOT NULL DEFAULT 0;

CREATE TABLE iam_mfa_enrollments (
    principal_id VARCHAR(64) PRIMARY KEY,
    secret_ciphertext TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    CONSTRAINT fk_iam_mfa_enrollment_principal FOREIGN KEY (principal_id) REFERENCES iam_principals(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_recovery_codes (
    principal_id VARCHAR(64) NOT NULL,
    code_hash CHAR(64) NOT NULL,
    created_at BIGINT NOT NULL,
    used_at BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (principal_id, code_hash),
    KEY idx_iam_recovery_codes_available (principal_id, used_at),
    CONSTRAINT fk_iam_recovery_principal FOREIGN KEY (principal_id) REFERENCES iam_principals(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_mfa_challenges (
    id_hash CHAR(64) PRIMARY KEY,
    principal_id VARCHAR(64) NOT NULL,
    return_url TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT NOT NULL DEFAULT 0,
    attempts INT NOT NULL DEFAULT 0,
    KEY idx_iam_mfa_challenges_principal (principal_id, consumed_at, expires_at),
    CONSTRAINT fk_iam_mfa_challenge_principal FOREIGN KEY (principal_id) REFERENCES iam_principals(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE iam_mfa_challenges;
DROP TABLE iam_recovery_codes;
DROP TABLE iam_mfa_enrollments;
ALTER TABLE iam_credentials DROP COLUMN totp_last_used_step;
