-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys = OFF;
BEGIN;
CREATE TABLE iam_groups_next (
    tenant_id TEXT NOT NULL,
    id TEXT NOT NULL,
    name TEXT NOT NULL,
    name_key TEXT NOT NULL,
    group_type TEXT NOT NULL CHECK (group_type IN ('static', 'dynamic')),
    rule_json TEXT NOT NULL DEFAULT '' CHECK (length(rule_json) <= 4096),
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0 AND sort_order <= 1000000),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT uq_iam_groups_name UNIQUE (tenant_id, name_key),
    CHECK ((group_type = 'static' AND rule_json = '') OR (group_type = 'dynamic' AND rule_json <> ''))
);
INSERT INTO iam_groups_next
    (tenant_id,id,name,name_key,group_type,rule_json,description,status,sort_order,version,created_at,updated_at)
SELECT tenant_id,id,name,name_key,group_type,'',description,status,sort_order,version,created_at,updated_at
FROM iam_groups;
DROP TABLE iam_groups;
ALTER TABLE iam_groups_next RENAME TO iam_groups;
CREATE INDEX idx_iam_groups_order ON iam_groups (tenant_id, sort_order, name_key, id);
COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
CREATE TEMP TABLE IF NOT EXISTS iam_dynamic_group_rollback_guard (
    dynamic_count INTEGER NOT NULL CHECK (dynamic_count = 0)
);
DELETE FROM iam_dynamic_group_rollback_guard;
INSERT INTO iam_dynamic_group_rollback_guard
SELECT COUNT(*) FROM iam_groups WHERE group_type = 'dynamic';
DROP TABLE iam_dynamic_group_rollback_guard;
PRAGMA foreign_keys = OFF;
BEGIN;
CREATE TABLE iam_groups_previous (
    tenant_id TEXT NOT NULL,
    id TEXT NOT NULL,
    name TEXT NOT NULL,
    name_key TEXT NOT NULL,
    group_type TEXT NOT NULL CHECK (group_type = 'static'),
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0 AND sort_order <= 1000000),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT uq_iam_groups_name UNIQUE (tenant_id, name_key)
);
INSERT INTO iam_groups_previous
    (tenant_id,id,name,name_key,group_type,description,status,sort_order,version,created_at,updated_at)
SELECT tenant_id,id,name,name_key,group_type,description,status,sort_order,version,created_at,updated_at
FROM iam_groups;
DROP TABLE iam_groups;
ALTER TABLE iam_groups_previous RENAME TO iam_groups;
CREATE INDEX idx_iam_groups_order ON iam_groups (tenant_id, sort_order, name_key, id);
COMMIT;
PRAGMA foreign_keys = ON;
