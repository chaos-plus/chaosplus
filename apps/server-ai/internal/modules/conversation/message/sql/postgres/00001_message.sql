-- +goose Up
CREATE TABLE conversation_message_events (
 seq BIGSERIAL PRIMARY KEY, id BIGINT NOT NULL UNIQUE CHECK(id > 0), tenant_id BIGINT NOT NULL CHECK(tenant_id > 0), entity_id BIGINT NOT NULL CHECK(entity_id > 0), channel_id BIGINT NOT NULL CHECK(channel_id > 0) REFERENCES conversation_channels(id) ON DELETE RESTRICT, actor_id BIGINT NOT NULL CHECK(actor_id > 0),
 type VARCHAR(32) NOT NULL CHECK(type = 'CHANNEL_MESSAGE_POSTED'), idempotency_key VARCHAR(128) NOT NULL, payload_json JSONB NOT NULL, created_at BIGINT NOT NULL CHECK(created_at > 0), UNIQUE(tenant_id, entity_id, channel_id, idempotency_key)
);
CREATE INDEX idx_conversation_message_events_channel ON conversation_message_events(tenant_id, entity_id, channel_id, seq);
CREATE TABLE conversation_messages (
 id BIGINT PRIMARY KEY CHECK(id > 0), seq BIGINT NOT NULL UNIQUE CHECK(seq > 0), event_id BIGINT NOT NULL UNIQUE CHECK(event_id > 0) REFERENCES conversation_message_events(id) ON DELETE RESTRICT, tenant_id BIGINT NOT NULL CHECK(tenant_id > 0), entity_id BIGINT NOT NULL CHECK(entity_id > 0), channel_id BIGINT NOT NULL CHECK(channel_id > 0) REFERENCES conversation_channels(id) ON DELETE RESTRICT, author_id BIGINT NOT NULL CHECK(author_id > 0), author_kind VARCHAR(16) NOT NULL CHECK(author_kind IN ('human','agent','workflow')),
 idempotency_key VARCHAR(128) NOT NULL, payload_json JSONB NOT NULL, created_at BIGINT NOT NULL CHECK(created_at > 0), UNIQUE(tenant_id, entity_id, channel_id, idempotency_key)
);
CREATE INDEX idx_conversation_messages_channel ON conversation_messages(tenant_id, entity_id, channel_id, seq);
-- +goose Down
DROP TABLE IF EXISTS conversation_messages;
DROP TABLE IF EXISTS conversation_message_events;
