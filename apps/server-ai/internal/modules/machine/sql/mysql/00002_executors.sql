-- +goose Up
CREATE TABLE executors (
 id BIGINT PRIMARY KEY CHECK(id > 0), tenant_id BIGINT NOT NULL CHECK(tenant_id > 0), entity_id BIGINT NOT NULL CHECK(entity_id > 0), owner_id BIGINT NOT NULL CHECK(owner_id > 0),
 runtime TEXT NOT NULL, model_config_json TEXT NOT NULL DEFAULT ('{}'), name TEXT NOT NULL DEFAULT (''),
 created_at BIGINT NOT NULL, created_by BIGINT NOT NULL, updated_at BIGINT NOT NULL, updated_by BIGINT NOT NULL,
 deleted_at BIGINT NOT NULL DEFAULT 0, deleted_by BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE meta (
 runner_id BIGINT NOT NULL, key TEXT NOT NULL, value_json TEXT NOT NULL DEFAULT ('{}'),
 PRIMARY KEY (runner_id, key)
);
-- +goose Down
DROP TABLE IF EXISTS meta;
DROP TABLE IF EXISTS executors;
