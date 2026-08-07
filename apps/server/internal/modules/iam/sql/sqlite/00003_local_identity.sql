-- +goose Up
DROP TABLE IF EXISTS authz_outbox;

CREATE TABLE iam_principals (
    id TEXT NOT NULL PRIMARY KEY,
    login_name TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled', 'locked')),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    disabled_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_iam_principals_email ON iam_principals (email);

CREATE TABLE iam_credentials (
    principal_id TEXT NOT NULL PRIMARY KEY,
    password_hash TEXT NOT NULL,
    totp_secret TEXT NOT NULL DEFAULT '',
    mfa_required INTEGER NOT NULL DEFAULT 0,
    failed_attempts INTEGER NOT NULL DEFAULT 0,
    locked_until BIGINT NOT NULL DEFAULT 0,
    password_changed_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE
);

CREATE TABLE iam_sessions (
    id_hash TEXT NOT NULL PRIMARY KEY,
    principal_id TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    last_seen_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    absolute_expires_at BIGINT NOT NULL,
    revoked_at BIGINT NOT NULL DEFAULT 0,
    ip_address TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_sessions_principal ON iam_sessions (principal_id, revoked_at, expires_at);

CREATE TABLE iam_refresh_tokens (
    id_hash TEXT NOT NULL PRIMARY KEY,
    family_id TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    used_at BIGINT NOT NULL DEFAULT 0,
    revoked_at BIGINT NOT NULL DEFAULT 0,
    FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_refresh_family ON iam_refresh_tokens (family_id, revoked_at);

CREATE TABLE iam_oauth_clients (
    id TEXT NOT NULL PRIMARY KEY,
	tenant_id TEXT NOT NULL,
    secret_hash TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    redirect_uris TEXT NOT NULL DEFAULT '[]',
    grant_types TEXT NOT NULL DEFAULT 'authorization_code,refresh_token',
    scopes TEXT NOT NULL DEFAULT 'openid profile email',
    public_client INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'active',
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);
CREATE INDEX idx_iam_oauth_clients_tenant ON iam_oauth_clients (tenant_id, status, name);

CREATE TABLE iam_oauth_codes (
    code_hash TEXT NOT NULL PRIMARY KEY,
    client_id TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    scope TEXT NOT NULL,
    code_challenge TEXT NOT NULL,
    nonce TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL,
    consumed_at BIGINT NOT NULL DEFAULT 0,
    FOREIGN KEY (client_id) REFERENCES iam_oauth_clients (id) ON DELETE CASCADE,
    FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE
);

CREATE TABLE iam_entities (
    tenant_id TEXT NOT NULL,
    id TEXT NOT NULL,
    parent_id TEXT NULL,
    type TEXT NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, parent_id, type, name),
    FOREIGN KEY (tenant_id, parent_id) REFERENCES iam_entities (tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX idx_iam_entities_tree ON iam_entities (tenant_id, parent_id, type, status);

CREATE TABLE iam_role_bindings (
    tenant_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    scope_type TEXT NOT NULL DEFAULT 'tenant',
    scope_id TEXT NOT NULL,
    effect TEXT NOT NULL DEFAULT 'allow' CHECK (effect IN ('allow', 'deny')),
    expires_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, role_id, principal_id, scope_type, scope_id),
    FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles (tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_role_bindings_subject ON iam_role_bindings (tenant_id, principal_id, scope_type, scope_id, expires_at);

CREATE TABLE iam_audit_events (
    id TEXT NOT NULL PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '',
    principal_id TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL,
    target_type TEXT NOT NULL DEFAULT '',
    target_id TEXT NOT NULL DEFAULT '',
    outcome TEXT NOT NULL,
    ip_address TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT '{}',
    created_at BIGINT NOT NULL
);
CREATE INDEX idx_iam_audit_tenant_time ON iam_audit_events (tenant_id, created_at);
CREATE INDEX idx_iam_audit_principal_time ON iam_audit_events (principal_id, created_at);

-- +goose Down
DROP TABLE iam_audit_events;
DROP TABLE iam_role_bindings;
DROP TABLE iam_entities;
DROP TABLE iam_oauth_codes;
DROP TABLE iam_oauth_clients;
DROP TABLE iam_refresh_tokens;
DROP TABLE iam_sessions;
DROP TABLE iam_credentials;
DROP TABLE iam_principals;
