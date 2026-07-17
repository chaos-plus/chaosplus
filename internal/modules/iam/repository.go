package iam

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
)

type roleRow struct {
	bun.BaseModel `bun:"table:iam_roles"`
	TenantID      string `bun:"tenant_id,pk"`
	ID            string `bun:"id,pk"`
	Name          string
	Description   string
	CreatedAt     int64
	UpdatedAt     int64
}

type permissionRow struct {
	bun.BaseModel  `bun:"table:iam_role_permissions"`
	TenantID       string `bun:"tenant_id,pk"`
	RoleID         string `bun:"role_id,pk"`
	PermissionCode string `bun:"permission_code,pk"`
	CreatedAt      int64
}

type memberRow struct {
	bun.BaseModel `bun:"table:iam_role_members"`
	TenantID      string `bun:"tenant_id,pk"`
	RoleID        string `bun:"role_id,pk"`
	UserSubject   string `bun:"user_subject,pk"`
	CreatedAt     int64
}

type outboxRow struct {
	bun.BaseModel    `bun:"table:authz_outbox"`
	ID               string `bun:"id,pk"`
	TenantID         string
	RelationshipKey  string
	ResourceType     string
	ResourceID       string
	ResourceRelation string
	SubjectType      string
	SubjectID        string
	SubjectRelation  string
	Operation        string
	Version          int64
	Status           string
	Attempts         int
	AvailableAt      int64
	LockedBy         string
	LockedAt         int64
	LastError        string
	ZedToken         string
	CreatedAt        int64
	UpdatedAt        int64
	DeliveredAt      int64
}

type OutboxMessage struct {
	ID           string
	TenantID     string
	Operation    spicedbx.RelationshipOperation
	Relationship spicedbx.Relationship
	Version      int64
	Attempts     int
}

type Repository struct {
	db      *bun.DB
	nextID  IDGenerator
	dialect string
	now     func() time.Time
}

func NewRepository(db *bun.DB, nextID IDGenerator) *Repository {
	if db == nil || nextID == nil {
		panic("iam repository requires database and id generator")
	}
	dialect := db.Dialect().Name().String()
	if dialect == "pg" {
		dialect = "postgres"
	}
	return &Repository{db: db, nextID: nextID, dialect: dialect, now: time.Now}
}

func (r *Repository) CreateRole(ctx context.Context, tenantID, name, description string) (Role, error) {
	id, err := r.nextID()
	if err != nil {
		return Role{}, fmt.Errorf("generate role id: %w", err)
	}
	now := r.now().UTC().UnixMilli()
	row := roleRow{TenantID: tenantID, ID: id, Name: name, Description: description, CreatedAt: now, UpdatedAt: now}
	if _, err := r.db.NewInsert().Model(&row).Exec(ctx); err != nil {
		if isUniqueViolation(err) {
			return Role{}, ErrRoleNameConflict
		}
		return Role{}, fmt.Errorf("insert role: %w", err)
	}
	return roleFromRow(row), nil
}

func (r *Repository) ListRoles(ctx context.Context, tenantID string) ([]Role, error) {
	var rows []roleRow
	if err := r.db.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID).Order("name ASC", "id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	roles := make([]Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, roleFromRow(row))
	}
	return roles, nil
}

func (r *Repository) GetRole(ctx context.Context, tenantID, roleID string) (Role, error) {
	row, err := getRoleRow(ctx, r.db, tenantID, roleID)
	if err != nil {
		return Role{}, err
	}
	return roleFromRow(row), nil
}

func (r *Repository) UpdateRole(ctx context.Context, tenantID, roleID, name, description string) (Role, error) {
	now := r.now().UTC().UnixMilli()
	result, err := r.db.NewUpdate().Model((*roleRow)(nil)).
		Set("name = ?", name).
		Set("description = ?", description).
		Set("updated_at = ?", now).
		Where("tenant_id = ? AND id = ?", tenantID, roleID).
		Exec(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return Role{}, ErrRoleNameConflict
		}
		return Role{}, fmt.Errorf("update role: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return Role{}, ErrRoleNotFound
	}
	return r.GetRole(ctx, tenantID, roleID)
}

func (r *Repository) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := ensureRole(ctx, tx, tenantID, roleID); err != nil {
			return err
		}
		var permissions []permissionRow
		if err := tx.NewSelect().Model(&permissions).Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Scan(ctx); err != nil {
			return fmt.Errorf("list role permissions for delete: %w", err)
		}
		var members []memberRow
		if err := tx.NewSelect().Model(&members).Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Scan(ctx); err != nil {
			return fmt.Errorf("list role members for delete: %w", err)
		}
		for _, permission := range permissions {
			if err := r.upsertOutbox(ctx, tx, tenantID, spicedbx.RelationshipDelete, permissionRelationship(tenantID, roleID, permission.PermissionCode)); err != nil {
				return err
			}
		}
		for _, member := range members {
			if err := r.upsertOutbox(ctx, tx, tenantID, spicedbx.RelationshipDelete, memberRelationship(roleID, member.UserSubject)); err != nil {
				return err
			}
		}
		if _, err := tx.NewDelete().Model((*permissionRow)(nil)).Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Exec(ctx); err != nil {
			return fmt.Errorf("delete role permissions: %w", err)
		}
		if _, err := tx.NewDelete().Model((*memberRow)(nil)).Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Exec(ctx); err != nil {
			return fmt.Errorf("delete role members: %w", err)
		}
		if _, err := tx.NewDelete().Model((*roleRow)(nil)).Where("tenant_id = ? AND id = ?", tenantID, roleID).Exec(ctx); err != nil {
			return fmt.Errorf("delete role: %w", err)
		}
		return nil
	})
}

