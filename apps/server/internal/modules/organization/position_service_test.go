package organization

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestPositionLifecycleAndMembership(t *testing.T) {
	db, service := newPositionService(t)
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "administrator"})
	addTenantMember(t, db, "tenant-a", "principal-a", "Alice", iam.MemberActive)
	addTenantMember(t, db, "tenant-a", "principal-disabled", "Disabled", iam.MemberDisabled)
	addTenantMember(t, db, "tenant-b", "principal-b", "Bob", iam.MemberActive)

	engineer := createPosition(ctx, t, service, "tenant-a", CreatePosition{Code: " Engineer ", Name: "Engineer", SortOrder: 20})
	manager := createPosition(ctx, t, service, "tenant-a", CreatePosition{Code: "manager", Name: "Manager", SortOrder: 10})
	other := createPosition(ctx, t, service, "tenant-b", CreatePosition{Code: "engineer", Name: "Engineer"})
	assert.Equal(t, "engineer", engineer.Code)

	items, err := service.List(ctx, " tenant-a ")
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, []string{manager.ID, engineer.ID}, []string{items[0].ID, items[1].ID})
	_, err = service.Get(ctx, "tenant-a", other.ID)
	assert.ErrorIs(t, err, ErrPositionNotFound)
	_, err = service.Create(ctx, "tenant-a", CreatePosition{Code: "ENGINEER", Name: "Duplicate"})
	assert.ErrorIs(t, err, ErrPositionCodeConflict)

	revisionBeforeNoop, err := policyx.Current(ctx, db, "tenant-a")
	require.NoError(t, err)
	engineer, err = service.Update(ctx, "tenant-a", engineer.ID, UpdatePosition{Name: stringPointer(engineer.Name), Version: engineer.Version})
	require.NoError(t, err)
	assert.Equal(t, int64(1), engineer.Version)
	revisionAfterNoop, err := policyx.Current(ctx, db, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, revisionBeforeNoop, revisionAfterNoop)

	newCode, newName, disabled, order := "platform-engineer", "Platform Engineer", StatusDisabled, 5
	engineer, err = service.Update(ctx, "tenant-a", engineer.ID, UpdatePosition{Code: &newCode, Name: &newName, Status: &disabled, SortOrder: &order, Version: engineer.Version})
	require.NoError(t, err)
	assert.Equal(t, int64(2), engineer.Version)
	assert.Equal(t, "platform-engineer", engineer.Code)
	_, err = service.Update(ctx, "tenant-a", engineer.ID, UpdatePosition{Name: &newName, Version: 1})
	assert.ErrorIs(t, err, ErrPositionVersionConflict)

	start := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	_, err = service.PutMember(ctx, "tenant-a", engineer.ID, "principal-disabled", PositionMemberWindow{})
	assert.ErrorIs(t, err, ErrPositionMemberInactive)
	_, err = service.PutMember(ctx, "tenant-a", engineer.ID, "principal-b", PositionMemberWindow{})
	assert.ErrorIs(t, err, ErrPositionMemberInactive)
	member, err := service.PutMember(ctx, "tenant-a", engineer.ID, "principal-a", PositionMemberWindow{StartsAt: &start, EndsAt: &end})
	require.NoError(t, err)
	assert.Equal(t, "Alice", member.DisplayName)
	assert.Equal(t, start, *member.StartsAt)

	revisionBeforeNoop, err = policyx.Current(ctx, db, "tenant-a")
	require.NoError(t, err)
	memberAgain, err := service.PutMember(ctx, "tenant-a", engineer.ID, "principal-a", PositionMemberWindow{StartsAt: &start, EndsAt: &end})
	require.NoError(t, err)
	assert.Equal(t, member.UpdatedAt, memberAgain.UpdatedAt)
	revisionAfterNoop, err = policyx.Current(ctx, db, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, revisionBeforeNoop, revisionAfterNoop)

	members, err := service.ListMembers(ctx, "tenant-a", engineer.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, "principal-a", members[0].PrincipalID)
	assert.ErrorIs(t, service.Delete(ctx, "tenant-a", engineer.ID, engineer.Version), ErrPositionHasMembers)

	deleted, err := service.DeleteMember(ctx, "tenant-a", engineer.ID, "missing")
	require.NoError(t, err)
	assert.False(t, deleted)
	deleted, err = service.DeleteMember(ctx, "tenant-a", engineer.ID, "principal-a")
	require.NoError(t, err)
	assert.True(t, deleted)
	require.NoError(t, service.Delete(ctx, "tenant-a", engineer.ID, engineer.Version))
	_, err = service.Get(ctx, "tenant-a", engineer.ID)
	assert.ErrorIs(t, err, ErrPositionNotFound)

	events, total, err := auditmod.NewService(db).List(ctx, auditmod.Filter{TenantID: "tenant-a", Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, int64(6), total)
	require.Len(t, events, 6)
	assert.Equal(t, []string{"position_created", "position_created", "position_updated", "position_member_assigned", "position_member_removed", "position_deleted"}, positionEventTypes(events))
	integrity, err := auditmod.NewService(db).Verify(ctx, "tenant-a")
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
}

