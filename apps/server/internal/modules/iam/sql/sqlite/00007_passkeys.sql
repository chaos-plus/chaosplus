-- +goose Up
CREATE TABLE iam_passkey_users (
    principal_id BIGINT NOT NULL PRIMARY KEY,
    user_handle TEXT NOT NULL UNIQUE,
    created_at BIGINT NOT NULL,
    FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE
);

CREATE TABLE iam_passkeys (
    id_hash TEXT NOT NULL PRIMARY KEY,
    principal_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    credential_ciphertext TEXT NOT NULL,
    sign_count BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    last_used_at BIGINT NOT NULL DEFAULT 0,
    FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_passkeys_principal ON iam_passkeys (principal_id, created_at);

CREATE TABLE iam_passkey_challenges (
    id_hash TEXT NOT NULL PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('registration', 'login')),
    principal_id BIGINT NOT NULL DEFAULT '',
    return_url TEXT NOT NULL DEFAULT '',
    session_data TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_iam_passkey_challenges_expiry ON iam_passkey_challenges (expires_at, consumed_at);

-- +goose Down
DROP TABLE IF EXISTS iam_passkey_challenges;
DROP TABLE IF EXISTS iam_passkeys;
DROP TABLE IF EXISTS iam_passkey_users;