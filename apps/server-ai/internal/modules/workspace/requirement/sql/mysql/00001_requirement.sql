-- +goose Up
CREATE TABLE workspace_requirements (
 id BIGINT PRIMARY KEY CHECK(id>0), tenant_id BIGINT NOT NULL CHECK(tenant_id>0), entity_id BIGINT NOT NULL CHECK(entity_id>0), owner_id BIGINT NOT NULL CHECK(owner_id>0),
 parent_id BIGINT NULL CHECK(parent_id IS NULL OR parent_id>0), title VARCHAR(300) NOT NULL CHECK(char_length(title) BETWEEN 1 AND 300), description TEXT NOT NULL, acceptance_criteria TEXT NOT NULL,
 status VARCHAR(16) NOT NULL CHECK(status IN ('draft','approved','rejected','closed')),
 created_at BIGINT NOT NULL CHECK(created_at>0), created_by BIGINT NOT NULL CHECK(created_by>0), updated_at BIGINT NOT NULL CHECK(updated_at>0), updated_by BIGINT NOT NULL CHECK(updated_by>0),
 deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at>=0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by>=0), version BIGINT NOT NULL DEFAULT 1 CHECK(version>=1),
 CHECK(parent_id IS NULL OR parent_id<>id), CHECK((deleted_at=0 AND deleted_by=0) OR (deleted_at>0 AND deleted_by>0)),
 CONSTRAINT fk_workspace_requirement_parent FOREIGN KEY(parent_id) REFERENCES workspace_requirements(id) ON DELETE RESTRICT,
 INDEX idx_workspace_requirements_scope(tenant_id,entity_id,deleted_at,status,updated_at), INDEX idx_workspace_requirements_parent(tenant_id,entity_id,parent_id,deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE IF EXISTS workspace_requirements;
