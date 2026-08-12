-- +goose Up
CREATE TABLE conversation_message_events (
 seq BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, id BIGINT NOT NULL UNIQUE, tenant_id BIGINT NOT NULL, entity_id BIGINT NOT NULL, channel_id BIGINT NOT NULL, actor_id BIGINT NOT NULL,
 type VARCHAR(32) NOT NULL, idempotency_key VARCHAR(128) NOT NULL, payload_json JSON NOT NULL, created_at BIGINT NOT NULL,
 UNIQUE KEY uq_conversation_message_event_idempotency(tenant_id, entity_id, channel_id, idempotency_key), CONSTRAINT fk_conversation_message_event_channel FOREIGN KEY(channel_id) REFERENCES conversation_channels(id) ON DELETE RESTRICT,
 CHECK(id > 0 AND tenant_id > 0 AND entity_id > 0 AND channel_id > 0 AND actor_id > 0 AND type = 'CHANNEL_MESSAGE_POSTED' AND created_at > 0), INDEX idx_conversation_message_events_channel(tenant_id, entity_id, channel_id, seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE conversation_messages (
 id BIGINT PRIMARY KEY, seq BIGINT NOT NULL UNIQUE, event_id BIGINT NOT NULL UNIQUE, tenant_id BIGINT NOT NULL, entity_id BIGINT NOT NULL, channel_id BIGINT NOT NULL, author_id BIGINT NOT NULL, author_kind VARCHAR(16) NOT NULL,
 idempotency_key VARCHAR(128) NOT NULL, payload_json JSON NOT NULL, created_at BIGINT NOT NULL,
 UNIQUE KEY uq_conversation_message_idempotency(tenant_id, entity_id, channel_id, idempotency_key), CONSTRAINT fk_conversation_message_event FOREIGN KEY(event_id) REFERENCES conversation_message_events(id) ON DELETE RESTRICT, CONSTRAINT fk_conversation_message_channel FOREIGN KEY(channel_id) REFERENCES conversation_channels(id) ON DELETE RESTRICT,
 CHECK(id > 0 AND seq > 0 AND event_id > 0 AND tenant_id > 0 AND entity_id > 0 AND channel_id > 0 AND author_id > 0 AND author_kind IN ('human','agent','workflow') AND created_at > 0), INDEX idx_conversation_messages_channel(tenant_id, entity_id, channel_id, seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE IF EXISTS conversation_messages;
DROP TABLE IF EXISTS conversation_message_events;
