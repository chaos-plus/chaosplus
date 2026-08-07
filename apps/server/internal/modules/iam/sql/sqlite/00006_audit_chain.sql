-- +goose Up
ALTER TABLE iam_audit_events ADD COLUMN sequence BIGINT NOT NULL DEFAULT 0;
ALTER TABLE iam_audit_events ADD COLUMN previous_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE iam_audit_events ADD COLUMN event_hash TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX uq_iam_audit_chain_sequence ON iam_audit_events (tenant_id, sequence) WHERE sequence > 0;
CREATE UNIQUE INDEX uq_iam_audit_chain_hash ON iam_audit_events (tenant_id, event_hash) WHERE event_hash <> '';
CREATE TABLE iam_audit_heads (
    tenant_id TEXT NOT NULL PRIMARY KEY,
    sequence BIGINT NOT NULL DEFAULT 0,
    event_hash TEXT NOT NULL DEFAULT '',
    updated_at BIGINT NOT NULL
);
-- +goose StatementBegin
CREATE TRIGGER trg_iam_audit_events_no_update
BEFORE UPDATE ON iam_audit_events
BEGIN
    SELECT RAISE(ABORT, 'iam_audit_events is append-only');
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER trg_iam_audit_events_no_delete
BEFORE DELETE ON iam_audit_events
BEGIN
    SELECT RAISE(ABORT, 'iam_audit_events is append-only');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER trg_iam_audit_events_no_delete;
DROP TRIGGER trg_iam_audit_events_no_update;
DROP TABLE iam_audit_heads;
DROP INDEX uq_iam_audit_chain_hash;
DROP INDEX uq_iam_audit_chain_sequence;
ALTER TABLE iam_audit_events DROP COLUMN event_hash;
ALTER TABLE iam_audit_events DROP COLUMN previous_hash;
ALTER TABLE iam_audit_events DROP COLUMN sequence;
