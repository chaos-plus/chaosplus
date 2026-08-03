-- +goose Up
CREATE TABLE iam_passkey_users (
    principal_id VARCHAR(255) NOT NULL PRIMARY KEY,
    user_handle VARCHAR(86) NOT NULL UNIQUE,
    created_at BIGINT NOT NULL,
    CONSTRAINT fk_iam_passkey_users_principal FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE iam_passkeys (
    id_hash CHAR(64) NOT NULL PRIMARY KEY,
    principal_id VARCHAR(255) NOT NULL,
    name VARCHAR(200) NOT NULL,
    credential_ciphertext TEXT NOT NULL,
    sign_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    last_used_at BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT fk_iam_passkeys_principal FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE,
    INDEX idx_iam_passkeys_principal (principal_id, created_at)
) ENGINE=InnoDB;

CREATE TABLE iam_passkey_challenges (
    id_hash CHAR(64) NOT NULL PRIMARY KEY,
    kind VARCHAR(16) NOT NULL,
    principal_id VARCHAR(255) NOT NULL DEFAULT '',
    return_url VARCHAR(2048) NOT NULL DEFAULT '',
    session_data TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT chk_iam_passkey_challenges_kind CHECK (kind IN ('registration', 'login')),
    INDEX idx_iam_passkey_challenges_expiry (expires_at, consumed_at)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE IF EXISTS iam_passkey_challenges;
DROP TABLE IF EXISTS iam_passkeys;
DROP TABLE IF EXISTS iam_passkey_users;
