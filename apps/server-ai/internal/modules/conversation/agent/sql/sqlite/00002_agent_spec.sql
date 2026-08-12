-- +goose Up
ALTER TABLE conversation_agents ADD COLUMN spec_json TEXT NOT NULL DEFAULT '{}' CHECK(length(spec_json) <= 65535);
-- +goose Down
ALTER TABLE conversation_agents DROP COLUMN spec_json;
