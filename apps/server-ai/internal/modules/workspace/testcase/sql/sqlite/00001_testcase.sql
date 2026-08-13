-- +goose Up
CREATE TABLE workspace_test_cases (
 id BIGINT PRIMARY KEY CHECK(id>0), tenant_id BIGINT NOT NULL CHECK(tenant_id>0), entity_id BIGINT NOT NULL CHECK(entity_id>0), owner_id BIGINT NOT NULL CHECK(owner_id>0),
 requirement_id BIGINT NULL CHECK(requirement_id IS NULL OR requirement_id>0), title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 300), description TEXT NOT NULL DEFAULT '', preconditions TEXT NOT NULL DEFAULT '',
 priority TEXT NOT NULL CHECK(priority IN ('highest','high','medium','low','lowest')), status TEXT NOT NULL CHECK(status IN ('draft','active','retired')), assignee_id BIGINT NULL CHECK(assignee_id IS NULL OR assignee_id>0),
 created_at BIGINT NOT NULL CHECK(created_at>0), created_by BIGINT NOT NULL CHECK(created_by>0), updated_at BIGINT NOT NULL CHECK(updated_at>0), updated_by BIGINT NOT NULL CHECK(updated_by>0),
 deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at>=0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by>=0), version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
 CHECK((deleted_at=0 AND deleted_by=0) OR (deleted_at>0 AND deleted_by>0)), FOREIGN KEY(requirement_id) REFERENCES workspace_requirements(id) ON DELETE RESTRICT
);
CREATE INDEX idx_workspace_test_cases_scope ON workspace_test_cases(tenant_id,entity_id,deleted_at,status,updated_at);
CREATE INDEX idx_workspace_test_cases_requirement ON workspace_test_cases(tenant_id,entity_id,requirement_id,deleted_at);
CREATE TABLE workspace_test_steps (
 id BIGINT PRIMARY KEY CHECK(id>0), tenant_id BIGINT NOT NULL CHECK(tenant_id>0), entity_id BIGINT NOT NULL CHECK(entity_id>0), test_case_id BIGINT NOT NULL CHECK(test_case_id>0), position INTEGER NOT NULL CHECK(position BETWEEN 1 AND 100),
 action TEXT NOT NULL CHECK(length(action)>0), expected_result TEXT NOT NULL CHECK(length(expected_result)>0), created_at BIGINT NOT NULL CHECK(created_at>0), created_by BIGINT NOT NULL CHECK(created_by>0), updated_at BIGINT NOT NULL CHECK(updated_at>0), updated_by BIGINT NOT NULL CHECK(updated_by>0),
 deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at>=0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by>=0), version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
 CHECK((deleted_at=0 AND deleted_by=0) OR (deleted_at>0 AND deleted_by>0)), FOREIGN KEY(test_case_id) REFERENCES workspace_test_cases(id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX uq_workspace_test_steps_position ON workspace_test_steps(tenant_id,entity_id,test_case_id,position) WHERE deleted_at=0;
CREATE INDEX idx_workspace_test_steps_case ON workspace_test_steps(tenant_id,entity_id,test_case_id,deleted_at,position);
-- +goose Down
DROP TABLE IF EXISTS workspace_test_steps;
DROP TABLE IF EXISTS workspace_test_cases;
