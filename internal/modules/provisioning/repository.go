package provisioning

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/uptrace/bun"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
)

type directoryRow struct {
	bun.BaseModel `bun:"table:iam_scim_directories,alias:directory"`
	ID            string `bun:"id,pk"`
	TenantID      string
	Name          string
	NameKey       string
	Status        string
	Version       int64
	CreatedAt     int64
	UpdatedAt     int64
}

type credentialRow struct {
	bun.BaseModel `bun:"table:iam_scim_credentials,alias:credential"`
	ID            string `bun:"id,pk"`
	DirectoryID   string
	Name          string
	TokenHash     string
	ExpiresAt     int64
	LastUsedAt    int64
	RevokedAt     int64
	CreatedAt     int64
}

type credentialAuthRow struct {
	credentialRow   `bun:",embed"`
	TenantID        string `bun:"tenant_id"`
	DirectoryStatus string `bun:"directory_status"`
}

type resourceRow struct {
	bun.BaseModel `bun:"table:iam_scim_resources,alias:resource"`
	DirectoryID   string `bun:"directory_id,pk"`
	ResourceType  string `bun:"resource_type,pk"`
	ResourceID    string `bun:"resource_id,pk"`
	ExternalID    string
	ExternalKey   string
	Version       int64
	CreatedAt     int64
	UpdatedAt     int64
	DeletedAt     int64
}

type targetRow struct {
	bun.BaseModel         `bun:"table:iam_scim_targets,alias:target"`
	ID                    string `bun:"id,pk"`
	TenantID              string
	Name                  string
	NameKey               string
	BaseURL               string
	BearerTokenCiphertext string
	Status                string
	Version               int64
	CreatedAt             int64
	UpdatedAt             int64
}

type targetResourceRow struct {
	bun.BaseModel `bun:"table:iam_scim_target_resources,alias:target_resource"`
	TargetID      string `bun:"target_id,pk"`
	ResourceType  string `bun:"resource_type,pk"`
	ResourceID    string `bun:"resource_id,pk"`
	ExternalID    string
	Version       int64
	CreatedAt     int64
	UpdatedAt     int64
	DeletedAt     int64
}

type userRecord struct {
	resourceRow      `bun:",embed"`
	LoginName        string `bun:"login_name"`
	Email            string `bun:"email"`
	DisplayName      string `bun:"display_name"`
	PrincipalStatus  string `bun:"principal_status"`
	MembershipStatus string `bun:"membership_status"`
}

type groupRecord struct {
	resourceRow `bun:",embed"`
	Name        string `bun:"name"`
	Status      string `bun:"status"`
}

type Repository struct {
	db       *bun.DB
	executor bun.IDB
}

func NewRepository(db *bun.DB) *Repository {
	if db == nil {
		panic("provisioning repository requires database")
	}
	return &Repository{db: db, executor: db}
}

func (r *Repository) withExecutor(db bun.IDB) *Repository { return &Repository{db: r.db, executor: db} }

func (r *Repository) listTargets(ctx context.Context, tenantID string) ([]targetRow, error) {
	rows := make([]targetRow, 0)
	if err := r.executor.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID).Order("name_key ASC", "id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list SCIM targets: %w", err)
	}
	return rows, nil
}

func (r *Repository) getTarget(ctx context.Context, tenantID, id string) (targetRow, error) {
	var row targetRow
	if err := r.executor.NewSelect().Model(&row).Where("tenant_id = ? AND id = ?", tenantID, id).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, ErrTargetMissing
		}
		return row, fmt.Errorf("get SCIM target: %w", err)
	}
	return row, nil
}

func (r *Repository) insertTarget(ctx context.Context, row *targetRow) error {
	if _, err := r.executor.NewInsert().Model(row).Exec(ctx); err != nil {
		if bunx.IsUniqueViolation(err) {
			return ErrTargetName
		}
		return fmt.Errorf("insert SCIM target: %w", err)
	}
	return nil
}

func (r *Repository) replaceTarget(ctx context.Context, row *targetRow, expectedVersion int64) error {
	result, err := r.executor.NewUpdate().Model(row).Where("id = ? AND version = ?", row.ID, expectedVersion).Exec(ctx)
	if err != nil {
		if bunx.IsUniqueViolation(err) {
			return ErrTargetName
		}
		return fmt.Errorf("replace SCIM target: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("replace SCIM target: %w", err)
	}
	if affected == 0 {
		return ErrTargetVersion
	}
	return nil
}

func (r *Repository) deleteTarget(ctx context.Context, id string) error {
	if _, err := r.executor.NewDelete().Table("iam_scim_targets").Where("id = ?", id).Exec(ctx); err != nil {
		return fmt.Errorf("delete SCIM target: %w", err)
	}
	return nil
}

// getTargetResource returns sql.ErrNoRows when no mapping exists yet; a
// missing mapping is the normal first-push condition, not an error.
func (r *Repository) listTargetResources(ctx context.Context, targetID string) ([]targetResourceRow, error) {
	var rows []targetResourceRow
	if err := r.executor.NewSelect().Model(&rows).Where("target_id = ?", targetID).Order("resource_type ASC", "resource_id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list SCIM target resources: %w", err)
	}
	return rows, nil
}

