-- workflows: persisted workflow definitions (PRD §16).
-- Each row is one workflow version. id + version = unique.
CREATE TABLE IF NOT EXISTS workflows (
    id          TEXT NOT NULL,
    version     TEXT NOT NULL DEFAULT '1',
    name        TEXT NOT NULL DEFAULT '',
    def_json    TEXT NOT NULL DEFAULT '{}',   -- WorkflowDef JSON
    instance_id TEXT NOT NULL DEFAULT '',
    owner_id    TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (id, version)
);
