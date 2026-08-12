-- +goose Up
CREATE TABLE iam_stepup_challenges (
    id_hash CHAR(64) PRIMARY KEY,
    principal_id BIGINT NOT NULL,
    session_hash CHAR(64) NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT NOT NULL DEFAULT 0,
    attempts INT NOT NULL DEFAULT 0,
    KEY idx_iam_stepup_challenges_principal (principal_id, consumed_at, expires_at),
    CONSTRAINT fk_iam_stepup_challenge_principal FOREIGN KEY (principal_id) REFERENCES iam_principals(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE iam_stepup_challenges;