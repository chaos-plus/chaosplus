-- +goose Up
ALTER TABLE iam_credentials ADD COLUMN credential_version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE iam_credentials ADD COLUMN recovery_cooldown_until BIGINT NOT NULL DEFAULT 0;
ALTER TABLE iam_principals ADD COLUMN email_verified INTEGER NOT NULL DEFAULT 0;

CREATE TABLE iam_password_recovery_tokens (
    token_hmac TEXT NOT NULL PRIMARY KEY,
    principal_id TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT NOT NULL DEFAULT 0,
    FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_password_recovery_principal ON iam_password_recovery_tokens (principal_id, consumed_at, expires_at);

CREATE TABLE iam_notification_outbox (
    id TEXT NOT NULL PRIMARY KEY,
    kind TEXT NOT NULL,
    recipient TEXT NOT NULL,
    payload_ciphertext TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'delivering', 'sent', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at BIGINT NOT NULL,
    locked_at BIGINT NOT NULL DEFAULT 0,
    sent_at BIGINT NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);
CREATE INDEX idx_iam_notification_outbox_pending ON iam_notification_outbox (status, available_at, created_at);

-- +goose Down
DROP TABLE IF EXISTS iam_notification_outbox;
DROP TABLE IF EXISTS iam_password_recovery_tokens;
ALTER TABLE iam_principals DROP COLUMN email_verified;
ALTER TABLE iam_credentials DROP COLUMN recovery_cooldown_until;
ALTER TABLE iam_credentials DROP COLUMN credential_version;
