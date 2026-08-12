-- +goose Up
CREATE TABLE skills (
 id BIGINT PRIMARY KEY CHECK(id > 0), tenant_id BIGINT NOT NULL CHECK(tenant_id > 0), entity_id BIGINT NOT NULL CHECK(entity_id > 0), owner_id BIGINT NOT NULL CHECK(owner_id > 0),
 name VARCHAR(200) NOT NULL CHECK(CHAR_LENGTH(name) BETWEEN 1 AND 200), frontmatter_json TEXT NOT NULL CHECK(CHAR_LENGTH(frontmatter_json) <= 65535), body TEXT NOT NULL CHECK(CHAR_LENGTH(body) <= 65535),
 source_run_id BIGINT NOT NULL DEFAULT 0, approved_by BIGINT NOT NULL DEFAULT 0, version VARCHAR(64) NOT NULL DEFAULT '' CHECK(CHAR_LENGTH(version) <= 64),
 created_at BIGINT NOT NULL, created_by BIGINT NOT NULL, updated_at BIGINT NOT NULL, updated_by BIGINT NOT NULL,
 deleted_at BIGINT NOT NULL DEFAULT 0, deleted_by BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_skills_scope ON skills(tenant_id, entity_id, deleted_at, name);
-- +goose Down
DROP TABLE IF EXISTS skills;
