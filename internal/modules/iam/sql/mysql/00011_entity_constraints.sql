-- +goose Up
ALTER TABLE iam_entities
    ADD COLUMN parent_key VARCHAR(64) GENERATED ALWAYS AS (COALESCE(parent_id, '')) STORED,
    ADD UNIQUE KEY uq_iam_entities_sibling_name (tenant_id, parent_key, type, name);

-- +goose Down
ALTER TABLE iam_entities
    DROP INDEX uq_iam_entities_sibling_name,
    DROP COLUMN parent_key;