func (r *Repository) getTargetResource(ctx context.Context, targetID, resourceType, resourceID string) (targetResourceRow, error) {
	var row targetResourceRow
	if err := r.executor.NewSelect().Model(&row).Where("target_id = ? AND resource_type = ? AND resource_id = ?", targetID, resourceType, resourceID).Scan(ctx); err != nil {
		return row, err
	}
	return row, nil
}

func (r *Repository) upsertTargetResource(ctx context.Context, row *targetResourceRow) error {
	existing, err := r.getTargetResource(ctx, row.TargetID, row.ResourceType, row.ResourceID)
	switch {
	case err == nil:
		existing.ExternalID = row.ExternalID
		existing.DeletedAt = 0
		existing.Version++
		existing.UpdatedAt = row.UpdatedAt
		if _, err := r.executor.NewUpdate().Model(&existing).Where("target_id = ? AND resource_type = ? AND resource_id = ?", existing.TargetID, existing.ResourceType, existing.ResourceID).Exec(ctx); err != nil {
			return fmt.Errorf("update SCIM target resource: %w", err)
		}
		*row = existing
	case errors.Is(err, sql.ErrNoRows):
		if _, err := r.executor.NewInsert().Model(row).Exec(ctx); err != nil {
			return fmt.Errorf("insert SCIM target resource: %w", err)
		}
	default:
		return err
	}
	return nil
}

func (r *Repository) markTargetResourceDeleted(ctx context.Context, targetID, resourceType, resourceID string, at int64) error {
	if _, err := r.executor.NewUpdate().Table("iam_scim_target_resources").Set("deleted_at = ?", at).Set("updated_at = ?", at).Where("target_id = ? AND resource_type = ? AND resource_id = ?", targetID, resourceType, resourceID).Exec(ctx); err != nil {
		return fmt.Errorf("mark SCIM target resource deleted: %w", err)
	}
	return nil
}

func (r *Repository) listDirectories(ctx context.Context, tenantID string) ([]directoryRow, error) {
	rows := make([]directoryRow, 0)
	if err := r.executor.NewSelect().Model(&rows).Where("tenant_id = ?", tenantID).Order("name_key ASC", "id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list SCIM directories: %w", err)
	}
	return rows, nil
}

func (r *Repository) getDirectory(ctx context.Context, tenantID, id string) (directoryRow, error) {
	var row directoryRow
	if err := r.executor.NewSelect().Model(&row).Where("tenant_id = ? AND id = ?", tenantID, id).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, ErrDirectoryMissing
		}
		return row, fmt.Errorf("get SCIM directory: %w", err)
	}
	return row, nil
}

func (r *Repository) insertDirectory(ctx context.Context, row *directoryRow) error {
	if _, err := r.executor.NewInsert().Model(row).Exec(ctx); err != nil {
		if isDirectoryNameViolation(err) {
			return ErrDirectoryName
		}
		return fmt.Errorf("insert SCIM directory: %w", err)
	}
	return nil
}

func (r *Repository) updateDirectory(ctx context.Context, row *directoryRow, version int64) error {
	result, err := r.executor.NewUpdate().Model(row).Column("name", "name_key", "status", "version", "updated_at").
		Where("tenant_id = ? AND id = ? AND version = ?", row.TenantID, row.ID, version).Exec(ctx)
	if err != nil {
		if isDirectoryNameViolation(err) {
			return ErrDirectoryName
		}
		return fmt.Errorf("update SCIM directory: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrDirectoryVersion
	}
	return nil
}

func (r *Repository) listCredentials(ctx context.Context, directoryID string) ([]credentialRow, error) {
	rows := make([]credentialRow, 0)
	if err := r.executor.NewSelect().Model(&rows).Where("directory_id = ?", directoryID).Order("created_at DESC", "id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list SCIM credentials: %w", err)
	}
	return rows, nil
}

func (r *Repository) activeCredentialCount(ctx context.Context, directoryID string, now int64) (int, error) {
	return r.executor.NewSelect().Model((*credentialRow)(nil)).Where("directory_id = ? AND revoked_at = 0 AND (expires_at = 0 OR expires_at > ?)", directoryID, now).Count(ctx)
}

