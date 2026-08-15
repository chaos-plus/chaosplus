-- +goose Up
ALTER TABLE workspace_tasks ADD COLUMN priority VARCHAR(16) NOT NULL DEFAULT 'medium' CHECK(priority IN ('highest','high','medium','low','lowest')), ADD COLUMN due_at BIGINT NOT NULL DEFAULT 0 CHECK(due_at>=0), ADD INDEX idx_workspace_tasks_planning(tenant_id,entity_id,deleted_at,priority,due_at);
-- +goose Down
ALTER TABLE workspace_tasks DROP INDEX idx_workspace_tasks_planning, DROP COLUMN due_at, DROP COLUMN priority;
