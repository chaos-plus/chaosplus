-- +goose Up
CREATE TABLE machine_onboarding_tokens (
    machine_id BIGINT PRIMARY KEY CHECK (machine_id > 0),
    tenant_id BIGINT NOT NULL CHECK (tenant_id > 0),
    entity_id BIGINT NOT NULL CHECK (entity_id > 0),
    owner_id BIGINT NOT NULL CHECK (owner_id > 0),
    token_hash CHAR(64) NOT NULL UNIQUE,
    expires_at BIGINT NOT NULL CHECK (expires_at > 0),
    created_at BIGINT NOT NULL CHECK (created_at > 0)
);
CREATE INDEX idx_machine_onboarding_scope_expiry ON machine_onboarding_tokens (tenant_id, entity_id, owner_id, expires_at);
CREATE TABLE machine_connection_leases (
    machine_id BIGINT PRIMARY KEY CHECK (machine_id > 0),
    tenant_id BIGINT NOT NULL CHECK (tenant_id > 0),
    entity_id BIGINT NOT NULL CHECK (entity_id > 0),
    holder_id BIGINT NOT NULL CHECK (holder_id > 0),
    fencing_token BIGINT NOT NULL CHECK (fencing_token >= 1),
    expires_at BIGINT NOT NULL CHECK (expires_at > 0),
    name VARCHAR(200) NOT NULL DEFAULT '',
    runtimes_json JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(runtimes_json) = 'array'),
    os VARCHAR(200) NOT NULL DEFAULT '',
    address VARCHAR(1000) NOT NULL DEFAULT '',
    updated_at BIGINT NOT NULL CHECK (updated_at > 0)
);
CREATE INDEX idx_machine_connection_scope_expiry ON machine_connection_leases (tenant_id, entity_id, expires_at, machine_id);

-- +goose Down
DROP TABLE IF EXISTS machine_connection_leases;
DROP TABLE IF EXISTS machine_onboarding_tokens;
