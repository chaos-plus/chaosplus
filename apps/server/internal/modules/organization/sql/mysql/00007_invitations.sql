-- +goose Up
CREATE TABLE iam_invitations (
    tenant_id BIGINT NOT NULL,
    id BIGINT NOT NULL,
    email VARCHAR(320) NOT NULL,
    email_key VARCHAR(320) NOT NULL,
    token_hmac CHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL,
    department_id BIGINT NOT NULL DEFAULT '',
    expires_at BIGINT NOT NULL,
    accepted_by BIGINT NOT NULL DEFAULT '',
    accepted_at BIGINT NOT NULL DEFAULT 0,
    revoked_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE KEY uq_iam_invitations_id (id),
    UNIQUE KEY uq_iam_invitations_token (token_hmac),
    KEY idx_iam_invitations_email (tenant_id, email_key, status),
    KEY idx_iam_invitations_expiry (status, expires_at),
    CONSTRAINT fk_iam_invitations_tenant FOREIGN KEY (tenant_id) REFERENCES iam_tenants (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE iam_invitation_roles (
    tenant_id BIGINT NOT NULL,
    invitation_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL,
    PRIMARY KEY (tenant_id, invitation_id, role_id),
    CONSTRAINT fk_iam_invitation_roles_invitation FOREIGN KEY (tenant_id, invitation_id)
        REFERENCES iam_invitations (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_iam_invitation_roles_role FOREIGN KEY (tenant_id, role_id)
        REFERENCES iam_roles (tenant_id, id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS iam_invitation_roles;
DROP TABLE IF EXISTS iam_invitations;