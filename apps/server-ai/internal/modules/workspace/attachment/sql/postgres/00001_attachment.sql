-- +goose Up
CREATE TABLE workspace_attachments (
 id BIGINT PRIMARY KEY CHECK(id > 0), tenant_id BIGINT NOT NULL CHECK(tenant_id > 0), entity_id BIGINT NOT NULL CHECK(entity_id > 0), owner_id BIGINT NOT NULL CHECK(owner_id > 0),
 resource_type VARCHAR(16) NOT NULL CHECK(resource_type IN ('requirement','task','objective','testcase','defect','conversation')), resource_id BIGINT NOT NULL CHECK(resource_id > 0), filename VARCHAR(255) NOT NULL CHECK(char_length(filename) BETWEEN 1 AND 255), content_type VARCHAR(127) NOT NULL,
 size_bytes BIGINT NOT NULL CHECK(size_bytes BETWEEN 1 AND 104857600), checksum CHAR(64) NOT NULL CHECK(checksum ~ '^[0-9a-f]{64}$'), object_key VARCHAR(512) NOT NULL UNIQUE,
 status VARCHAR(16) NOT NULL CHECK(status IN ('available','deleting','delete_failed')), created_at BIGINT NOT NULL CHECK(created_at > 0), created_by BIGINT NOT NULL CHECK(created_by > 0), updated_at BIGINT NOT NULL CHECK(updated_at > 0), updated_by BIGINT NOT NULL CHECK(updated_by > 0),
 deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at >= 0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by >= 0), version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1), CHECK((deleted_at = 0 AND deleted_by = 0) OR (deleted_at > 0 AND deleted_by > 0))
);
CREATE INDEX idx_workspace_attachments_resource ON workspace_attachments(tenant_id, entity_id, resource_type, resource_id, deleted_at, created_at);
CREATE INDEX idx_workspace_attachments_deletion ON workspace_attachments(status, updated_at) WHERE deleted_at = 0;
-- +goose Down
DROP TABLE IF EXISTS workspace_attachments;
