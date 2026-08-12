-- +goose Up
CREATE TABLE conversation_channels (
 id BIGINT PRIMARY KEY CHECK(id > 0), tenant_id BIGINT NOT NULL CHECK(tenant_id > 0), entity_id BIGINT NOT NULL CHECK(entity_id > 0), project_id BIGINT NOT NULL CHECK(project_id > 0), owner_id BIGINT NOT NULL CHECK(owner_id > 0),
 name VARCHAR(120) NOT NULL CHECK(char_length(name) BETWEEN 1 AND 120), topic VARCHAR(1000) NOT NULL DEFAULT '', status VARCHAR(16) NOT NULL CHECK(status IN ('active','archived')),
 created_at BIGINT NOT NULL CHECK(created_at > 0), created_by BIGINT NOT NULL CHECK(created_by > 0), updated_at BIGINT NOT NULL CHECK(updated_at > 0), updated_by BIGINT NOT NULL CHECK(updated_by > 0), deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at >= 0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by >= 0), version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1),
 CHECK((deleted_at = 0 AND deleted_by = 0) OR (deleted_at > 0 AND deleted_by > 0))
);
CREATE INDEX idx_conversation_channels_scope ON conversation_channels(tenant_id, entity_id, project_id, deleted_at, status, updated_at, id);
CREATE TABLE conversation_channel_members (
 id BIGINT PRIMARY KEY CHECK(id > 0), tenant_id BIGINT NOT NULL CHECK(tenant_id > 0), entity_id BIGINT NOT NULL CHECK(entity_id > 0), channel_id BIGINT NOT NULL CHECK(channel_id > 0) REFERENCES conversation_channels(id) ON DELETE RESTRICT, member_id BIGINT NOT NULL CHECK(member_id > 0), kind VARCHAR(16) NOT NULL CHECK(kind IN ('human','agent')), role VARCHAR(16) NOT NULL CHECK(role IN ('owner','member')),
 joined_at BIGINT NOT NULL CHECK(joined_at > 0), joined_by BIGINT NOT NULL CHECK(joined_by > 0), removed_at BIGINT NOT NULL DEFAULT 0 CHECK(removed_at >= 0), removed_by BIGINT NOT NULL DEFAULT 0 CHECK(removed_by >= 0), version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1),
 UNIQUE(tenant_id, entity_id, channel_id, member_id, kind), CHECK((removed_at = 0 AND removed_by = 0) OR (removed_at > 0 AND removed_by > 0))
);
CREATE INDEX idx_conversation_channel_members_active ON conversation_channel_members(tenant_id, entity_id, channel_id, removed_at, kind, member_id);
-- +goose Down
DROP TABLE IF EXISTS conversation_channel_members;
DROP TABLE IF EXISTS conversation_channels;
