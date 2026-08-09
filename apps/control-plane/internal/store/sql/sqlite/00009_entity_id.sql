-- +goose Up
-- 实体隔离(PRD:注册用户=租户,租户下多个 instance 实体;machine/频道/工作项等归实体)。
ALTER TABLE channels ADD COLUMN entity_id TEXT NOT NULL DEFAULT '';
ALTER TABLE work_items ADD COLUMN entity_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_specs ADD COLUMN entity_id TEXT NOT NULL DEFAULT '';
ALTER TABLE okrs ADD COLUMN entity_id TEXT NOT NULL DEFAULT '';
ALTER TABLE machine_runners ADD COLUMN entity_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_channels_entity ON channels(entity_id);
CREATE INDEX idx_work_items_entity ON work_items(entity_id);
CREATE INDEX idx_agent_specs_entity ON agent_specs(entity_id);
CREATE INDEX idx_okrs_entity ON okrs(entity_id);
CREATE INDEX idx_machine_runners_entity ON machine_runners(entity_id);

-- +goose Down
DROP INDEX idx_machine_runners_entity;
DROP INDEX idx_okrs_entity;
DROP INDEX idx_agent_specs_entity;
DROP INDEX idx_work_items_entity;
DROP INDEX idx_channels_entity;
ALTER TABLE machine_runners DROP COLUMN entity_id;
ALTER TABLE okrs DROP COLUMN entity_id;
ALTER TABLE agent_specs DROP COLUMN entity_id;
ALTER TABLE work_items DROP COLUMN entity_id;
ALTER TABLE channels DROP COLUMN entity_id;
