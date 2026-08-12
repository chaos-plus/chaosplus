-- +goose Up
CREATE TABLE executors (
 id BIGINT PRIMARY KEY CHECK(id > 0), tenant_id BIGINT NOT NULL CHECK(tenant_id > 0), entity_id BIGINT NOT NULL CHECK(entity_id > 0), owner_id BIGINT NOT NULL CHECK(owner_id > 0),
 runtime VARCHAR(64) NOT NULL CHECK(CHAR_LENGTH(runtime) BETWEEN 1 AND 64), model_config_json TEXT NOT NULL CHECK(CHAR_LENGTH(model_config_json) <= 65535), name VARCHAR(200) NOT NULL DEFAULT '' CHECK(CHAR_LENGTH(name) <= 200),
 created_at BIGINT NOT NULL, created_by BIGINT NOT NULL, updated_at BIGINT NOT NULL, updated_by BIGINT NOT NULL,
 deleted_at BIGINT NOT NULL DEFAULT 0, deleted_by BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE meta (
 runner_id BIGINT NOT NULL CHECK(runner_id > 0), `key` VARCHAR(128) NOT NULL CHECK(CHAR_LENGTH(`key`) BETWEEN 1 AND 128), value_json TEXT NOT NULL CHECK(CHAR_LENGTH(value_json) <= 65535),
 PRIMARY KEY (runner_id, `key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- +goose Down
DROP TABLE IF EXISTS meta;
DROP TABLE IF EXISTS executors;
