-- +goose Up
CREATE TABLE IF NOT EXISTS email_templates (
    template_key TEXT NOT NULL PRIMARY KEY,
    subject      TEXT NOT NULL DEFAULT '',
    html_body    TEXT NOT NULL DEFAULT '',
    updated_at   INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE IF EXISTS email_templates;
