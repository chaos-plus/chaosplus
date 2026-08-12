-- +goose Up
CREATE TABLE workflows_tenant_scoped (
    id          TEXT NOT NULL,
    version     TEXT NOT NULL DEFAULT '1',
    name        TEXT NOT NULL DEFAULT '',
    def_json    TEXT NOT NULL DEFAULT '{}',
    instance_id TEXT NOT NULL DEFAULT '',
    owner_id    TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (instance_id, id, version)
);

INSERT INTO workflows_tenant_scoped
    (id, version, name, def_json, instance_id, owner_id, created_at, updated_at)
SELECT id, version, name, def_json, instance_id, owner_id, created_at, updated_at
FROM workflows;

DROP TABLE workflows;
ALTER TABLE workflows_tenant_scoped RENAME TO workflows;

-- +goose Down
CREATE TABLE workflows_global (
    id          TEXT NOT NULL,
    version     TEXT NOT NULL DEFAULT '1',
    name        TEXT NOT NULL DEFAULT '',
    def_json    TEXT NOT NULL DEFAULT '{}',
    instance_id TEXT NOT NULL DEFAULT '',
    owner_id    TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (id, version)
);

INSERT OR IGNORE INTO workflows_global
    (id, version, name, def_json, instance_id, owner_id, created_at, updated_at)
SELECT id, version, name, def_json, instance_id, owner_id, created_at, updated_at
FROM workflows;

DROP TABLE workflows;
ALTER TABLE workflows_global RENAME TO workflows;
