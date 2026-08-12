-- +goose Up
ALTER TABLE iam_audit_events ADD COLUMN sequence BIGINT NOT NULL DEFAULT 0;
ALTER TABLE iam_audit_events ADD COLUMN previous_hash VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE iam_audit_events ADD COLUMN event_hash VARCHAR(64) NOT NULL DEFAULT '';
CREATE UNIQUE INDEX uq_iam_audit_chain_sequence ON iam_audit_events (tenant_id, sequence) WHERE sequence > 0;
CREATE UNIQUE INDEX uq_iam_audit_chain_hash ON iam_audit_events (tenant_id, event_hash) WHERE event_hash <> '';
CREATE TABLE iam_audit_heads (
    tenant_id BIGINT PRIMARY KEY,
    sequence BIGINT NOT NULL DEFAULT 0,
    event_hash VARCHAR(64) NOT NULL DEFAULT '',
    updated_at BIGINT NOT NULL
);
-- +goose StatementBegin
CREATE FUNCTION reject_iam_audit_event_change() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'iam_audit_events is append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER trg_iam_audit_events_no_update BEFORE UPDATE ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION reject_iam_audit_event_change();
CREATE TRIGGER trg_iam_audit_events_no_delete BEFORE DELETE ON iam_audit_events FOR EACH ROW EXECUTE FUNCTION reject_iam_audit_event_change();

-- +goose Down
DROP TRIGGER trg_iam_audit_events_no_delete ON iam_audit_events;
DROP TRIGGER trg_iam_audit_events_no_update ON iam_audit_events;
DROP FUNCTION reject_iam_audit_event_change();
DROP TABLE iam_audit_heads;
DROP INDEX uq_iam_audit_chain_hash;
DROP INDEX uq_iam_audit_chain_sequence;
ALTER TABLE iam_audit_events DROP COLUMN event_hash;
ALTER TABLE iam_audit_events DROP COLUMN previous_hash;
ALTER TABLE iam_audit_events DROP COLUMN sequence;