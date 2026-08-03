-- +goose Up
ALTER TABLE iam_audit_events
    ADD COLUMN sequence BIGINT NULL,
    ADD COLUMN previous_hash CHAR(64) NULL,
    ADD COLUMN event_hash CHAR(64) NULL;
CREATE UNIQUE INDEX uq_iam_audit_chain_sequence ON iam_audit_events (tenant_id, sequence);
CREATE UNIQUE INDEX uq_iam_audit_chain_hash ON iam_audit_events (tenant_id, event_hash);
CREATE TABLE iam_audit_heads (
    tenant_id VARCHAR(128) PRIMARY KEY,
    sequence BIGINT NOT NULL DEFAULT 0,
    event_hash CHAR(64) NOT NULL DEFAULT '',
    updated_at BIGINT NOT NULL
) ENGINE=InnoDB;
-- +goose StatementBegin
CREATE TRIGGER trg_iam_audit_events_no_update
BEFORE UPDATE ON iam_audit_events FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'iam_audit_events is append-only';
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER trg_iam_audit_events_no_delete
BEFORE DELETE ON iam_audit_events FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'iam_audit_events is append-only';
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER trg_iam_audit_events_no_delete;
DROP TRIGGER trg_iam_audit_events_no_update;
DROP TABLE iam_audit_heads;
DROP INDEX uq_iam_audit_chain_hash ON iam_audit_events;
DROP INDEX uq_iam_audit_chain_sequence ON iam_audit_events;
ALTER TABLE iam_audit_events
    DROP COLUMN event_hash,
    DROP COLUMN previous_hash,
    DROP COLUMN sequence;
