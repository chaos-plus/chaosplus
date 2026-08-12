-- +goose Up

CREATE TABLE iam_principals (
    id BIGINT PRIMARY KEY, login_name VARCHAR(200) NOT NULL UNIQUE, email VARCHAR(320) NOT NULL DEFAULT '',
    display_name VARCHAR(128) NOT NULL, status VARCHAR(16) NOT NULL,
    created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL, disabled_at BIGINT NOT NULL DEFAULT 0,
    KEY idx_iam_principals_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE iam_credentials (
    principal_id BIGINT PRIMARY KEY, password_hash TEXT NOT NULL, totp_secret TEXT NOT NULL,
    mfa_required BOOLEAN NOT NULL DEFAULT FALSE, failed_attempts INT NOT NULL DEFAULT 0,
    locked_until BIGINT NOT NULL DEFAULT 0, password_changed_at BIGINT NOT NULL, updated_at BIGINT NOT NULL,
    CONSTRAINT fk_iam_credentials_principal FOREIGN KEY (principal_id) REFERENCES iam_principals(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE iam_sessions (
    id_hash CHAR(64) PRIMARY KEY, principal_id VARCHAR(64) NOT NULL, created_at BIGINT NOT NULL,
    last_seen_at BIGINT NOT NULL, expires_at BIGINT NOT NULL, absolute_expires_at BIGINT NOT NULL,
    revoked_at BIGINT NOT NULL DEFAULT 0, ip_address VARCHAR(64) NOT NULL DEFAULT '', user_agent VARCHAR(512) NOT NULL DEFAULT '',
    KEY idx_iam_sessions_principal (principal_id,revoked_at,expires_at),
    CONSTRAINT fk_iam_sessions_principal FOREIGN KEY (principal_id) REFERENCES iam_principals(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE iam_refresh_tokens (
    id_hash CHAR(64) PRIMARY KEY, family_id VARCHAR(64) NOT NULL, principal_id VARCHAR(64) NOT NULL,
    client_id BIGINT NOT NULL, scope TEXT NOT NULL, created_at BIGINT NOT NULL,
    expires_at BIGINT NOT NULL, used_at BIGINT NOT NULL DEFAULT 0, revoked_at BIGINT NOT NULL DEFAULT 0,
    KEY idx_iam_refresh_family (family_id,revoked_at),
    CONSTRAINT fk_iam_refresh_principal FOREIGN KEY (principal_id) REFERENCES iam_principals(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE iam_oauth_clients (
    id BIGINT PRIMARY KEY, tenant_id VARCHAR(128) NOT NULL, secret_hash TEXT NOT NULL, name VARCHAR(128) NOT NULL, redirect_uris TEXT NOT NULL,
    grant_types TEXT NOT NULL, scopes TEXT NOT NULL, public_client BOOLEAN NOT NULL DEFAULT TRUE,
    status VARCHAR(16) NOT NULL DEFAULT 'active', created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL,
    KEY idx_iam_oauth_clients_tenant (tenant_id,status,name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE iam_oauth_codes (
    code_hash CHAR(64) PRIMARY KEY, client_id VARCHAR(128) NOT NULL, principal_id VARCHAR(64) NOT NULL,
    redirect_uri TEXT NOT NULL, scope TEXT NOT NULL, code_challenge VARCHAR(128) NOT NULL, nonce VARCHAR(256) NOT NULL,
    created_at BIGINT NOT NULL, expires_at BIGINT NOT NULL, consumed_at BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT fk_iam_codes_client FOREIGN KEY (client_id) REFERENCES iam_oauth_clients(id) ON DELETE CASCADE,
    CONSTRAINT fk_iam_codes_principal FOREIGN KEY (principal_id) REFERENCES iam_principals(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE iam_entities (
    tenant_id BIGINT NOT NULL, id VARCHAR(64) NOT NULL, parent_id VARCHAR(64) NULL, type VARCHAR(64) NOT NULL,
    name VARCHAR(200) NOT NULL, status VARCHAR(16) NOT NULL DEFAULT 'active', metadata TEXT NOT NULL,
    created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL, PRIMARY KEY (tenant_id,id),
    UNIQUE KEY uq_iam_entities_name (tenant_id,parent_id,type,name), KEY idx_iam_entities_tree (tenant_id,parent_id,type,status),
    CONSTRAINT fk_iam_entities_parent FOREIGN KEY (tenant_id,parent_id) REFERENCES iam_entities(tenant_id,id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE iam_role_bindings (
    tenant_id BIGINT NOT NULL, role_id VARCHAR(32) NOT NULL, principal_id VARCHAR(64) NOT NULL,
    scope_type VARCHAR(32) NOT NULL DEFAULT 'tenant', scope_id VARCHAR(64) NOT NULL,
    effect VARCHAR(8) NOT NULL DEFAULT 'allow', expires_at BIGINT NOT NULL DEFAULT 0, created_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id,role_id,principal_id,scope_type,scope_id),
    KEY idx_iam_role_bindings_subject (tenant_id,principal_id,scope_type,scope_id,expires_at),
    CONSTRAINT fk_iam_role_bindings_role FOREIGN KEY (tenant_id,role_id) REFERENCES iam_roles(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE iam_audit_events (
    id BIGINT PRIMARY KEY, tenant_id VARCHAR(128) NOT NULL DEFAULT '', principal_id VARCHAR(64) NOT NULL DEFAULT '',
    event_type VARCHAR(64) NOT NULL, target_type VARCHAR(64) NOT NULL DEFAULT '', target_id VARCHAR(128) NOT NULL DEFAULT '',
    outcome VARCHAR(16) NOT NULL, ip_address VARCHAR(64) NOT NULL DEFAULT '', user_agent VARCHAR(512) NOT NULL DEFAULT '',
    detail TEXT NOT NULL, created_at BIGINT NOT NULL,
    KEY idx_iam_audit_tenant_time (tenant_id,created_at), KEY idx_iam_audit_principal_time (principal_id,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_oauth_consents (
    principal_id BIGINT NOT NULL,
    client_id    BIGINT NOT NULL,
    tenant_id    BIGINT NOT NULL,
    scope        TEXT NOT NULL DEFAULT '',
    created_at   BIGINT NOT NULL,
    last_used_at BIGINT NOT NULL,
    PRIMARY KEY (principal_id, client_id),
    CONSTRAINT fk_iam_oauth_consents_principal FOREIGN KEY (principal_id) REFERENCES iam_principals (id) ON DELETE CASCADE,
    CONSTRAINT fk_iam_oauth_consents_client FOREIGN KEY (client_id) REFERENCES iam_oauth_clients (id) ON DELETE CASCADE,
    CONSTRAINT fk_iam_oauth_consents_tenant FOREIGN KEY (tenant_id) REFERENCES iam_tenants (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

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