-- +goose Up
CREATE TABLE workspace_test_runs (
 id BIGINT PRIMARY KEY CHECK(id>0), tenant_id BIGINT NOT NULL CHECK(tenant_id>0), entity_id BIGINT NOT NULL CHECK(entity_id>0), owner_id BIGINT NOT NULL CHECK(owner_id>0),
 test_case_id BIGINT NOT NULL CHECK(test_case_id>0), executor_id BIGINT NOT NULL CHECK(executor_id>0), workflow_run_id BIGINT NULL CHECK(workflow_run_id IS NULL OR workflow_run_id>0), environment VARCHAR(300) NOT NULL CHECK(char_length(environment) BETWEEN 1 AND 300),
 status VARCHAR(16) NOT NULL CHECK(status IN ('queued','running','passed','failed','blocked','cancelled')), started_at BIGINT NOT NULL DEFAULT 0 CHECK(started_at>=0), completed_at BIGINT NOT NULL DEFAULT 0 CHECK(completed_at>=0), observed_result TEXT NOT NULL, failure_summary TEXT NOT NULL,
 created_at BIGINT NOT NULL CHECK(created_at>0), created_by BIGINT NOT NULL CHECK(created_by>0), updated_at BIGINT NOT NULL CHECK(updated_at>0), updated_by BIGINT NOT NULL CHECK(updated_by>0), version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
 CHECK((status='queued' AND started_at=0 AND completed_at=0) OR (status='running' AND started_at>0 AND completed_at=0) OR (status IN ('passed','failed','blocked','cancelled') AND started_at>0 AND completed_at>=started_at)),
 CHECK(status<>'failed' OR char_length(failure_summary)>0), CONSTRAINT fk_workspace_test_run_case FOREIGN KEY(test_case_id) REFERENCES workspace_test_cases(id) ON DELETE RESTRICT, CONSTRAINT fk_workspace_test_run_workflow FOREIGN KEY(workflow_run_id) REFERENCES workflow_runs(id) ON DELETE RESTRICT,
 INDEX idx_workspace_test_runs_scope(tenant_id,entity_id,status,created_at), INDEX idx_workspace_test_runs_case(tenant_id,entity_id,test_case_id,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE IF EXISTS workspace_test_runs;