func (r *Repository) GrantPermission(ctx context.Context, tenantID, roleID, code string) (bool, error) {
	return r.changePermission(ctx, tenantID, roleID, code, spicedbx.RelationshipTouch)
}

func (r *Repository) RevokePermission(ctx context.Context, tenantID, roleID, code string) (bool, error) {
	return r.changePermission(ctx, tenantID, roleID, code, spicedbx.RelationshipDelete)
}

func (r *Repository) changePermission(ctx context.Context, tenantID, roleID, code string, operation spicedbx.RelationshipOperation) (bool, error) {
	var changed bool
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := ensureRole(ctx, tx, tenantID, roleID); err != nil {
			return err
		}
		now := r.now().UTC().UnixMilli()
		if operation == spicedbx.RelationshipTouch {
			row := permissionRow{TenantID: tenantID, RoleID: roleID, PermissionCode: code, CreatedAt: now}
			result, err := tx.NewInsert().Model(&row).Ignore().Exec(ctx)
			if err != nil {
				return fmt.Errorf("insert role permission: %w", err)
			}
			if affected, _ := result.RowsAffected(); affected > 0 {
				changed = true
			}
		} else {
			result, err := tx.NewDelete().Model((*permissionRow)(nil)).Where("tenant_id = ? AND role_id = ? AND permission_code = ?", tenantID, roleID, code).Exec(ctx)
			if err != nil {
				return fmt.Errorf("delete role permission: %w", err)
			}
			if affected, _ := result.RowsAffected(); affected > 0 {
				changed = true
			}
		}
		return r.upsertOutbox(ctx, tx, tenantID, operation, permissionRelationship(tenantID, roleID, code))
	})
	return changed, err
}

func (r *Repository) ListPermissions(ctx context.Context, tenantID, roleID string) ([]string, error) {
	if err := ensureRole(ctx, r.db, tenantID, roleID); err != nil {
		return nil, err
	}
	codes := make([]string, 0)
	if err := r.db.NewSelect().Model((*permissionRow)(nil)).Column("permission_code").Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Order("permission_code ASC").Scan(ctx, &codes); err != nil {
		return nil, fmt.Errorf("list role permissions: %w", err)
	}
	return codes, nil
}

func (r *Repository) AddMember(ctx context.Context, tenantID, roleID, subject string) (bool, error) {
	return r.changeMember(ctx, tenantID, roleID, subject, spicedbx.RelationshipTouch)
}

func (r *Repository) RemoveMember(ctx context.Context, tenantID, roleID, subject string) (bool, error) {
	return r.changeMember(ctx, tenantID, roleID, subject, spicedbx.RelationshipDelete)
}

func (r *Repository) changeMember(ctx context.Context, tenantID, roleID, subject string, operation spicedbx.RelationshipOperation) (bool, error) {
	var changed bool
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := ensureRole(ctx, tx, tenantID, roleID); err != nil {
			return err
		}
		now := r.now().UTC().UnixMilli()
		if operation == spicedbx.RelationshipTouch {
			row := memberRow{TenantID: tenantID, RoleID: roleID, UserSubject: subject, CreatedAt: now}
			result, err := tx.NewInsert().Model(&row).Ignore().Exec(ctx)
			if err != nil {
				return fmt.Errorf("insert role member: %w", err)
			}
			if affected, _ := result.RowsAffected(); affected > 0 {
				changed = true
			}
		} else {
			result, err := tx.NewDelete().Model((*memberRow)(nil)).Where("tenant_id = ? AND role_id = ? AND user_subject = ?", tenantID, roleID, subject).Exec(ctx)
			if err != nil {
				return fmt.Errorf("delete role member: %w", err)
			}
			if affected, _ := result.RowsAffected(); affected > 0 {
				changed = true
			}
		}
		return r.upsertOutbox(ctx, tx, tenantID, operation, memberRelationship(roleID, subject))
	})
	return changed, err
}

func (r *Repository) ListMembers(ctx context.Context, tenantID, roleID string) ([]string, error) {
	if err := ensureRole(ctx, r.db, tenantID, roleID); err != nil {
		return nil, err
	}
	subjects := make([]string, 0)
	if err := r.db.NewSelect().Model((*memberRow)(nil)).Column("user_subject").Where("tenant_id = ? AND role_id = ?", tenantID, roleID).Order("user_subject ASC").Scan(ctx, &subjects); err != nil {
		return nil, fmt.Errorf("list role members: %w", err)
	}
	return subjects, nil
}

