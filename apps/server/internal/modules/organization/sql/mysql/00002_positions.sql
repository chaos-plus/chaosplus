-- +goose Up
CREATE TABLE iam_positions (
    tenant_id BIGINT NOT NULL,
    id BIGINT NOT NULL,
    code VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 1,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE KEY uq_iam_positions_code (tenant_id, code),
    KEY idx_iam_positions_order (tenant_id, sort_order, code, id),
    CONSTRAINT chk_iam_positions_status CHECK (status IN ('active', 'disabled')),
    CONSTRAINT chk_iam_positions_sort_order CHECK (sort_order >= 0 AND sort_order <= 1000000),
    CONSTRAINT chk_iam_positions_version CHECK (version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_position_members (
    tenant_id BIGINT NOT NULL,
    position_id BIGINT NOT NULL,
    principal_id BIGINT NOT NULL,
    starts_at BIGINT NOT NULL DEFAULT 0,
    ends_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, position_id, principal_id),
    KEY idx_iam_position_members_principal (tenant_id, principal_id, starts_at, ends_at, position_id),
    CONSTRAINT fk_iam_position_members_position FOREIGN KEY (tenant_id, position_id) REFERENCES iam_positions (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_iam_position_members_principal FOREIGN KEY (tenant_id, principal_id) REFERENCES iam_tenant_members (tenant_id, principal_id) ON DELETE CASCADE,
    CONSTRAINT chk_iam_position_members_starts CHECK (starts_at >= 0),
    CONSTRAINT chk_iam_position_members_ends CHECK (ends_at >= 0 AND (ends_at = 0 OR starts_at = 0 OR ends_at > starts_at))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS iam_position_members;
DROP TABLE IF EXISTS iam_positions;