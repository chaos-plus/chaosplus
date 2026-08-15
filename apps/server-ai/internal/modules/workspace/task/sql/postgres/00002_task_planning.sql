-- +goose Up
ALTER TABLE workspace_tasks ADD COLUMN priority VARCHAR(16) NOT NULL DEFAULT 'medium' CHECK(priority IN ('highest','high','medium','low','lowest'));
ALTER TABLE workspace_tasks ADD COLUMN due_at BIGINT NOT NULL DEFAULT 0 CHECK(due_at>=0);
CREATE INDEX idx_workspace_tasks_planning ON workspace_tasks(tenant_id,entity_id,deleted_at,priority,due_at);
-- +goose Down
DROP INDEX IF EXISTS idx_workspace_tasks_planning;
ALTER TABLE workspace_tasks DROP COLUMN due_at;
ALTER TABLE workspace_tasks DROP COLUMN priority;
