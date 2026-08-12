-- +goose Up
CREATE TABLE artifacts (
    id BIGINT PRIMARY KEY CHECK (id > 0),
    tenant_id BIGINT NOT NULL CHECK (tenant_id > 0),
    entity_id BIGINT NOT NULL CHECK (entity_id > 0),
    project_id BIGINT NOT NULL CHECK (project_id > 0),
    owner_id BIGINT NOT NULL CHECK (owner_id > 0),
    logical_key TEXT NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 255),
    logical_path TEXT NOT NULL CHECK (length(logical_path) BETWEEN 1 AND 2048),
    type TEXT NOT NULL CHECK (type IN ('file', 'json', 'text', 'image', 'audio', 'video', 'archive', 'directory')),
    checksum TEXT NOT NULL CHECK (length(checksum) BETWEEN 1 AND 255),
    size_bytes BIGINT NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    status TEXT NOT NULL DEFAULT 'valid' CHECK (status IN ('valid', 'stale', 'invalid', 'orphaned')),
    force_valid BOOLEAN NOT NULL DEFAULT FALSE CHECK (force_valid IN (FALSE, TRUE)),
    producer_run_id BIGINT NOT NULL CHECK (producer_run_id > 0),
    producer_node_key TEXT NOT NULL CHECK (length(producer_node_key) BETWEEN 1 AND 255),
    attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt >= 1),
    runner_handle TEXT NOT NULL DEFAULT '',
    spawn_handle TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL CHECK (created_at > 0),
    created_by BIGINT NOT NULL CHECK (created_by > 0),
    updated_at BIGINT NOT NULL CHECK (updated_at > 0),
    updated_by BIGINT NOT NULL CHECK (updated_by > 0),
    deleted_at BIGINT NOT NULL DEFAULT 0 CHECK (deleted_at >= 0),
    deleted_by BIGINT NOT NULL DEFAULT 0 CHECK (deleted_by >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    UNIQUE (tenant_id, entity_id, project_id, logical_path),
    CHECK ((deleted_at = 0 AND deleted_by = 0) OR (deleted_at > 0 AND deleted_by > 0))
);
CREATE INDEX idx_artifacts_scope_status ON artifacts (tenant_id, entity_id, project_id, deleted_at, status, id);
CREATE INDEX idx_artifacts_logical_key ON artifacts (tenant_id, entity_id, project_id, logical_key, deleted_at);

CREATE TABLE artifact_deps (
    tenant_id BIGINT NOT NULL CHECK (tenant_id > 0),
    entity_id BIGINT NOT NULL CHECK (entity_id > 0),
    artifact_id BIGINT NOT NULL CHECK (artifact_id > 0),
    depends_on_id BIGINT NOT NULL CHECK (depends_on_id > 0),
    input_checksum TEXT NOT NULL CHECK (length(input_checksum) BETWEEN 1 AND 255),
    created_at BIGINT NOT NULL CHECK (created_at > 0),
    created_by BIGINT NOT NULL CHECK (created_by > 0),
    PRIMARY KEY (tenant_id, entity_id, artifact_id, depends_on_id),
    FOREIGN KEY (artifact_id) REFERENCES artifacts (id) ON DELETE CASCADE,
    FOREIGN KEY (depends_on_id) REFERENCES artifacts (id) ON DELETE RESTRICT,
    CHECK (artifact_id <> depends_on_id)
);
CREATE INDEX idx_artifact_deps_input ON artifact_deps (tenant_id, entity_id, depends_on_id);

CREATE TABLE validation_results (
    id BIGINT PRIMARY KEY CHECK (id > 0), tenant_id BIGINT NOT NULL CHECK (tenant_id > 0), entity_id BIGINT NOT NULL CHECK (entity_id > 0),
    artifact_id BIGINT NOT NULL CHECK (artifact_id > 0), execution_key TEXT NOT NULL DEFAULT '', validator_key TEXT NOT NULL CHECK (length(validator_key) BETWEEN 1 AND 255),
    validator_type TEXT NOT NULL CHECK (validator_type IN ('automated', 'ai_assisted', 'human')), passed BOOLEAN NOT NULL CHECK (passed IN (FALSE, TRUE)),
    evidence_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(evidence_json)), reviewed_by BIGINT NOT NULL CHECK (reviewed_by > 0), ts BIGINT NOT NULL CHECK (ts > 0),
    FOREIGN KEY (artifact_id) REFERENCES artifacts (id) ON DELETE CASCADE
);
CREATE INDEX idx_validation_results_subject ON validation_results (tenant_id, entity_id, artifact_id, ts DESC, id);

CREATE TABLE feedback_log (
    id BIGINT PRIMARY KEY CHECK (id > 0), tenant_id BIGINT NOT NULL CHECK (tenant_id > 0), entity_id BIGINT NOT NULL CHECK (entity_id > 0),
    artifact_id BIGINT NOT NULL CHECK (artifact_id > 0), execution_key TEXT NOT NULL DEFAULT '', reviewer_id BIGINT NOT NULL CHECK (reviewer_id > 0),
    category TEXT NOT NULL CHECK (length(category) BETWEEN 1 AND 128), location TEXT NOT NULL DEFAULT '', expected TEXT NOT NULL DEFAULT '', detail TEXT NOT NULL CHECK (length(detail) BETWEEN 1 AND 8192),
    ts BIGINT NOT NULL CHECK (ts > 0), FOREIGN KEY (artifact_id) REFERENCES artifacts (id) ON DELETE CASCADE
);
CREATE INDEX idx_feedback_log_subject ON feedback_log (tenant_id, entity_id, artifact_id, ts DESC, id);

-- +goose Down
DROP TABLE IF EXISTS feedback_log;
DROP TABLE IF EXISTS validation_results;
DROP TABLE IF EXISTS artifact_deps;
DROP TABLE IF EXISTS artifacts;
