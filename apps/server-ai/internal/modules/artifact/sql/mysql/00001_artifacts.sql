-- +goose Up
CREATE TABLE artifacts (
    id BIGINT NOT NULL PRIMARY KEY, tenant_id BIGINT NOT NULL, entity_id BIGINT NOT NULL, project_id BIGINT NOT NULL, owner_id BIGINT NOT NULL,
    logical_key VARCHAR(255) NOT NULL, logical_path VARCHAR(2048) NOT NULL, type VARCHAR(32) NOT NULL, checksum VARCHAR(255) NOT NULL, size_bytes BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'valid', force_valid BOOLEAN NOT NULL DEFAULT FALSE, producer_run_id BIGINT NOT NULL, producer_node_key VARCHAR(255) NOT NULL, attempt INT NOT NULL DEFAULT 1,
    runner_handle VARCHAR(255) NOT NULL DEFAULT '', spawn_handle VARCHAR(255) NOT NULL DEFAULT '', created_at BIGINT NOT NULL, created_by BIGINT NOT NULL, updated_at BIGINT NOT NULL, updated_by BIGINT NOT NULL,
    deleted_at BIGINT NOT NULL DEFAULT 0, deleted_by BIGINT NOT NULL DEFAULT 0, version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT uq_artifacts_path UNIQUE (tenant_id, entity_id, project_id, logical_path),
    CONSTRAINT chk_artifacts_scope CHECK (id > 0 AND tenant_id > 0 AND entity_id > 0 AND project_id > 0 AND owner_id > 0 AND producer_run_id > 0 AND created_by > 0 AND updated_by > 0),
    CONSTRAINT chk_artifacts_type CHECK (type IN ('file','json','text','image','audio','video','archive','directory')),
    CONSTRAINT chk_artifacts_status CHECK (status IN ('valid','stale','invalid','orphaned')),
    CONSTRAINT chk_artifacts_values CHECK (size_bytes >= 0 AND attempt >= 1 AND created_at > 0 AND updated_at > 0 AND deleted_at >= 0 AND version >= 1),
    CONSTRAINT chk_artifacts_deleted CHECK ((deleted_at = 0 AND deleted_by = 0) OR (deleted_at > 0 AND deleted_by > 0)),
    KEY idx_artifacts_scope_status (tenant_id, entity_id, project_id, deleted_at, status, id), KEY idx_artifacts_logical_key (tenant_id, entity_id, project_id, logical_key, deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE artifact_deps (
    tenant_id BIGINT NOT NULL, entity_id BIGINT NOT NULL, artifact_id BIGINT NOT NULL, depends_on_id BIGINT NOT NULL, input_checksum VARCHAR(255) NOT NULL, created_at BIGINT NOT NULL, created_by BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, entity_id, artifact_id, depends_on_id), CONSTRAINT fk_artifact_deps_artifact FOREIGN KEY (artifact_id) REFERENCES artifacts(id) ON DELETE CASCADE,
    CONSTRAINT fk_artifact_deps_input FOREIGN KEY (depends_on_id) REFERENCES artifacts(id) ON DELETE RESTRICT, CONSTRAINT chk_artifact_deps CHECK (tenant_id > 0 AND entity_id > 0 AND artifact_id > 0 AND depends_on_id > 0 AND artifact_id <> depends_on_id AND created_at > 0 AND created_by > 0),
    KEY idx_artifact_deps_input (tenant_id, entity_id, depends_on_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE validation_results (
    id BIGINT NOT NULL PRIMARY KEY, tenant_id BIGINT NOT NULL, entity_id BIGINT NOT NULL, artifact_id BIGINT NOT NULL, execution_key VARCHAR(255) NOT NULL DEFAULT '', validator_key VARCHAR(255) NOT NULL,
    validator_type VARCHAR(32) NOT NULL, passed BOOLEAN NOT NULL, evidence_json JSON NOT NULL, reviewed_by BIGINT NOT NULL, ts BIGINT NOT NULL,
    CONSTRAINT fk_validation_artifact FOREIGN KEY (artifact_id) REFERENCES artifacts(id) ON DELETE CASCADE, CONSTRAINT chk_validation_scope CHECK (id > 0 AND tenant_id > 0 AND entity_id > 0 AND artifact_id > 0 AND reviewed_by > 0 AND ts > 0),
    CONSTRAINT chk_validation_type CHECK (validator_type IN ('automated','ai_assisted','human')), KEY idx_validation_results_subject (tenant_id, entity_id, artifact_id, ts DESC, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE feedback_log (
    id BIGINT NOT NULL PRIMARY KEY, tenant_id BIGINT NOT NULL, entity_id BIGINT NOT NULL, artifact_id BIGINT NOT NULL, execution_key VARCHAR(255) NOT NULL DEFAULT '', reviewer_id BIGINT NOT NULL,
    category VARCHAR(128) NOT NULL, location VARCHAR(2048) NOT NULL DEFAULT '', expected TEXT NOT NULL, detail TEXT NOT NULL, ts BIGINT NOT NULL,
    CONSTRAINT fk_feedback_artifact FOREIGN KEY (artifact_id) REFERENCES artifacts(id) ON DELETE CASCADE, CONSTRAINT chk_feedback_scope CHECK (id > 0 AND tenant_id > 0 AND entity_id > 0 AND artifact_id > 0 AND reviewer_id > 0 AND ts > 0),
    KEY idx_feedback_log_subject (tenant_id, entity_id, artifact_id, ts DESC, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE IF EXISTS feedback_log;
DROP TABLE IF EXISTS validation_results;
DROP TABLE IF EXISTS artifact_deps;
DROP TABLE IF EXISTS artifacts;