func (r *Repository) insertCredential(ctx context.Context, row *credentialRow) error {
	if _, err := r.executor.NewInsert().Model(row).Exec(ctx); err != nil {
		return fmt.Errorf("insert SCIM credential: %w", err)
	}
	return nil
}

func (r *Repository) revokeCredential(ctx context.Context, directoryID, id string, now int64) (bool, error) {
	result, err := r.executor.NewUpdate().Model((*credentialRow)(nil)).Set("revoked_at = ?", now).
		Where("directory_id = ? AND id = ? AND revoked_at = 0", directoryID, id).Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("revoke SCIM credential: %w", err)
	}
	count, _ := result.RowsAffected()
	if count == 1 {
		return true, nil
	}
	exists, err := r.executor.NewSelect().Model((*credentialRow)(nil)).Where("directory_id = ? AND id = ?", directoryID, id).Exists(ctx)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, ErrCredentialMissing
	}
	return false, nil
}

func (r *Repository) credentialForAuth(ctx context.Context, id string) (credentialAuthRow, error) {
	var row credentialAuthRow
	err := r.executor.NewSelect().TableExpr("iam_scim_credentials AS credential").
		ColumnExpr("credential.*").ColumnExpr("directory.tenant_id AS tenant_id, directory.status AS directory_status").
		Join("JOIN iam_scim_directories AS directory ON directory.id = credential.directory_id").Where("credential.id = ?", id).Scan(ctx, &row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, ErrUnauthorized
		}
		return row, fmt.Errorf("read SCIM credential: %w", err)
	}
	return row, nil
}

func (r *Repository) touchCredential(ctx context.Context, id string, now int64) error {
	_, err := r.executor.NewUpdate().Model((*credentialRow)(nil)).Set("last_used_at = ?", now).
		Where("id = ? AND revoked_at = 0 AND last_used_at < ?", id, now-300000).Exec(ctx)
	return err
}

func (r *Repository) getResource(ctx context.Context, directoryID, resourceType, resourceID string, includeDeleted bool) (resourceRow, error) {
	var row resourceRow
	query := r.executor.NewSelect().Model(&row).Where("directory_id = ? AND resource_type = ? AND resource_id = ?", directoryID, resourceType, resourceID)
	if !includeDeleted {
		query = query.Where("deleted_at = 0")
	}
	if err := query.Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, ErrResourceMissing
		}
		return row, err
	}
	return row, nil
}

func (r *Repository) getResourceByExternalKey(ctx context.Context, directoryID, resourceType, key string) (resourceRow, error) {
	var row resourceRow
	if err := r.executor.NewSelect().Model(&row).Where("directory_id = ? AND resource_type = ? AND external_key = ?", directoryID, resourceType, key).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, ErrResourceMissing
		}
		return row, err
	}
	return row, nil
}

func (r *Repository) insertResource(ctx context.Context, row *resourceRow) error {
	if _, err := r.executor.NewInsert().Model(row).Exec(ctx); err != nil {
		if bunx.IsUniqueViolation(err) {
			return ErrResourceConflict
		}
		return fmt.Errorf("insert SCIM resource mapping: %w", err)
	}
	return nil
}

func (r *Repository) updateResource(ctx context.Context, row *resourceRow, expectedVersion int64) error {
	result, err := r.executor.NewUpdate().Model(row).Column("external_id", "external_key", "version", "updated_at", "deleted_at").
		Where("directory_id = ? AND resource_type = ? AND resource_id = ? AND version = ?", row.DirectoryID, row.ResourceType, row.ResourceID, expectedVersion).Exec(ctx)
	if err != nil {
		if bunx.IsUniqueViolation(err) {
			return ErrResourceConflict
		}
		return fmt.Errorf("update SCIM resource mapping: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrResourceVersion
	}
	return nil
}

func (r *Repository) user(ctx context.Context, directoryID, resourceID string) (userRecord, error) {
	var row userRecord
	err := r.userQuery(directoryID, false).Where("resource.resource_id = ?", resourceID).Scan(ctx, &row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, ErrResourceMissing
		}
		return row, fmt.Errorf("get SCIM user: %w", err)
	}
	return row, nil
}

func (r *Repository) userPage(ctx context.Context, directoryID string, request ListRequest, dialect string) ([]userRecord, int, error) {
	query := r.userQuery(directoryID, false)
	if err := applySCIMFilter(query, request.Filter, ResourceUser, dialect); err != nil {
		return nil, 0, err
	}
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count SCIM users: %w", err)
	}
	if request.Count == 0 {
		return []userRecord{}, total, nil
	}
	query = r.userQuery(directoryID, false)
	if err := applySCIMFilter(query, request.Filter, ResourceUser, dialect); err != nil {
		return nil, 0, err
	}
	rows := make([]userRecord, 0, request.Count)
	if err := query.Order("principal.login_name ASC", "resource.resource_id ASC").Offset(request.StartIndex-1).Limit(request.Count).Scan(ctx, &rows); err != nil {
		return nil, 0, fmt.Errorf("list SCIM users: %w", err)
	}
	return rows, total, nil
}

