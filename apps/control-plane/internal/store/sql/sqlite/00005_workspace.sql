-- +goose Up
ALTER TABLE work_items ADD COLUMN parent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE work_items ADD COLUMN estimate_hours REAL NOT NULL DEFAULT 0;
ALTER TABLE work_items ADD COLUMN spent_hours REAL NOT NULL DEFAULT 0;
ALTER TABLE work_items ADD COLUMN progress INTEGER NOT NULL DEFAULT 0;
ALTER TABLE work_items ADD COLUMN workflow_run_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_work_items_parent ON work_items(parent_id);

CREATE TABLE attachments (
    id TEXT PRIMARY KEY,
    owner_type TEXT NOT NULL,      -- work_item | message
    owner_id TEXT NOT NULL,
    filename TEXT NOT NULL,
    mime TEXT NOT NULL DEFAULT 'application/octet-stream',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    store_path TEXT NOT NULL,
    created_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_attachments_owner ON attachments(owner_type, owner_id);

CREATE TABLE okrs (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    objective TEXT NOT NULL DEFAULT '',
    period TEXT NOT NULL DEFAULT '',
    key_results TEXT NOT NULL DEFAULT '[]',   -- JSON [{title,target,progress,unit}]
    created_at BIGINT NOT NULL DEFAULT 0,
    updated_at BIGINT NOT NULL DEFAULT 0
);

-- +goose Down
DROP INDEX idx_work_items_parent;
DROP TABLE attachments;
DROP TABLE okrs;
ALTER TABLE work_items DROP COLUMN workflow_run_id;
ALTER TABLE work_items DROP COLUMN progress;
ALTER TABLE work_items DROP COLUMN spent_hours;
ALTER TABLE work_items DROP COLUMN estimate_hours;
ALTER TABLE work_items DROP COLUMN parent_id;
