-- +goose Up
CREATE TABLE iam_stepup_challenges (
    id_hash TEXT NOT NULL PRIMARY KEY,
    principal_id TEXT NOT NULL,
    session_hash TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_stepup_challenges_principal ON iam_stepup_challenges (principal_id, consumed_at, expires_at);

-- +goose Down
DROP TABLE iam_stepup_challenges;
