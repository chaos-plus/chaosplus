-- +goose Up
-- PRD F.2 / F.6: human-approval verdicts and structured rejections as audit
-- projections. REVIEW_APPROVED writes validation_results; REVIEW_REJECTED also
-- writes feedback_log (the structured {category,location,expected,detail}).
CREATE TABLE validation_results (
    id            TEXT PRIMARY KEY,
    artifact_id   TEXT NOT NULL DEFAULT '',
    execution_id  TEXT NOT NULL DEFAULT '',
    validator_id  TEXT NOT NULL DEFAULT 'human',
    validator_type TEXT NOT NULL DEFAULT 'human',
    passed        INTEGER NOT NULL,
    evidence_json TEXT NOT NULL DEFAULT '{}',
    reviewed_by   TEXT NOT NULL DEFAULT '',
    ts            INTEGER NOT NULL
);
CREATE TABLE feedback_log (
    id           TEXT PRIMARY KEY,
    artifact_id  TEXT NOT NULL DEFAULT '',
    execution_id TEXT NOT NULL DEFAULT '',
    reviewer     TEXT NOT NULL DEFAULT '',
    category     TEXT NOT NULL DEFAULT '',
    location     TEXT NOT NULL DEFAULT '',
    expected     TEXT NOT NULL DEFAULT '',
    detail       TEXT NOT NULL DEFAULT '',
    ts           INTEGER NOT NULL
);
CREATE INDEX idx_validation_results_artifact ON validation_results(artifact_id);
CREATE INDEX idx_feedback_log_artifact ON feedback_log(artifact_id);

-- +goose Down
DROP TABLE IF EXISTS feedback_log;
DROP TABLE IF EXISTS validation_results;
