-- +goose Up
CREATE TABLE workspace_requirement_key_results (
 tenant_id BIGINT NOT NULL CHECK(tenant_id>0), entity_id BIGINT NOT NULL CHECK(entity_id>0),
 requirement_id BIGINT NOT NULL CHECK(requirement_id>0), key_result_id BIGINT NOT NULL CHECK(key_result_id>0),
 created_at BIGINT NOT NULL CHECK(created_at>0), created_by BIGINT NOT NULL CHECK(created_by>0),
 PRIMARY KEY(tenant_id,entity_id,requirement_id,key_result_id),
 CONSTRAINT fk_workspace_requirement_kr_requirement FOREIGN KEY(requirement_id) REFERENCES workspace_requirements(id) ON DELETE CASCADE,
 CONSTRAINT fk_workspace_requirement_kr_key_result FOREIGN KEY(key_result_id) REFERENCES workspace_key_results(id) ON DELETE RESTRICT
);
CREATE INDEX idx_workspace_requirement_key_results_kr ON workspace_requirement_key_results(tenant_id,entity_id,key_result_id,requirement_id);
-- +goose Down
DROP TABLE IF EXISTS workspace_requirement_key_results;
