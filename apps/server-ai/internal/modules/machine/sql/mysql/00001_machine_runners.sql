-- +goose Up
CREATE TABLE machine_runners (
    id BIGINT NOT NULL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    address VARCHAR(512) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'confirmed',
    last_heartbeat_at BIGINT NOT NULL DEFAULT 0,
    token_hash CHAR(64) NOT NULL DEFAULT '',
    os VARCHAR(128) NOT NULL DEFAULT '',
    entity_id BIGINT NOT NULL,
    owner_id BIGINT NOT NULL,
    registered_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    created_by BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    updated_by BIGINT NOT NULL,
    deleted_at BIGINT NOT NULL DEFAULT 0,
    deleted_by BIGINT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT chk_machine_status CHECK (status IN ('confirmed', 'offline')),
    CONSTRAINT chk_machine_times CHECK (last_heartbeat_at >= 0 AND registered_at >= 0 AND created_at > 0 AND updated_at > 0 AND deleted_at >= 0),
    CONSTRAINT chk_machine_version CHECK (version >= 1),
    CONSTRAINT chk_machine_scope CHECK (tenant_id > 0 AND entity_id > 0 AND owner_id > 0 AND created_by > 0 AND updated_by > 0),
    CONSTRAINT chk_machine_deleted CHECK ((deleted_at = 0 AND deleted_by = 0) OR (deleted_at > 0 AND deleted_by > 0)),
    KEY idx_machine_runners_scope (tenant_id, entity_id, deleted_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS machine_runners;
