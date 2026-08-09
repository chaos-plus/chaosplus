-- +goose Up
-- PRD §16 / §15.1: persistent run + node execution records so runs survive a
-- restart (as history) and in-flight runs are crash-recovered to failed.
CREATE TABLE workflow_runs (
    id                    TEXT PRIMARY KEY,
    workflow_id           TEXT NOT NULL DEFAULT '',
    workflow_version      TEXT NOT NULL DEFAULT '',
    workflow_def_snapshot TEXT NOT NULL DEFAULT '{}',
    status                TEXT NOT NULL DEFAULT 'running',
    workspace             TEXT NOT NULL DEFAULT '',
    started_at            INTEGER NOT NULL,
    completed_at          INTEGER NOT NULL DEFAULT 0
);
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
CREATE INDEX idx_workflow_runs_status ON workflow_runs(status);
CREATE INDEX idx_node_executions_run ON node_executions(run_id);

-- +goose Down
DROP TABLE IF EXISTS node_executions;
DROP TABLE IF EXISTS workflow_runs;
