-- +goose Up
ALTER TABLE iam_relationships
    ADD COLUMN starts_at BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN ends_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE iam_resource_relationships
    ADD COLUMN starts_at BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN ends_at BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE iam_resource_relationships
    DROP COLUMN ends_at,
    DROP COLUMN starts_at;
ALTER TABLE iam_relationships
    DROP COLUMN ends_at,
    DROP COLUMN starts_at;