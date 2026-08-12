-- +goose Up
CREATE TABLE iam_positions (
    tenant_id BIGINT NOT NULL,
    id BIGINT NOT NULL,
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0 AND sort_order <= 1000000),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT uq_iam_positions_code UNIQUE (tenant_id, code)
);
CREATE INDEX idx_iam_positions_order ON iam_positions (tenant_id, sort_order, code, id);

CREATE TABLE iam_position_members (
    tenant_id BIGINT NOT NULL,
    position_id BIGINT NOT NULL,
    principal_id BIGINT NOT NULL,
    starts_at BIGINT NOT NULL DEFAULT 0 CHECK (starts_at >= 0),
    ends_at BIGINT NOT NULL DEFAULT 0 CHECK (ends_at >= 0 AND (ends_at = 0 OR starts_at = 0 OR ends_at > starts_at)),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, position_id, principal_id),
    FOREIGN KEY (tenant_id, position_id) REFERENCES iam_positions (tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, principal_id) REFERENCES iam_tenant_members (tenant_id, principal_id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_position_members_principal ON iam_position_members (tenant_id, principal_id, starts_at, ends_at, position_id);

-- +goose Down
DROP TABLE IF EXISTS iam_position_members;
DROP TABLE IF EXISTS iam_positions;