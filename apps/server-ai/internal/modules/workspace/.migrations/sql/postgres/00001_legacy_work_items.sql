-- +goose Up
CREATE TABLE workspace_legacy_migration_guard (ok INTEGER NOT NULL CHECK(ok = 1));
INSERT INTO workspace_legacy_migration_guard
SELECT 0 FROM workspace_tasks
WHERE kind = 'test' AND (parent_id IS NOT NULL OR channel_id IS NOT NULL OR workflow_run_id IS NOT NULL OR workflow_id IS NOT NULL OR project_id IS NOT NULL OR workspace <> '' OR estimate_ms <> 0 OR spent_ms <> 0 OR progress <> 0)
LIMIT 1;
INSERT INTO workspace_legacy_migration_guard
SELECT 0 FROM workspace_tasks
WHERE kind = 'bug' AND (channel_id IS NOT NULL OR workflow_run_id IS NOT NULL OR workflow_id IS NOT NULL OR project_id IS NOT NULL OR workspace <> '' OR estimate_ms <> 0 OR spent_ms <> 0 OR progress <> 0 OR (requirement_id IS NULL AND parent_id IS NULL) OR (parent_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM workspace_tasks parent WHERE parent.id = workspace_tasks.parent_id AND parent.kind = 'task')))
LIMIT 1;
INSERT INTO workspace_legacy_migration_guard
SELECT 0 FROM workspace_tasks
WHERE kind = 'task' AND parent_id IS NOT NULL AND EXISTS (SELECT 1 FROM workspace_tasks parent WHERE parent.id = workspace_tasks.parent_id AND parent.kind <> 'task')
LIMIT 1;
DROP TABLE workspace_legacy_migration_guard;

UPDATE workspace_attachments SET resource_type = 'testcase'
WHERE resource_type = 'task' AND resource_id IN (SELECT id FROM workspace_tasks WHERE kind = 'test');
UPDATE workspace_attachments SET resource_type = 'defect'
WHERE resource_type = 'task' AND resource_id IN (SELECT id FROM workspace_tasks WHERE kind = 'bug');

INSERT INTO workspace_test_cases (id, tenant_id, entity_id, owner_id, requirement_id, title, description, preconditions, priority, status, assignee_id, created_at, created_by, updated_at, updated_by, deleted_at, deleted_by, version)
SELECT id, tenant_id, entity_id, owner_id, requirement_id, title, description, '', 'medium', CASE WHEN status IN ('done', 'cancelled') THEN 'retired' ELSE 'draft' END, assignee_id, created_at, created_by, updated_at, updated_by, deleted_at, deleted_by, version
FROM workspace_tasks WHERE kind = 'test';

INSERT INTO workspace_defects (id, tenant_id, entity_id, owner_id, requirement_id, task_id, test_case_id, test_run_id, title, description, reproduction_steps, expected_result, actual_result, severity, priority, status, resolution, resolution_note, assignee_id, created_at, created_by, updated_at, updated_by, deleted_at, deleted_by, version)
SELECT id, tenant_id, entity_id, owner_id, requirement_id, parent_id, NULL, NULL, title, description,
       CASE WHEN description <> '' THEN description ELSE title END,
       'Not recorded in the legacy work item.', 'Not recorded in the legacy work item.', 'major', 'medium',
       CASE status WHEN 'in_progress' THEN 'in_progress' WHEN 'review' THEN 'triaged' WHEN 'done' THEN 'resolved' WHEN 'cancelled' THEN 'rejected' ELSE 'open' END,
       CASE WHEN status = 'done' THEN 'fixed' ELSE '' END,
       CASE WHEN status = 'done' THEN 'Migrated from a completed legacy bug; verification is required.' ELSE '' END,
       assignee_id, created_at, created_by, updated_at, updated_by, deleted_at, deleted_by, version
FROM workspace_tasks WHERE kind = 'bug';

DELETE FROM workspace_tasks WHERE kind IN ('test', 'bug');
ALTER TABLE workspace_tasks DROP COLUMN kind;

-- +goose Down
ALTER TABLE workspace_tasks ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'task' CHECK(kind = 'task');
