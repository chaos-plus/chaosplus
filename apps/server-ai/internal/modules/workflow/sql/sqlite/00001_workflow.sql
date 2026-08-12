-- +goose Up
CREATE TABLE workflows (
 id BIGINT PRIMARY KEY CHECK(id>0), tenant_id BIGINT NOT NULL CHECK(tenant_id>0), entity_id BIGINT NOT NULL CHECK(entity_id>0), owner_id BIGINT NOT NULL CHECK(owner_id>0),
 workflow_key TEXT NOT NULL CHECK(length(workflow_key) BETWEEN 1 AND 255), revision BIGINT NOT NULL CHECK(revision>=1), name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 255), def_json TEXT NOT NULL CHECK(json_valid(def_json)),
 created_at BIGINT NOT NULL CHECK(created_at>0), created_by BIGINT NOT NULL CHECK(created_by>0), updated_at BIGINT NOT NULL CHECK(updated_at>0), updated_by BIGINT NOT NULL CHECK(updated_by>0),
 deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at>=0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by>=0), version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
 UNIQUE(tenant_id,entity_id,workflow_key,revision), CHECK((deleted_at=0 AND deleted_by=0) OR (deleted_at>0 AND deleted_by>0))
);
CREATE INDEX idx_workflows_scope ON workflows(tenant_id,entity_id,deleted_at,updated_at,id);
CREATE TABLE workflow_runs (
 id BIGINT PRIMARY KEY CHECK(id>0), tenant_id BIGINT NOT NULL CHECK(tenant_id>0), entity_id BIGINT NOT NULL CHECK(entity_id>0), project_id BIGINT NOT NULL CHECK(project_id>0), owner_id BIGINT NOT NULL CHECK(owner_id>0),
 def_json TEXT NOT NULL CHECK(json_valid(def_json)), status TEXT NOT NULL CHECK(status IN ('running','waiting_approval','completed','failed','paused','cancelled')), context_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(context_json)),
 workspace TEXT NOT NULL, runner_handle TEXT NOT NULL DEFAULT '', created_at BIGINT NOT NULL CHECK(created_at>0), created_by BIGINT NOT NULL CHECK(created_by>0), updated_at BIGINT NOT NULL CHECK(updated_at>0), updated_by BIGINT NOT NULL CHECK(updated_by>0),
 deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at>=0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by>=0), version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1), CHECK((deleted_at=0 AND deleted_by=0) OR (deleted_at>0 AND deleted_by>0))
);
CREATE INDEX idx_workflow_runs_scope ON workflow_runs(tenant_id,entity_id,project_id,deleted_at,status,created_at,id);
CREATE TABLE workflow_events (
 seq INTEGER PRIMARY KEY AUTOINCREMENT, id BIGINT NOT NULL UNIQUE CHECK(id>0), tenant_id BIGINT NOT NULL CHECK(tenant_id>0), entity_id BIGINT NOT NULL CHECK(entity_id>0), project_id BIGINT NOT NULL CHECK(project_id>0), actor_id BIGINT NOT NULL CHECK(actor_id>0), run_id BIGINT NOT NULL CHECK(run_id>0),
 ts BIGINT NOT NULL CHECK(ts>0), type TEXT NOT NULL, idempotency_key TEXT NOT NULL UNIQUE, schema_version BIGINT NOT NULL DEFAULT 1 CHECK(schema_version>=1), payload_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(payload_json))
);
CREATE INDEX idx_workflow_events_run ON workflow_events(tenant_id,entity_id,run_id,seq);
CREATE TABLE node_executions (
 tenant_id BIGINT NOT NULL CHECK(tenant_id>0), entity_id BIGINT NOT NULL CHECK(entity_id>0), run_id BIGINT NOT NULL CHECK(run_id>0), node_key TEXT NOT NULL, attempt INTEGER NOT NULL CHECK(attempt>=1),
 status TEXT NOT NULL CHECK(status IN ('pending','running','completed','failed','skipped','waiting_approval','retrying','paused_for_human')), started_at BIGINT NOT NULL DEFAULT 0 CHECK(started_at>=0), completed_at BIGINT NOT NULL DEFAULT 0 CHECK(completed_at>=0), error TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,entity_id,run_id,node_key,attempt), FOREIGN KEY(run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
);
CREATE TABLE run_leases (
 tenant_id BIGINT NOT NULL CHECK(tenant_id>0), entity_id BIGINT NOT NULL CHECK(entity_id>0), run_id BIGINT NOT NULL CHECK(run_id>0), holder_id BIGINT NOT NULL CHECK(holder_id>0), fencing_token BIGINT NOT NULL CHECK(fencing_token>=1), expires_at BIGINT NOT NULL CHECK(expires_at>0), updated_at BIGINT NOT NULL CHECK(updated_at>0),
 PRIMARY KEY(tenant_id,entity_id,run_id), FOREIGN KEY(run_id) REFERENCES workflow_runs(id) ON DELETE CASCADE
);
CREATE INDEX idx_run_leases_expiry ON run_leases(expires_at);
-- +goose Down
DROP TABLE IF EXISTS run_leases; DROP TABLE IF EXISTS node_executions; DROP TABLE IF EXISTS workflow_events; DROP TABLE IF EXISTS workflow_runs; DROP TABLE IF EXISTS workflows;