func TestPositionDeletionRejectsRoleBinding(t *testing.T) {
	db, service := newPositionService(t)
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "administrator"})
	position := createPosition(ctx, t, service, "tenant", CreatePosition{Code: "operator", Name: "Operator"})
	now := time.Now().UTC().UnixMilli()
	_, err := db.ExecContext(ctx, "INSERT INTO iam_roles (tenant_id,id,name,description,created_at,updated_at) VALUES (?,?,?,?,?,?)", "tenant", "role", "Role", "", now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO iam_position_role_bindings (tenant_id,role_id,position_id,created_at) VALUES (?,?,?,?)", "tenant", "role", position.ID, now)
	require.NoError(t, err)
	assert.ErrorIs(t, service.Delete(ctx, "tenant", position.ID, position.Version), ErrPositionRoleBound)
	_, err = db.ExecContext(ctx, "DELETE FROM iam_position_role_bindings WHERE tenant_id = ? AND role_id = ? AND position_id = ?", "tenant", "role", position.ID)
	require.NoError(t, err)
	assert.NoError(t, service.Delete(ctx, "tenant", position.ID, position.Version))
}

func TestPositionDeletionRejectsRelationship(t *testing.T) {
	db, service := newPositionService(t)
	position := createPosition(t.Context(), t, service, "tenant", CreatePosition{Code: "operator", Name: "Operator"})
	_, err := db.ExecContext(t.Context(), `INSERT INTO iam_entities
		(tenant_id,id,parent_id,type,name,status,metadata,created_at,updated_at)
		VALUES ('tenant','company',NULL,'company','Company','active','{}',1,1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_relationships
		(tenant_id,subject_type,subject_id,subject_relation,relation,resource_type,resource_id,created_at)
		VALUES ('tenant','position',?,'member','viewer','company','company',1)`, position.ID)
	require.NoError(t, err)

	assert.ErrorIs(t, service.Delete(t.Context(), "tenant", position.ID, position.Version), ErrPositionRelationshipBound)
	_, err = db.ExecContext(t.Context(), "DELETE FROM iam_relationships WHERE tenant_id = 'tenant' AND subject_type = 'position' AND subject_id = ?", position.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_resource_relationships
		(tenant_id,entity_id,subject_type,subject_id,subject_relation,relation,resource_type,resource_id,created_at)
		VALUES ('tenant','company','position',?,'member','viewer','store','store-1',1)`, position.ID)
	require.NoError(t, err)
	assert.ErrorIs(t, service.Delete(t.Context(), "tenant", position.ID, position.Version), ErrPositionRelationshipBound)
	_, err = db.ExecContext(t.Context(), "DELETE FROM iam_resource_relationships WHERE tenant_id = 'tenant' AND subject_type = 'position' AND subject_id = ?", position.ID)
	require.NoError(t, err)
	assert.NoError(t, service.Delete(t.Context(), "tenant", position.ID, position.Version))
}

func TestPositionValidationAndTransactionalRollback(t *testing.T) {
	db, service := newPositionService(t)
	_, err := service.List(t.Context(), "")
	assert.ErrorIs(t, err, ErrPositionInvalid)
	_, err = service.Get(t.Context(), "tenant", "")
	assert.ErrorIs(t, err, ErrPositionInvalid)
	_, err = service.Update(t.Context(), "tenant", "position", UpdatePosition{})
	assert.ErrorIs(t, err, ErrPositionInvalid)
	assert.ErrorIs(t, service.Delete(t.Context(), "tenant", "position", 0), ErrPositionInvalid)
	_, err = service.ListMembers(t.Context(), "tenant", "")
	assert.ErrorIs(t, err, ErrPositionInvalid)

	service.nextID = func() (string, error) { return "", errors.New("entropy unavailable") }
	_, err = service.Create(t.Context(), "tenant", CreatePosition{Code: "position", Name: "Position"})
	assert.ErrorContains(t, err, "entropy unavailable")
	service.nextID = func() (string, error) { return "", nil }
	_, err = service.Create(t.Context(), "tenant", CreatePosition{Code: "position", Name: "Position"})
	assert.ErrorIs(t, err, ErrPositionInvalid)

	service.nextID = func() (string, error) { return "position", nil }
	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER reject_position_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'position_created' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)
	_, err = service.Create(t.Context(), "tenant", CreatePosition{Code: "position", Name: "Position"})
	assert.Error(t, err)
	count, countErr := db.NewSelect().Model((*positionRow)(nil)).Count(t.Context())
	require.NoError(t, countErr)
	assert.Zero(t, count)
	revision, revisionErr := policyx.Current(t.Context(), db, "tenant")
	require.NoError(t, revisionErr)
	assert.Zero(t, revision)

	assert.Panics(t, func() {
		NewPositionService(nil, realOrganizationAuditAppender(db), iam.NewMembershipChecker(db), iam.NewAdministratorGuard(), func() (string, error) { return "id", nil })
	})
	assert.Panics(t, func() {
		NewPositionService(db, nil, iam.NewMembershipChecker(db), iam.NewAdministratorGuard(), func() (string, error) { return "id", nil })
	})
	assert.Panics(t, func() {
		NewPositionService(db, realOrganizationAuditAppender(db), nil, iam.NewAdministratorGuard(), func() (string, error) { return "id", nil })
	})
	assert.Panics(t, func() {
		NewPositionService(db, realOrganizationAuditAppender(db), iam.NewMembershipChecker(db), nil, func() (string, error) { return "id", nil })
	})
	assert.Panics(t, func() {
		NewPositionService(db, realOrganizationAuditAppender(db), iam.NewMembershipChecker(db), iam.NewAdministratorGuard(), nil)
	})
}

func TestPositionMemberAuditFailureRollsBack(t *testing.T) {
	db, service := newPositionService(t)
	addTenantMember(t, db, "tenant", "principal", "Principal", iam.MemberActive)
	position := createPosition(t.Context(), t, service, "tenant", CreatePosition{Code: "position", Name: "Position"})
	_, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_position_member_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'position_member_assigned' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)

	_, err = service.PutMember(t.Context(), "tenant", position.ID, "principal", PositionMemberWindow{})
	assert.Error(t, err)
	members, listErr := service.ListMembers(t.Context(), "tenant", position.ID)
	require.NoError(t, listErr)
	assert.Empty(t, members)
	revision, revisionErr := policyx.Current(t.Context(), db, "tenant")
	require.NoError(t, revisionErr)
	assert.Equal(t, int64(1), revision)
}

func newPositionService(t *testing.T) (*bun.DB, *PositionService) {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	var sequence atomic.Int64
	service := NewPositionService(db, realOrganizationAuditAppender(db), iam.NewMembershipChecker(db), iam.NewAdministratorGuard(), func() (string, error) {
		return fmt.Sprintf("position-%d", sequence.Add(1)), nil
	})
	return db, service
}

func addTenantMember(t *testing.T, db *bun.DB, tenantID, principalID, displayName string, status iam.MemberStatus) {
	t.Helper()
	require.NoError(t, EnsureTenant(t.Context(), db, tenantID))
	repo := iam.NewRepository(db, func() (string, error) { return "unused", nil })
	_, err := repo.PutMember(t.Context(), iam.TenantMember{TenantID: tenantID, Subject: principalID, DisplayName: displayName, Status: status})
	require.NoError(t, err)
}

func addLocalPrincipal(t *testing.T, db *bun.DB, principalID string) {
	t.Helper()
	now := time.Now().UTC().UnixMilli()
	_, err := db.ExecContext(t.Context(), `INSERT INTO iam_principals
		(id,login_name,email,display_name,status,created_at,updated_at,disabled_at)
		VALUES (?,?,?,?,'active',?,?,0)`, principalID, principalID, principalID+"@example.test", principalID, now, now)
	require.NoError(t, err)
}

func bindDirectoryAdministrator(t *testing.T, db *bun.DB, tenantID, assigneeType, assigneeID string) {
	t.Helper()
	now := time.Now().UTC().UnixMilli()
	roleID := assigneeType + "-administrator"
	_, err := db.ExecContext(t.Context(), `INSERT INTO iam_roles (tenant_id,id,name,description,created_at,updated_at) VALUES (?,?,?,'',?,?)`, tenantID, roleID, "Administrator", now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_role_permissions (tenant_id,role_id,permission_code,created_at) VALUES (?,?,'tenant_administer',?)`, tenantID, roleID, now)
	require.NoError(t, err)
	switch assigneeType {
	case "group":
		_, err = db.ExecContext(t.Context(), `INSERT INTO iam_group_role_bindings (tenant_id,role_id,group_id,created_at) VALUES (?,?,?,?)`, tenantID, roleID, assigneeID, now)
	case "position":
		_, err = db.ExecContext(t.Context(), `INSERT INTO iam_position_role_bindings (tenant_id,role_id,position_id,created_at) VALUES (?,?,?,?)`, tenantID, roleID, assigneeID, now)
	default:
		t.Fatalf("unsupported directory assignee type %q", assigneeType)
	}
	require.NoError(t, err)
}

func createPosition(ctx context.Context, t *testing.T, service *PositionService, tenantID string, input CreatePosition) Position {
	t.Helper()
	position, err := service.Create(ctx, tenantID, input)
	require.NoError(t, err)
	return position
}

func positionEventTypes(events []auditmod.Event) []string {
	sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
	result := make([]string, len(events))
	for index := range events {
		result[index] = events[index].EventType
	}
	return result
}
