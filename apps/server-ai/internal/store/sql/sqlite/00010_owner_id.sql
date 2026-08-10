-- +goose Up
-- agent / machine 属于「人 + 实体」:owner_id 是创建它的 human,entity_id 是归属实体。
ALTER TABLE agent_specs ADD COLUMN owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE machine_runners ADD COLUMN owner_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE machine_runners DROP COLUMN owner_id;
ALTER TABLE agent_specs DROP COLUMN owner_id;
