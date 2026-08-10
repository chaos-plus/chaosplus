-- +goose Up
-- PRD §16 node_executions: per-node attempt projection (run/def persistence
-- lives in 00011_workflow_runs / 00012_workflows from the merged base).
CREATE TABLE node_executions (
    run_id       TEXT NOT NULL,
    node_id      TEXT NOT NULL,
    attempt      INTEGER NOT NULL DEFAULT 1,
    status       TEXT NOT NULL DEFAULT 'pending',
    started_at   INTEGER NOT NULL DEFAULT 0,
    completed_at INTEGER NOT NULL DEFAULT 0,
    error        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (run_id, node_id, attempt)
);
CREATE INDEX idx_node_executions_run ON node_executions(run_id);

-- +goose Down
DROP TABLE IF EXISTS node_executions;
