-- +goose Up
ALTER TABLE workflow_runs ADD COLUMN context_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE workflow_runs ADD COLUMN workspace TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_runs ADD COLUMN runner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_runs ADD COLUMN instance_id TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_runs ADD COLUMN project_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE workflow_runs DROP COLUMN project_id;
ALTER TABLE workflow_runs DROP COLUMN instance_id;
ALTER TABLE workflow_runs DROP COLUMN runner_id;
ALTER TABLE workflow_runs DROP COLUMN workspace;
ALTER TABLE workflow_runs DROP COLUMN context_json;