func (r *Repository) upsertOutbox(ctx context.Context, db bun.IDB, tenantID string, operation spicedbx.RelationshipOperation, rel spicedbx.Relationship) error {
	id, err := r.nextID()
	if err != nil {
		return fmt.Errorf("generate outbox id: %w", err)
	}
	now := r.now().UTC().UnixMilli()
	args := []any{id, tenantID, relationshipKey(tenantID, rel), rel.Resource.Type, rel.Resource.ID, rel.Relation, rel.Subject.Object.Type, rel.Subject.Object.ID, rel.Subject.Relation, operation, 1, OutboxPending, 0, now, "", 0, "", "", now, now, 0}
	query := outboxUpsertSQL(r.dialect)
	if query == "" {
		return fmt.Errorf("unsupported iam database dialect %q", r.dialect)
	}
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert authz outbox: %w", err)
	}
	return nil
}

func outboxUpsertSQL(dialect string) string {
	const insert = `INSERT INTO authz_outbox
 (id, tenant_id, relationship_key, resource_type, resource_id, resource_relation, subject_type, subject_id, subject_relation, operation, version, status, attempts, available_at, locked_by, locked_at, last_error, zed_token, created_at, updated_at, delivered_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if dialect == "mysql" {
		return insert + ` ON DUPLICATE KEY UPDATE
 operation = VALUES(operation), version = version + 1, updated_at = VALUES(updated_at),
 status = IF(status = 'processing', status, 'pending'),
 attempts = IF(status = 'processing', attempts, 0),
 available_at = IF(status = 'processing', available_at, VALUES(available_at)),
 locked_by = IF(status = 'processing', locked_by, ''),
 locked_at = IF(status = 'processing', locked_at, 0),
 last_error = IF(status = 'processing', last_error, ''),
 delivered_at = IF(status = 'processing', delivered_at, 0)`
	}
	if dialect == "sqlite" || dialect == "postgres" {
		return insert + ` ON CONFLICT (tenant_id, relationship_key) DO UPDATE SET
 operation = excluded.operation, version = authz_outbox.version + 1, updated_at = excluded.updated_at,
 status = CASE WHEN authz_outbox.status = 'processing' THEN authz_outbox.status ELSE 'pending' END,
 attempts = CASE WHEN authz_outbox.status = 'processing' THEN authz_outbox.attempts ELSE 0 END,
 available_at = CASE WHEN authz_outbox.status = 'processing' THEN authz_outbox.available_at ELSE excluded.available_at END,
 locked_by = CASE WHEN authz_outbox.status = 'processing' THEN authz_outbox.locked_by ELSE '' END,
 locked_at = CASE WHEN authz_outbox.status = 'processing' THEN authz_outbox.locked_at ELSE 0 END,
 last_error = CASE WHEN authz_outbox.status = 'processing' THEN authz_outbox.last_error ELSE '' END,
 delivered_at = CASE WHEN authz_outbox.status = 'processing' THEN authz_outbox.delivered_at ELSE 0 END`
	}
	return ""
}

func getRoleRow(ctx context.Context, db bun.IDB, tenantID, roleID string) (roleRow, error) {
	var row roleRow
	if err := db.NewSelect().Model(&row).Where("tenant_id = ? AND id = ?", tenantID, roleID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return roleRow{}, ErrRoleNotFound
		}
		return roleRow{}, fmt.Errorf("get role: %w", err)
	}
	return row, nil
}

func ensureRole(ctx context.Context, db bun.IDB, tenantID, roleID string) error {
	_, err := getRoleRow(ctx, db, tenantID, roleID)
	return err
}

func roleFromRow(row roleRow) Role {
	return Role{
		ID: row.ID, TenantID: row.TenantID, Name: row.Name, Description: row.Description,
		CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC(),
	}
}

func permissionRelationship(tenantID, roleID, code string) spicedbx.Relationship {
	return spicedbx.Relationship{
		Resource: spicedbx.ObjectRef{Type: "tenant", ID: tenantID}, Relation: code + "_role",
		Subject: spicedbx.SubjectRef{Object: spicedbx.ObjectRef{Type: "role", ID: roleID}, Relation: "member"},
	}
}

func memberRelationship(roleID, subject string) spicedbx.Relationship {
	return spicedbx.Relationship{
		Resource: spicedbx.ObjectRef{Type: "role", ID: roleID}, Relation: "member",
		Subject: spicedbx.SubjectRef{Object: spicedbx.ObjectRef{Type: "user", ID: subject}},
	}
}

func relationshipKey(tenantID string, rel spicedbx.Relationship) string {
	sum := sha256.Sum256([]byte(tenantID + "\x00" + rel.String()))
	return hex.EncodeToString(sum[:])
}

func isUniqueViolation(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate entry") || strings.Contains(message, "duplicate key")
}
