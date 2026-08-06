-- +goose Up
CREATE TABLE iam_stepup_challenges (
    id_hash CHAR(64) PRIMARY KEY,
    principal_id VARCHAR(64) NOT NULL REFERENCES iam_principals(id) ON DELETE CASCADE,
    session_hash CHAR(64) NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_iam_stepup_challenges_principal ON iam_stepup_challenges (principal_id, consumed_at, expires_at);

-- +goose Down
DROP TABLE iam_stepup_challenges;
