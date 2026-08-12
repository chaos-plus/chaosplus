-- +goose Up

CREATE TABLE iam_principals (
    id BIGINT PRIMARY KEY,
    login_name VARCHAR(200) NOT NULL UNIQUE,
    email VARCHAR(320) NOT NULL DEFAULT '',
    display_name VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('active', 'disabled', 'locked')),
    created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL, disabled_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_iam_principals_email ON iam_principals (email);
CREATE TABLE iam_credentials (
    principal_id BIGINT PRIMARY KEY REFERENCES iam_principals(id) ON DELETE CASCADE,
    password_hash TEXT NOT NULL, totp_secret TEXT NOT NULL DEFAULT '', mfa_required BOOLEAN NOT NULL DEFAULT FALSE,
    failed_attempts INTEGER NOT NULL DEFAULT 0, locked_until BIGINT NOT NULL DEFAULT 0,
    password_changed_at BIGINT NOT NULL, updated_at BIGINT NOT NULL
);
CREATE TABLE iam_sessions (
    id_hash CHAR(64) PRIMARY KEY, principal_id VARCHAR(64) NOT NULL REFERENCES iam_principals(id) ON DELETE CASCADE,
    created_at BIGINT NOT NULL, last_seen_at BIGINT NOT NULL, expires_at BIGINT NOT NULL,
    absolute_expires_at BIGINT NOT NULL, revoked_at BIGINT NOT NULL DEFAULT 0,
    ip_address VARCHAR(64) NOT NULL DEFAULT '', user_agent VARCHAR(512) NOT NULL DEFAULT ''
);
CREATE INDEX idx_iam_sessions_principal ON iam_sessions (principal_id, revoked_at, expires_at);
CREATE TABLE iam_refresh_tokens (
    id_hash CHAR(64) PRIMARY KEY, family_id VARCHAR(64) NOT NULL,
    principal_id BIGINT NOT NULL REFERENCES iam_principals(id) ON DELETE CASCADE,
    client_id BIGINT NOT NULL, scope TEXT NOT NULL DEFAULT '', created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL, used_at BIGINT NOT NULL DEFAULT 0, revoked_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX idx_iam_refresh_family ON iam_refresh_tokens (family_id, revoked_at);
CREATE TABLE iam_oauth_clients (
    id BIGINT PRIMARY KEY, tenant_id VARCHAR(128) NOT NULL, secret_hash TEXT NOT NULL DEFAULT '', name VARCHAR(128) NOT NULL,
    redirect_uris TEXT NOT NULL DEFAULT '[]', grant_types TEXT NOT NULL DEFAULT 'authorization_code,refresh_token',
    scopes TEXT NOT NULL DEFAULT 'openid profile email', public_client BOOLEAN NOT NULL DEFAULT TRUE,
    status VARCHAR(16) NOT NULL DEFAULT 'active', created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL
);
CREATE INDEX idx_iam_oauth_clients_tenant ON iam_oauth_clients (tenant_id,status,name);
CREATE TABLE iam_oauth_codes (
    code_hash CHAR(64) PRIMARY KEY, client_id VARCHAR(128) NOT NULL REFERENCES iam_oauth_clients(id) ON DELETE CASCADE,
    principal_id BIGINT NOT NULL REFERENCES iam_principals(id) ON DELETE CASCADE,
    redirect_uri TEXT NOT NULL, scope TEXT NOT NULL, code_challenge VARCHAR(128) NOT NULL,
    nonce VARCHAR(256) NOT NULL DEFAULT '', created_at BIGINT NOT NULL, expires_at BIGINT NOT NULL, consumed_at BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE iam_entities (
    tenant_id BIGINT NOT NULL, id VARCHAR(64) NOT NULL, parent_id VARCHAR(64), type VARCHAR(64) NOT NULL,
    name VARCHAR(200) NOT NULL, status VARCHAR(16) NOT NULL DEFAULT 'active', metadata TEXT NOT NULL DEFAULT '{}',
    created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL, PRIMARY KEY (tenant_id,id),
    UNIQUE (tenant_id,parent_id,type,name),
    FOREIGN KEY (tenant_id,parent_id) REFERENCES iam_entities(tenant_id,id) ON DELETE RESTRICT
);
CREATE INDEX idx_iam_entities_tree ON iam_entities (tenant_id,parent_id,type,status);
CREATE TABLE iam_role_bindings (
    tenant_id BIGINT NOT NULL, role_id VARCHAR(32) NOT NULL, principal_id VARCHAR(64) NOT NULL,
    scope_type VARCHAR(32) NOT NULL DEFAULT 'tenant', scope_id VARCHAR(64) NOT NULL,
    effect VARCHAR(8) NOT NULL DEFAULT 'allow' CHECK (effect IN ('allow','deny')),
    expires_at BIGINT NOT NULL DEFAULT 0, created_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id,role_id,principal_id,scope_type,scope_id),
    FOREIGN KEY (tenant_id,role_id) REFERENCES iam_roles(tenant_id,id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_role_bindings_subject ON iam_role_bindings (tenant_id,principal_id,scope_type,scope_id,expires_at);
CREATE TABLE iam_audit_events (
    id BIGINT PRIMARY KEY, tenant_id VARCHAR(128) NOT NULL DEFAULT '', principal_id VARCHAR(64) NOT NULL DEFAULT '',
    event_type VARCHAR(64) NOT NULL, target_type VARCHAR(64) NOT NULL DEFAULT '', target_id VARCHAR(128) NOT NULL DEFAULT '',
    outcome VARCHAR(16) NOT NULL, ip_address VARCHAR(64) NOT NULL DEFAULT '', user_agent VARCHAR(512) NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT '{}', created_at BIGINT NOT NULL
);
CREATE INDEX idx_iam_audit_tenant_time ON iam_audit_events (tenant_id,created_at);
CREATE INDEX idx_iam_audit_principal_time ON iam_audit_events (principal_id,created_at);
CREATE TABLE iam_oauth_consents (
    principal_id BIGINT NOT NULL,
    client_id    BIGINT NOT NULL,
    tenant_id    BIGINT NOT NULL,
    scope        TEXT NOT NULL DEFAULT '',
    created_at   BIGINT NOT NULL,
    last_used_at BIGINT NOT NULL,
    PRIMARY KEY (principal_id, client_id),
    FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE,
    FOREIGN KEY (client_id) REFERENCES iam_oauth_clients (id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id) REFERENCES iam_tenants (id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE iam_audit_events;
DROP TABLE iam_role_bindings;
DROP TABLE iam_entities;
DROP TABLE iam_oauth_consents;
DROP TABLE iam_oauth_codes;
DROP TABLE iam_oauth_clients;
DROP TABLE iam_refresh_tokens;
DROP TABLE iam_sessions;
DROP TABLE iam_credentials;
DROP TABLE iam_principals;