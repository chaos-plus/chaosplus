-- +goose Up
CREATE TABLE machine_onboarding_tokens (
    machine_id BIGINT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    entity_id BIGINT NOT NULL,
    owner_id BIGINT NOT NULL,
    token_hash CHAR(64) NOT NULL UNIQUE,
    expires_at BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    CHECK (machine_id > 0 AND tenant_id > 0 AND entity_id > 0 AND owner_id > 0 AND expires_at > 0 AND created_at > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE INDEX idx_machine_onboarding_scope_expiry ON machine_onboarding_tokens (tenant_id, entity_id, owner_id, expires_at);
CREATE TABLE machine_connection_leases (
    machine_id BIGINT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    entity_id BIGINT NOT NULL,
    holder_id BIGINT NOT NULL,
    fencing_token BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    name VARCHAR(200) NOT NULL DEFAULT '',
    runtimes_json JSON NOT NULL,
    os VARCHAR(200) NOT NULL DEFAULT '',
    address VARCHAR(1000) NOT NULL DEFAULT '',
    updated_at BIGINT NOT NULL,
    CHECK (machine_id > 0 AND tenant_id > 0 AND entity_id > 0 AND holder_id > 0 AND fencing_token >= 1 AND expires_at > 0 AND updated_at > 0),
    CHECK (JSON_TYPE(runtimes_json) = 'ARRAY')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE INDEX idx_machine_connection_scope_expiry ON machine_connection_leases (tenant_id, entity_id, expires_at, machine_id);

-- +goose Down
DROP TABLE IF EXISTS machine_connection_leases;
DROP TABLE IF EXISTS machine_onboarding_tokens;
