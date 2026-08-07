-- +goose Up
ALTER TABLE iam_sessions ADD COLUMN auth_time BIGINT NOT NULL DEFAULT 0;
ALTER TABLE iam_sessions ADD COLUMN acr INTEGER NOT NULL DEFAULT 0;
ALTER TABLE iam_sessions ADD COLUMN amr TEXT NOT NULL DEFAULT '';
UPDATE iam_sessions SET auth_time = created_at WHERE auth_time = 0;
ALTER TABLE iam_oauth_codes ADD COLUMN auth_time BIGINT NOT NULL DEFAULT 0;
ALTER TABLE iam_oauth_codes ADD COLUMN acr INTEGER NOT NULL DEFAULT 0;
ALTER TABLE iam_oauth_codes ADD COLUMN amr TEXT NOT NULL DEFAULT '';
ALTER TABLE iam_refresh_tokens ADD COLUMN auth_time BIGINT NOT NULL DEFAULT 0;
ALTER TABLE iam_refresh_tokens ADD COLUMN acr INTEGER NOT NULL DEFAULT 0;
ALTER TABLE iam_refresh_tokens ADD COLUMN amr TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE iam_refresh_tokens DROP COLUMN amr;
ALTER TABLE iam_refresh_tokens DROP COLUMN acr;
ALTER TABLE iam_refresh_tokens DROP COLUMN auth_time;
ALTER TABLE iam_oauth_codes DROP COLUMN amr;
ALTER TABLE iam_oauth_codes DROP COLUMN acr;
ALTER TABLE iam_oauth_codes DROP COLUMN auth_time;
ALTER TABLE iam_sessions DROP COLUMN amr;
ALTER TABLE iam_sessions DROP COLUMN acr;
ALTER TABLE iam_sessions DROP COLUMN auth_time;
