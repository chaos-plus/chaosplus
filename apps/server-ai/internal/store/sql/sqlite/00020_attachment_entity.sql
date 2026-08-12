-- +goose Up
ALTER TABLE attachments ADD COLUMN entity_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_attachments_entity_owner ON attachments(entity_id, owner_type, owner_id);

-- +goose Down
DROP INDEX idx_attachments_entity_owner;
ALTER TABLE attachments DROP COLUMN entity_id;