func (r *Repository) userQuery(directoryID string, includeDeleted bool) *bun.SelectQuery {
	query := r.executor.NewSelect().TableExpr("iam_scim_resources AS resource").ColumnExpr("resource.*").
		ColumnExpr("principal.login_name, principal.email, principal.display_name, principal.status AS principal_status").
		ColumnExpr("member.status AS membership_status").
		Join("JOIN iam_scim_directories AS directory ON directory.id = resource.directory_id").
		Join("JOIN iam_principals AS principal ON principal.id = resource.resource_id").
		Join("JOIN iam_tenant_members AS member ON member.tenant_id = directory.tenant_id AND member.user_subject = resource.resource_id").
		Where("resource.directory_id = ? AND resource.resource_type = ?", directoryID, ResourceUser)
	if !includeDeleted {
		query = query.Where("resource.deleted_at = 0")
	}
	return query
}

func (r *Repository) group(ctx context.Context, directoryID, resourceID string) (groupRecord, error) {
	var row groupRecord
	err := r.groupQuery(directoryID, false).Where("resource.resource_id = ?", resourceID).Scan(ctx, &row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return row, ErrResourceMissing
		}
		return row, fmt.Errorf("get SCIM group: %w", err)
	}
	return row, nil
}

func (r *Repository) groupPage(ctx context.Context, directoryID string, request ListRequest, dialect string) ([]groupRecord, int, error) {
	query := r.groupQuery(directoryID, false)
	if err := applySCIMFilter(query, request.Filter, ResourceGroup, dialect); err != nil {
		return nil, 0, err
	}
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count SCIM groups: %w", err)
	}
	if request.Count == 0 {
		return []groupRecord{}, total, nil
	}
	query = r.groupQuery(directoryID, false)
	if err := applySCIMFilter(query, request.Filter, ResourceGroup, dialect); err != nil {
		return nil, 0, err
	}
	rows := make([]groupRecord, 0, request.Count)
	if err := query.Order("scim_group.name_key ASC", "resource.resource_id ASC").Offset(request.StartIndex-1).Limit(request.Count).Scan(ctx, &rows); err != nil {
		return nil, 0, fmt.Errorf("list SCIM groups: %w", err)
	}
	return rows, total, nil
}

func (r *Repository) groupQuery(directoryID string, includeDeleted bool) *bun.SelectQuery {
	query := r.executor.NewSelect().TableExpr("iam_scim_resources AS resource").ColumnExpr("resource.*").
		ColumnExpr("scim_group.name, scim_group.status").
		Join("JOIN iam_scim_directories AS directory ON directory.id = resource.directory_id").
		Join("JOIN iam_groups AS scim_group ON scim_group.tenant_id = directory.tenant_id AND scim_group.id = resource.resource_id").
		Where("resource.directory_id = ? AND resource.resource_type = ?", directoryID, ResourceGroup)
	if !includeDeleted {
		query = query.Where("resource.deleted_at = 0")
	}
	return query
}

func (r *Repository) groupMembers(ctx context.Context, directoryID, groupID string) ([]string, error) {
	ids := make([]string, 0)
	err := r.executor.NewSelect().TableExpr("iam_group_members AS member").ColumnExpr("member.principal_id").
		Join("JOIN iam_scim_directories AS directory ON directory.tenant_id = member.tenant_id").
		Join("JOIN iam_scim_resources AS resource ON resource.directory_id = directory.id AND resource.resource_type = ? AND resource.resource_id = member.principal_id AND resource.deleted_at = 0", ResourceUser).
		Where("directory.id = ? AND member.group_id = ?", directoryID, groupID).Order("member.principal_id ASC").Scan(ctx, &ids)
	if err != nil {
		return nil, fmt.Errorf("list SCIM group members: %w", err)
	}
	return ids, nil
}

func externalKey(externalID, resourceID string) string {
	if externalID == "" {
		return "id:" + resourceID
	}
	digest := sha256.Sum256([]byte(externalID))
	return "ext:" + hex.EncodeToString(digest[:])
}


func isDirectoryNameViolation(err error) bool {
	message := strings.ToLower(err.Error())
	return bunx.IsUniqueViolation(err) && (strings.Contains(message, "scim_directories_name") || strings.Contains(message, "name_key"))
}
