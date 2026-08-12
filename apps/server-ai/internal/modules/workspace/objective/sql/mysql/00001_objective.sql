-- +goose Up
CREATE TABLE workspace_objectives (
 id BIGINT PRIMARY KEY CHECK(id > 0), tenant_id BIGINT NOT NULL CHECK(tenant_id > 0), entity_id BIGINT NOT NULL CHECK(entity_id > 0), owner_id BIGINT NOT NULL CHECK(owner_id > 0),
 title VARCHAR(300) NOT NULL CHECK(char_length(title) BETWEEN 1 AND 300), description TEXT NOT NULL, period_start BIGINT NOT NULL CHECK(period_start > 0), period_end BIGINT NOT NULL CHECK(period_end >= period_start), status VARCHAR(16) NOT NULL CHECK(status IN ('draft','active','completed','cancelled')),
 created_at BIGINT NOT NULL CHECK(created_at > 0), created_by BIGINT NOT NULL CHECK(created_by > 0), updated_at BIGINT NOT NULL CHECK(updated_at > 0), updated_by BIGINT NOT NULL CHECK(updated_by > 0), deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at >= 0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by >= 0), version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1),
 CHECK((deleted_at = 0 AND deleted_by = 0) OR (deleted_at > 0 AND deleted_by > 0)), INDEX idx_workspace_objectives_scope(tenant_id, entity_id, deleted_at, status, period_start)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE workspace_key_results (
 id BIGINT PRIMARY KEY CHECK(id > 0), tenant_id BIGINT NOT NULL CHECK(tenant_id > 0), entity_id BIGINT NOT NULL CHECK(entity_id > 0), owner_id BIGINT NOT NULL CHECK(owner_id > 0), objective_id BIGINT NOT NULL CHECK(objective_id > 0),
 title VARCHAR(300) NOT NULL CHECK(char_length(title) BETWEEN 1 AND 300), target_value DECIMAL(20,6) NOT NULL CHECK(target_value >= 0), current_value DECIMAL(20,6) NOT NULL CHECK(current_value >= 0), unit VARCHAR(32) NOT NULL,
 created_at BIGINT NOT NULL CHECK(created_at > 0), created_by BIGINT NOT NULL CHECK(created_by > 0), updated_at BIGINT NOT NULL CHECK(updated_at > 0), updated_by BIGINT NOT NULL CHECK(updated_by > 0), deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at >= 0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by >= 0), version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1),
 CHECK((deleted_at = 0 AND deleted_by = 0) OR (deleted_at > 0 AND deleted_by > 0)), CONSTRAINT fk_workspace_key_result_objective FOREIGN KEY(objective_id) REFERENCES workspace_objectives(id) ON DELETE RESTRICT, INDEX idx_workspace_key_results_objective(tenant_id, entity_id, objective_id, deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE IF EXISTS workspace_key_results;
DROP TABLE IF EXISTS workspace_objectives;
