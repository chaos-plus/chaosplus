-- +goose Up
CREATE TABLE run_leases (
    run_id         TEXT PRIMARY KEY,
    owner_id       TEXT NOT NULL,
    fencing_token  INTEGER NOT NULL,
    expires_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
CREATE INDEX idx_run_leases_expiry ON run_leases(expires_at);

-- +goose Down
DROP TABLE run_leases;
