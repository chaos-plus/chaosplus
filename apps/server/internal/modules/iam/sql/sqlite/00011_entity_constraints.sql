-- +goose Up
CREATE UNIQUE INDEX uq_iam_entities_sibling_name
    ON iam_entities (tenant_id, COALESCE(parent_id, ''), type, name);

-- +goose Down
DROP INDEX uq_iam_entities_sibling_name;