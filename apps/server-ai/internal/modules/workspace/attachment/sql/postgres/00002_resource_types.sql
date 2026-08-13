-- +goose Up
ALTER TABLE workspace_attachments DROP CONSTRAINT workspace_attachments_resource_type_check;
ALTER TABLE workspace_attachments ADD CONSTRAINT workspace_attachments_resource_type_check CHECK(resource_type IN ('requirement','task','objective','testcase','defect','conversation'));

-- +goose Down
ALTER TABLE workspace_attachments DROP CONSTRAINT workspace_attachments_resource_type_check;
ALTER TABLE workspace_attachments ADD CONSTRAINT workspace_attachments_resource_type_check CHECK(resource_type IN ('requirement','task','objective','conversation'));
