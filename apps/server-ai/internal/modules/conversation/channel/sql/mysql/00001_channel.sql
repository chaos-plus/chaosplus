-- +goose Up
CREATE TABLE conversation_channels (
 id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL, entity_id BIGINT NOT NULL, project_id BIGINT NOT NULL, owner_id BIGINT NOT NULL,
 name VARCHAR(120) NOT NULL, topic VARCHAR(1000) NOT NULL DEFAULT '', status VARCHAR(16) NOT NULL,
 created_at BIGINT NOT NULL, created_by BIGINT NOT NULL, updated_at BIGINT NOT NULL, updated_by BIGINT NOT NULL, deleted_at BIGINT NOT NULL DEFAULT 0, deleted_by BIGINT NOT NULL DEFAULT 0, version BIGINT NOT NULL DEFAULT 1,
 CHECK(id > 0 AND tenant_id > 0 AND entity_id > 0 AND project_id > 0 AND owner_id > 0 AND char_length(name) BETWEEN 1 AND 120 AND status IN ('active','archived') AND created_at > 0 AND created_by > 0 AND updated_at > 0 AND updated_by > 0 AND deleted_at >= 0 AND deleted_by >= 0 AND version >= 1),
 CHECK((deleted_at = 0 AND deleted_by = 0) OR (deleted_at > 0 AND deleted_by > 0)), INDEX idx_conversation_channels_scope(tenant_id, entity_id, project_id, deleted_at, status, updated_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE conversation_channel_members (
 id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL, entity_id BIGINT NOT NULL, channel_id BIGINT NOT NULL, member_id BIGINT NOT NULL, kind VARCHAR(16) NOT NULL, role VARCHAR(16) NOT NULL,
 joined_at BIGINT NOT NULL, joined_by BIGINT NOT NULL, removed_at BIGINT NOT NULL DEFAULT 0, removed_by BIGINT NOT NULL DEFAULT 0, version BIGINT NOT NULL DEFAULT 1,
 UNIQUE KEY uq_conversation_channel_member(tenant_id, entity_id, channel_id, member_id, kind), CONSTRAINT fk_conversation_channel_member_channel FOREIGN KEY(channel_id) REFERENCES conversation_channels(id) ON DELETE RESTRICT,
 CHECK(id > 0 AND tenant_id > 0 AND entity_id > 0 AND channel_id > 0 AND member_id > 0 AND kind IN ('human','agent') AND role IN ('owner','member') AND joined_at > 0 AND joined_by > 0 AND removed_at >= 0 AND removed_by >= 0 AND version >= 1),
 CHECK((removed_at = 0 AND removed_by = 0) OR (removed_at > 0 AND removed_by > 0)), INDEX idx_conversation_channel_members_active(tenant_id, entity_id, channel_id, removed_at, kind, member_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE IF EXISTS conversation_channel_members;
DROP TABLE IF EXISTS conversation_channels;
