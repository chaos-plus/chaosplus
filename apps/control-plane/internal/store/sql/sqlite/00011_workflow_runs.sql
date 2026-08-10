-- workflow_runs: persists run definitions across control-plane restarts.
-- RunManager rehydrates non-terminal runs from this table on boot so the UI
-- can show run history; goroutines are NOT resumed (v1 limitation).
CREATE TABLE IF NOT EXISTS workflow_runs (
    id          TEXT PRIMARY KEY,
    def_json    TEXT NOT NULL DEFAULT '{}',  -- WorkflowDef JSON
    status      TEXT NOT NULL DEFAULT 'running',  -- running|waiting_approval|completed|failed|paused
    created_at  INTEGER NOT NULL DEFAULT 0,       -- unix millis
    updated_at  INTEGER NOT NULL DEFAULT 0
);
