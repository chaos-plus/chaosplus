-- +goose Up
SET @workspace_attachment_resource_check = (
 SELECT tc.CONSTRAINT_NAME FROM information_schema.TABLE_CONSTRAINTS tc
 JOIN information_schema.CHECK_CONSTRAINTS cc ON cc.CONSTRAINT_SCHEMA = tc.CONSTRAINT_SCHEMA AND cc.CONSTRAINT_NAME = tc.CONSTRAINT_NAME
 WHERE tc.CONSTRAINT_SCHEMA = DATABASE() AND tc.TABLE_NAME = 'workspace_attachments' AND tc.CONSTRAINT_TYPE = 'CHECK' AND cc.CHECK_CLAUSE LIKE '%resource_type%'
 LIMIT 1
);
SET @workspace_attachment_drop_check = CONCAT('ALTER TABLE workspace_attachments DROP CHECK `', @workspace_attachment_resource_check, '`');
PREPARE workspace_attachment_statement FROM @workspace_attachment_drop_check;
EXECUTE workspace_attachment_statement;
DEALLOCATE PREPARE workspace_attachment_statement;
ALTER TABLE workspace_attachments ADD CONSTRAINT workspace_attachments_resource_type_check CHECK(resource_type IN ('requirement','task','objective','testcase','defect','conversation'));

-- +goose Down
ALTER TABLE workspace_attachments DROP CHECK workspace_attachments_resource_type_check;
ALTER TABLE workspace_attachments ADD CONSTRAINT workspace_attachments_resource_type_check CHECK(resource_type IN ('requirement','task','objective','conversation'));
