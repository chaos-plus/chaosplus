-- +goose Up
CREATE TABLE executors (
 id BIGINT PRIMARY KEY CHECK(id > 0), tenant_id BIGINT NOT NULL CHECK(tenant_id > 0), entity_id BIGINT NOT NULL CHECK(entity_id > 0), owner_id BIGINT NOT NULL CHECK(owner_id > 0),
 runtime TEXT NOT NULL CHECK(length(runtime) BETWEEN 1 AND 64), model_config_json TEXT NOT NULL DEFAULT '{}' CHECK(length(model_config_json) <= 65535), name TEXT NOT NULL DEFAULT '' CHECK(length(name) <= 200),
 created_at BIGINT NOT NULL CHECK(created_at > 0), created_by BIGINT NOT NULL CHECK(created_by > 0), updated_at BIGINT NOT NULL CHECK(updated_at > 0), updated_by BIGINT NOT NULL CHECK(updated_by > 0),
 deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at >= 0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by >= 0)
);
CREATE TABLE meta (
 runner_id BIGINT NOT NULL CHECK(runner_id > 0), key TEXT NOT NULL CHECK(length(key) BETWEEN 1 AND 128), value_json TEXT NOT NULL DEFAULT '{}' CHECK(length(value_json) <= 65535),
 PRIMARY KEY (runner_id, key)
);
-- +goose Down
DROP TABLE IF EXISTS meta;
DROP TABLE IF EXISTS executors;
