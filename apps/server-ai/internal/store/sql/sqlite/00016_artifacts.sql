-- +goose Up
CREATE TABLE artifacts (
    id               TEXT PRIMARY KEY,
    instance_id      TEXT NOT NULL DEFAULT '',
    project_id       TEXT NOT NULL DEFAULT '',
    logical_id       TEXT NOT NULL,
    logical_path     TEXT NOT NULL,
    type             TEXT NOT NULL DEFAULT 'file',
    checksum         TEXT NOT NULL,
    size_bytes       INTEGER NOT NULL DEFAULT 0,
    status           TEXT NOT NULL DEFAULT 'valid',
    force_valid      INTEGER NOT NULL DEFAULT 0,
    producer_run_id  TEXT NOT NULL DEFAULT '',
    producer_node_id TEXT NOT NULL DEFAULT '',
    attempt          INTEGER NOT NULL DEFAULT 1,
    runner_id        TEXT NOT NULL DEFAULT '',
    spawn_id         TEXT NOT NULL DEFAULT '',
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    UNIQUE (instance_id, project_id, logical_path)
);
CREATE INDEX idx_artifacts_project_status ON artifacts(instance_id, project_id, status);
CREATE INDEX idx_artifacts_logical_id ON artifacts(instance_id, project_id, logical_id);

CREATE TABLE artifact_deps (
    artifact_id    TEXT NOT NULL,
    depends_on_id  TEXT NOT NULL,
    input_checksum TEXT NOT NULL,
    created_at     INTEGER NOT NULL,
    PRIMARY KEY (artifact_id, depends_on_id),
    FOREIGN KEY (artifact_id) REFERENCES artifacts(id) ON DELETE CASCADE,
    FOREIGN KEY (depends_on_id) REFERENCES artifacts(id) ON DELETE RESTRICT
);
CREATE INDEX idx_artifact_deps_input ON artifact_deps(depends_on_id);

-- +goose Down
DROP TABLE IF EXISTS artifact_deps;
DROP TABLE IF EXISTS artifacts;
