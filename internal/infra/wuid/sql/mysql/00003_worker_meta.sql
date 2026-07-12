-- +goose Up
ALTER TABLE worker_ids ADD COLUMN meta TEXT;

-- +goose Down
ALTER TABLE worker_ids DROP COLUMN meta;
