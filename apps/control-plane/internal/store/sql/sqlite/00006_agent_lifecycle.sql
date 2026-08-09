-- +goose Up
-- PRD D.4:数字人需要描述、所属 machine、生命周期状态与默认频道。
ALTER TABLE agent_specs ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_specs ADD COLUMN machine_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_specs ADD COLUMN status TEXT NOT NULL DEFAULT 'stopped';  -- running | stopped | retired
ALTER TABLE agent_specs ADD COLUMN default_channels TEXT NOT NULL DEFAULT '';
-- 注销交接文档(§6.2.1 正常注销的产出)。
ALTER TABLE agent_specs ADD COLUMN handover_doc TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_specs ADD COLUMN retired_at BIGINT NOT NULL DEFAULT 0;

CREATE INDEX idx_agent_specs_machine ON agent_specs(machine_id);

-- +goose Down
DROP INDEX idx_agent_specs_machine;
ALTER TABLE agent_specs DROP COLUMN retired_at;
ALTER TABLE agent_specs DROP COLUMN handover_doc;
ALTER TABLE agent_specs DROP COLUMN default_channels;
ALTER TABLE agent_specs DROP COLUMN status;
ALTER TABLE agent_specs DROP COLUMN machine_id;
ALTER TABLE agent_specs DROP COLUMN description;
