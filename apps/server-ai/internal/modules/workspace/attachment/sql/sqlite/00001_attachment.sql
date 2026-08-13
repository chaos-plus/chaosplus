-- +goose Up
CREATE TABLE workspace_attachments (
 id BIGINT PRIMARY KEY CHECK(id > 0), tenant_id BIGINT NOT NULL CHECK(tenant_id > 0), entity_id BIGINT NOT NULL CHECK(entity_id > 0), owner_id BIGINT NOT NULL CHECK(owner_id > 0),
 resource_type TEXT NOT NULL CHECK(resource_type IN ('requirement','task','objective','testcase','defect','conversation')), resource_id BIGINT NOT NULL CHECK(resource_id > 0), filename TEXT NOT NULL CHECK(length(filename) BETWEEN 1 AND 255), content_type TEXT NOT NULL CHECK(length(content_type) BETWEEN 1 AND 127),
 size_bytes BIGINT NOT NULL CHECK(size_bytes BETWEEN 1 AND 104857600), checksum TEXT NOT NULL CHECK(length(checksum) = 64 AND checksum NOT GLOB '*[^0-9a-f]*'), object_key TEXT NOT NULL UNIQUE CHECK(length(object_key) BETWEEN 1 AND 512),
 status TEXT NOT NULL CHECK(status IN ('available','deleting','delete_failed')), created_at BIGINT NOT NULL CHECK(created_at > 0), created_by BIGINT NOT NULL CHECK(created_by > 0), updated_at BIGINT NOT NULL CHECK(updated_at > 0), updated_by BIGINT NOT NULL CHECK(updated_by > 0),
 deleted_at BIGINT NOT NULL DEFAULT 0 CHECK(deleted_at >= 0), deleted_by BIGINT NOT NULL DEFAULT 0 CHECK(deleted_by >= 0), version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1), CHECK((deleted_at = 0 AND deleted_by = 0) OR (deleted_at > 0 AND deleted_by > 0))
);
CREATE INDEX idx_workspace_attachments_resource ON workspace_attachments(tenant_id, entity_id, resource_type, resource_id, deleted_at, created_at);
CREATE INDEX idx_workspace_attachments_deletion ON workspace_attachments(status, updated_at) WHERE deleted_at = 0;
-- +goose Down
DROP TABLE IF EXISTS workspace_attachments;
