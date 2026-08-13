-- +goose Up
CREATE TABLE workspace_test_cases (
 id BIGINT PRIMARY KEY CHECK(id>0), tenant_id BIGINT NOT NULL CHECK(tenant_id>0), entity_id BIGINT NOT NULL CHECK(entity_id>0), owner_id BIGINT NOT NULL CHECK(owner_id>0),
 requirement_id BIGINT NULL CHECK(requirement_id IS NULL OR requirement_id>0), title VARCHAR(300) NOT NULL CHECK(char_length(title) BETWEEN 1 AND 300), description TEXT NOT NULL, preconditions TEXT NOT NULL,
 priority VARCHAR(16) NOT NULL CHECK(priority IN ('highest','high','medium','low','lowest')), status VARCHAR(16) NOT NULL CHECK(status IN ('draft','active','retired')), assignee_id BIGINT NULL CHECK(assignee_id IS NULL OR assignee_id>0),
 created_at BIGINT NOT NULL CHECK(created_at>0), created_by BIGINT NOT NULL CHECK(created_by>0), updated_at BIGINT NOT NULL CHECK(updated_at>0), updated_by BIGINT NOT NULL CHECK(updated_by>0),
 deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at>=0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by>=0), version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
 CHECK((deleted_at=0 AND deleted_by=0) OR (deleted_at>0 AND deleted_by>0)), CONSTRAINT fk_workspace_test_case_requirement FOREIGN KEY(requirement_id) REFERENCES workspace_requirements(id) ON DELETE RESTRICT,
 INDEX idx_workspace_test_cases_scope(tenant_id,entity_id,deleted_at,status,updated_at), INDEX idx_workspace_test_cases_requirement(tenant_id,entity_id,requirement_id,deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE workspace_test_steps (
 id BIGINT PRIMARY KEY CHECK(id>0), tenant_id BIGINT NOT NULL CHECK(tenant_id>0), entity_id BIGINT NOT NULL CHECK(entity_id>0), test_case_id BIGINT NOT NULL CHECK(test_case_id>0), position INTEGER NOT NULL CHECK(position BETWEEN 1 AND 100),
 action TEXT NOT NULL, expected_result TEXT NOT NULL, created_at BIGINT NOT NULL CHECK(created_at>0), created_by BIGINT NOT NULL CHECK(created_by>0), updated_at BIGINT NOT NULL CHECK(updated_at>0), updated_by BIGINT NOT NULL CHECK(updated_by>0),
 deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at>=0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by>=0), version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1), active_position INTEGER GENERATED ALWAYS AS (CASE WHEN deleted_at=0 THEN position ELSE NULL END) STORED,
 CHECK(char_length(action)>0), CHECK(char_length(expected_result)>0), CHECK((deleted_at=0 AND deleted_by=0) OR (deleted_at>0 AND deleted_by>0)), CONSTRAINT fk_workspace_test_step_case FOREIGN KEY(test_case_id) REFERENCES workspace_test_cases(id) ON DELETE RESTRICT,
 UNIQUE KEY uq_workspace_test_steps_position(tenant_id,entity_id,test_case_id,active_position), INDEX idx_workspace_test_steps_case(tenant_id,entity_id,test_case_id,deleted_at,position)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE IF EXISTS workspace_test_steps;
DROP TABLE IF EXISTS workspace_test_cases;
