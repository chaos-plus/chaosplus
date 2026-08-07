-- +goose Up
CREATE TABLE iam_invitations (
    tenant_id TEXT NOT NULL,
    id TEXT NOT NULL,
    email TEXT NOT NULL,
    email_key TEXT NOT NULL,
    token_hmac TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'accepted', 'revoked')),
    department_id TEXT NOT NULL DEFAULT '',
    expires_at BIGINT NOT NULL,
    accepted_by TEXT NOT NULL DEFAULT '',
    accepted_at BIGINT NOT NULL DEFAULT 0,
    revoked_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (id),
    UNIQUE (token_hmac),
    FOREIGN KEY (tenant_id) REFERENCES iam_tenants (id) ON DELETE CASCADE
);
CREATE INDEX idx_iam_invitations_email ON iam_invitations (tenant_id, email_key, status);
CREATE INDEX idx_iam_invitations_expiry ON iam_invitations (status, expires_at);

CREATE TABLE iam_invitation_roles (
    tenant_id TEXT NOT NULL,
    invitation_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    PRIMARY KEY (tenant_id, invitation_id, role_id),
    FOREIGN KEY (tenant_id, invitation_id) REFERENCES iam_invitations (tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, role_id) REFERENCES iam_roles (tenant_id, id) ON DELETE RESTRICT
);

-- +goose Down
DROP TABLE IF EXISTS iam_invitation_roles;
DROP TABLE IF EXISTS iam_invitations;
