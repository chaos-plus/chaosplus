-- +goose Up
CREATE TABLE iam_relationships (
    tenant_id VARCHAR(128) NOT NULL,
    subject_type VARCHAR(16) NOT NULL,
    subject_id VARCHAR(255) NOT NULL,
    subject_relation VARCHAR(16) NOT NULL DEFAULT '',
    relation VARCHAR(16) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    resource_id VARCHAR(64) NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, subject_type, subject_id, subject_relation, relation, resource_type, resource_id),
    CONSTRAINT chk_iam_relationship_subject_type CHECK (subject_type IN ('principal', 'group', 'position', 'entity')),
    CONSTRAINT chk_iam_relationship_relation CHECK (relation IN ('owner', 'editor', 'viewer')),
    CONSTRAINT fk_iam_relationship_resource FOREIGN KEY (tenant_id, resource_id)
        REFERENCES iam_entities (tenant_id, id) ON DELETE RESTRICT,
    INDEX idx_iam_relationships_subject (tenant_id, subject_type, subject_id, subject_relation),
    INDEX idx_iam_relationships_resource (tenant_id, resource_type, resource_id, relation)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE iam_relationships;
