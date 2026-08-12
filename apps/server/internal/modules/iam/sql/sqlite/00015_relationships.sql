-- +goose Up
CREATE TABLE iam_relationships (
    tenant_id BIGINT NOT NULL,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('principal', 'group', 'position', 'entity')),
    subject_id BIGINT NOT NULL,
    subject_relation TEXT NOT NULL DEFAULT '',
    relation TEXT NOT NULL CHECK (relation IN ('owner', 'editor', 'viewer')),
    resource_type TEXT NOT NULL,
    resource_id BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, subject_type, subject_id, subject_relation, relation, resource_type, resource_id),
    FOREIGN KEY (tenant_id, resource_id) REFERENCES iam_entities (tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_iam_relationships_subject ON iam_relationships (tenant_id, subject_type, subject_id, subject_relation);
CREATE INDEX idx_iam_relationships_resource ON iam_relationships (tenant_id, resource_type, resource_id, relation);

-- +goose Down
DROP TABLE iam_relationships;