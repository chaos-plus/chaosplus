-- +goose Up
CREATE TABLE skills (
 id BIGINT PRIMARY KEY CHECK(id > 0), tenant_id BIGINT NOT NULL CHECK(tenant_id > 0), entity_id BIGINT NOT NULL CHECK(entity_id > 0), owner_id BIGINT NOT NULL CHECK(owner_id > 0),
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200), frontmatter_json TEXT NOT NULL DEFAULT '{}' CHECK(length(frontmatter_json) <= 65535), body TEXT NOT NULL DEFAULT '' CHECK(length(body) <= 65535),
 source_run_id BIGINT NOT NULL DEFAULT 0 CHECK(source_run_id >= 0), approved_by BIGINT NOT NULL DEFAULT 0 CHECK(approved_by >= 0), version TEXT NOT NULL DEFAULT '' CHECK(length(version) <= 64),
 created_at BIGINT NOT NULL CHECK(created_at > 0), created_by BIGINT NOT NULL CHECK(created_by > 0), updated_at BIGINT NOT NULL CHECK(updated_at > 0), updated_by BIGINT NOT NULL CHECK(updated_by > 0),
 deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at >= 0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by >= 0)
);
CREATE INDEX idx_skills_scope ON skills(tenant_id, entity_id, deleted_at, name);
-- +goose Down
DROP TABLE IF EXISTS skills;
