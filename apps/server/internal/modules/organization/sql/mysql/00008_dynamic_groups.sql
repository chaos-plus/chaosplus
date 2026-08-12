-- +goose Up
CREATE INDEX idx_iam_groups_dynamic ON iam_groups (tenant_id, group_type, status);

-- +goose Down
DROP INDEX idx_iam_groups_dynamic;