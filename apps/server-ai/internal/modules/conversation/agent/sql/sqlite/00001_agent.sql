-- +goose Up
CREATE TABLE conversation_agents (
 id BIGINT PRIMARY KEY CHECK(id > 0), tenant_id BIGINT NOT NULL CHECK(tenant_id > 0), entity_id BIGINT NOT NULL CHECK(entity_id > 0), owner_id BIGINT NOT NULL CHECK(owner_id > 0), machine_id BIGINT NOT NULL CHECK(machine_id > 0),
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200), kind TEXT NOT NULL CHECK(kind IN ('digital_human','executor')), runtime TEXT NOT NULL CHECK(length(runtime) BETWEEN 1 AND 64), model TEXT NOT NULL DEFAULT '' CHECK(length(model) <= 128), provider TEXT NOT NULL DEFAULT '' CHECK(length(provider) <= 128), system_prompt TEXT NOT NULL DEFAULT '' CHECK(length(system_prompt) <= 65535), description TEXT NOT NULL DEFAULT '' CHECK(length(description) <= 4096),
 status TEXT NOT NULL CHECK(status IN ('running','stopped','retired')), handover_doc TEXT NOT NULL DEFAULT '' CHECK(length(handover_doc) <= 65535), retired_at BIGINT NOT NULL DEFAULT 0 CHECK(retired_at >= 0),
 created_at BIGINT NOT NULL CHECK(created_at > 0), created_by BIGINT NOT NULL CHECK(created_by > 0), updated_at BIGINT NOT NULL CHECK(updated_at > 0), updated_by BIGINT NOT NULL CHECK(updated_by > 0), deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at >= 0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by >= 0), version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1),
 CHECK((status = 'retired' AND retired_at > 0 AND length(handover_doc) > 0) OR (status <> 'retired' AND retired_at = 0)), CHECK((deleted_at = 0 AND deleted_by = 0) OR (deleted_at > 0 AND deleted_by > 0)),
 FOREIGN KEY(machine_id) REFERENCES machine_runners(id) ON DELETE RESTRICT
);
CREATE INDEX idx_conversation_agents_scope ON conversation_agents(tenant_id, entity_id, deleted_at, status, updated_at);
CREATE INDEX idx_conversation_agents_machine ON conversation_agents(tenant_id, entity_id, machine_id, deleted_at, status);
-- +goose Down
DROP TABLE IF EXISTS conversation_agents;
