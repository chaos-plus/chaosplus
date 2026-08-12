-- +goose Up
CREATE TABLE machine_runners (
    id BIGINT PRIMARY KEY,
    tenant_id BIGINT NOT NULL CHECK (tenant_id > 0),
    address VARCHAR(512) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'confirmed' CHECK (status IN ('confirmed', 'offline')),
    last_heartbeat_at BIGINT NOT NULL DEFAULT 0 CHECK (last_heartbeat_at >= 0),
    token_hash CHAR(64) NOT NULL DEFAULT '' CHECK (length(token_hash) IN (0, 64)),
    os VARCHAR(128) NOT NULL DEFAULT '',
    entity_id BIGINT NOT NULL CHECK (entity_id > 0),
    owner_id BIGINT NOT NULL CHECK (owner_id > 0),
    registered_at BIGINT NOT NULL DEFAULT 0 CHECK (registered_at >= 0),
    created_at BIGINT NOT NULL CHECK (created_at > 0),
    created_by BIGINT NOT NULL CHECK (created_by > 0),
    updated_at BIGINT NOT NULL CHECK (updated_at > 0),
    updated_by BIGINT NOT NULL CHECK (updated_by > 0),
    deleted_at BIGINT NOT NULL DEFAULT 0 CHECK (deleted_at >= 0),
    deleted_by BIGINT NOT NULL DEFAULT 0 CHECK (deleted_by >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    CHECK ((deleted_at = 0 AND deleted_by = 0) OR (deleted_at > 0 AND deleted_by > 0))
);
CREATE INDEX idx_machine_runners_scope ON machine_runners(tenant_id, entity_id, deleted_at, id);

-- +goose Down
DROP TABLE IF EXISTS machine_runners;
