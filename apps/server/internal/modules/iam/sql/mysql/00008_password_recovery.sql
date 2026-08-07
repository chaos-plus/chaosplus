-- +goose Up
ALTER TABLE iam_credentials
    ADD COLUMN credential_version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN recovery_cooldown_until BIGINT NOT NULL DEFAULT 0;
ALTER TABLE iam_principals ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE iam_password_recovery_tokens (
    token_hmac CHAR(64) NOT NULL PRIMARY KEY,
    principal_id VARCHAR(255) NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT fk_iam_password_recovery_principal FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE,
    INDEX idx_iam_password_recovery_principal (principal_id, consumed_at, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_notification_outbox (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    kind VARCHAR(64) NOT NULL,
    recipient VARCHAR(320) NOT NULL,
    payload_ciphertext TEXT NOT NULL,
    status VARCHAR(16) NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    available_at BIGINT NOT NULL,
    locked_at BIGINT NOT NULL DEFAULT 0,
    sent_at BIGINT NOT NULL DEFAULT 0,
    last_error VARCHAR(512) NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    CONSTRAINT chk_iam_notification_outbox_status CHECK (status IN ('pending', 'delivering', 'sent', 'failed')),
    INDEX idx_iam_notification_outbox_pending (status, available_at, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS iam_notification_outbox;
DROP TABLE IF EXISTS iam_password_recovery_tokens;
ALTER TABLE iam_principals DROP COLUMN email_verified;
ALTER TABLE iam_credentials
    DROP COLUMN recovery_cooldown_until,
    DROP COLUMN credential_version;
