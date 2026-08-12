package organization

import (
	"context"
	"errors"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestPositionLifecycleAndMembership(t *testing.T) {
	db, service := newPositionService(t)
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "administrator", PrincipalID: testID("administrator")})
	addTenantMember(t, db, "tenant-a", "principal-a", "Alice", iam.MemberActive)
	addTenantMember(t, db, "tenant-a", "principal-disabled", "Disabled", iam.MemberDisabled)
	addTenantMember(t, db, "tenant-b", "principal-b", "Bob", iam.MemberActive)

	engineer := createPosition(ctx, t, service, testID("tenant-a"), CreatePosition{Code: " Engineer ", Name: "Engineer", SortOrder: 20})
	manager := createPosition(ctx, t, service, testID("tenant-a"), CreatePosition{Code: "manager", Name: "Manager", SortOrder: 10})
	other := createPosition(ctx, t, service, testID("tenant-b"), CreatePosition{Code: "engineer", Name: "Engineer"})
	assert.Equal(t, "engineer", engineer.Code)

	items, err := service.List(ctx, testID("tenant-a"))
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, []string{manager.ID.String(), engineer.ID.String()}, []string{items[0].ID.String(), items[1].ID.String()})
	_, err = service.Get(ctx, testID("tenant-a"), other.ID)
	assert.ErrorIs(t, err, ErrPositionNotFound)
	_, err = service.Create(ctx, testID("tenant-a"), CreatePosition{Code: "ENGINEER", Name: "Duplicate"})
	assert.ErrorIs(t, err, ErrPositionCodeConflict)

	revisionBeforeNoop, err := policyx.Current(ctx, db, testID("tenant-a"))
	require.NoError(t, err)
	engineer, err = service.Update(ctx, testID("tenant-a"), engineer.ID, UpdatePosition{Name: stringPointer(engineer.Name), Version: engineer.Version})
	require.NoError(t, err)
	assert.Equal(t, int64(1), engineer.Version)
	revisionAfterNoop, err := policyx.Current(ctx, db, testID("tenant-a"))
	require.NoError(t, err)
	assert.Equal(t, revisionBeforeNoop, revisionAfterNoop)

	newCode, newName, disabled, order := "platform-engineer", "Platform Engineer", StatusDisabled, 5
	engineer, err = service.Update(ctx, testID("tenant-a"), engineer.ID, UpdatePosition{Code: &newCode, Name: &newName, Status: &disabled, SortOrder: &order, Version: engineer.Version})
	require.NoError(t, err)
	assert.Equal(t, int64(2), engineer.Version)
	assert.Equal(t, "platform-engineer", engineer.Code)
	_, err = service.Update(ctx, testID("tenant-a"), engineer.ID, UpdatePosition{Name: &newName, Version: 1})
	assert.ErrorIs(t, err, ErrPositionVersionConflict)

	start := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	_, err = service.PutMember(ctx, testID("tenant-a"), engineer.ID, testID("principal-disabled"), PositionMemberWindow{})
	assert.ErrorIs(t, err, ErrPositionMemberInactive)
	_, err = service.PutMember(ctx, testID("tenant-a"), engineer.ID, testID("principal-b"), PositionMemberWindow{})
	assert.ErrorIs(t, err, ErrPositionMemberInactive)
	member, err := service.PutMember(ctx, testID("tenant-a"), engineer.ID, testID("principal-a"), PositionMemberWindow{StartsAt: &start, EndsAt: &end})
	require.NoError(t, err)
	assert.Equal(t, "Alice", member.DisplayName)
	assert.Equal(t, start, *member.StartsAt)

	revisionBeforeNoop, err = policyx.Current(ctx, db, testID("tenant-a"))
	require.NoError(t, err)
	memberAgain, err := service.PutMember(ctx, testID("tenant-a"), engineer.ID, testID("principal-a"), PositionMemberWindow{StartsAt: &start, EndsAt: &end})
	require.NoError(t, err)
	assert.Equal(t, member.UpdatedAt, memberAgain.UpdatedAt)
	revisionAfterNoop, err = policyx.Current(ctx, db, testID("tenant-a"))
	require.NoError(t, err)
	assert.Equal(t, revisionBeforeNoop, revisionAfterNoop)

	members, err := service.ListMembers(ctx, testID("tenant-a"), engineer.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, testID("principal-a"), members[0].PrincipalID)
	assert.ErrorIs(t, service.Delete(ctx, testID("tenant-a"), engineer.ID, engineer.Version), ErrPositionHasMembers)

	deleted, err := service.DeleteMember(ctx, testID("tenant-a"), engineer.ID, testID("missing"))
	require.NoError(t, err)
	assert.False(t, deleted)
	deleted, err = service.DeleteMember(ctx, testID("tenant-a"), engineer.ID, testID("principal-a"))
	require.NoError(t, err)
	assert.True(t, deleted)
	require.NoError(t, service.Delete(ctx, testID("tenant-a"), engineer.ID, engineer.Version))
	_, err = service.Get(ctx, testID("tenant-a"), engineer.ID)
	assert.ErrorIs(t, err, ErrPositionNotFound)

	events, total, err := auditmod.NewService(db, newTestIDGenerator()).List(ctx, auditmod.Filter{TenantID: testID("tenant-a"), Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, int64(6), total)
	require.Len(t, events, 6)
	assert.Equal(t, []string{"position_created", "position_created", "position_updated", "position_member_assigned", "position_member_removed", "position_deleted"}, positionEventTypes(events))
	integrity, err := auditmod.NewService(db, newTestIDGenerator()).Verify(ctx, testID("tenant-a"))
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
}

func TestPositionDeletionRejectsRoleBinding(t *testing.T) {
	db, service := newPositionService(t)
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "administrator", PrincipalID: testID("administrator")})
	position := createPosition(ctx, t, service, testID("tenant"), CreatePosition{Code: "operator", Name: "Operator"})
	now := time.Now().UTC().UnixMilli()
	_, err := db.ExecContext(ctx, "INSERT INTO iam_roles (tenant_id,id,name,description,created_at,updated_at) VALUES (?,?,?,?,?,?)", testID("tenant"), testID("role"), "Role", "", now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO iam_position_role_bindings (tenant_id,role_id,position_id,created_at) VALUES (?,?,?,?)", testID("tenant"), testID("role"), position.ID, now)
	require.NoError(t, err)
	assert.ErrorIs(t, service.Delete(ctx, testID("tenant"), position.ID, position.Version), ErrPositionRoleBound)
	_, err = db.ExecContext(ctx, "DELETE FROM iam_position_role_bindings WHERE tenant_id = ? AND role_id = ? AND position_id = ?", testID("tenant"), testID("role"), position.ID)
	require.NoError(t, err)
	assert.NoError(t, service.Delete(ctx, testID("tenant"), position.ID, position.Version))
}

func TestPositionDeletionRejectsRelationship(t *testing.T) {
	db, service := newPositionService(t)
	position := createPosition(t.Context(), t, service, testID("tenant"), CreatePosition{Code: "operator", Name: "Operator"})
	_, err := db.ExecContext(t.Context(), `INSERT INTO iam_entities
		(tenant_id,id,parent_id,type,name,status,metadata,created_at,updated_at)
		VALUES (?,?,NULL,?,?,?,'{}',?,?)`, testID("tenant"), testID("company"), "company", "Company", "active", int64(1), int64(1))
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_relationships
		(tenant_id,subject_type,subject_id,subject_relation,relation,resource_type,resource_id,created_at)
		VALUES (?,?,?,?,?,?,?,?)`, testID("tenant"), "position", position.ID, "member", "viewer", testID("company"), testID("company"), int64(1))
	require.NoError(t, err)

	assert.ErrorIs(t, service.Delete(t.Context(), testID("tenant"), position.ID, position.Version), ErrPositionRelationshipBound)
	_, err = db.ExecContext(t.Context(), "DELETE FROM iam_relationships WHERE tenant_id = ? AND subject_type = 'position' AND subject_id = ?", testID("tenant"), position.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_resource_relationships
		(tenant_id,entity_id,subject_type,subject_id,subject_relation,relation,resource_type,resource_id,created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, testID("tenant"), testID("company"), "position", position.ID, "member", "viewer", testID("store"), testID("store-1"), int64(1))
	require.NoError(t, err)
	assert.ErrorIs(t, service.Delete(t.Context(), testID("tenant"), position.ID, position.Version), ErrPositionRelationshipBound)
	_, err = db.ExecContext(t.Context(), "DELETE FROM iam_resource_relationships WHERE tenant_id = ? AND subject_type = 'position' AND subject_id = ?", testID("tenant"), position.ID)
	require.NoError(t, err)
	assert.NoError(t, service.Delete(t.Context(), testID("tenant"), position.ID, position.Version))
}

func TestPositionValidationAndTransactionalRollback(t *testing.T) {
	db, service := newPositionService(t)
	_, err := service.List(t.Context(), 0)
	assert.ErrorIs(t, err, ErrPositionInvalid)
	_, err = service.Get(t.Context(), testID("tenant"), 0)
	assert.ErrorIs(t, err, ErrPositionInvalid)
	_, err = service.Update(t.Context(), testID("tenant"), testID("position"), UpdatePosition{})
	assert.ErrorIs(t, err, ErrPositionInvalid)
	assert.ErrorIs(t, service.Delete(t.Context(), testID("tenant"), testID("position"), 0), ErrPositionInvalid)
	_, err = service.ListMembers(t.Context(), testID("tenant"), 0)
	assert.ErrorIs(t, err, ErrPositionInvalid)

	service.nextID = func() (guid.ID, error) { return 0, errors.New("entropy unavailable") }
	_, err = service.Create(t.Context(), testID("tenant"), CreatePosition{Code: "position", Name: "Position"})
	assert.ErrorContains(t, err, "entropy unavailable")
	service.nextID = func() (guid.ID, error) { return 0, nil }
	_, err = service.Create(t.Context(), testID("tenant"), CreatePosition{Code: "position", Name: "Position"})
	assert.ErrorIs(t, err, ErrPositionInvalid)

	service.nextID = func() (guid.ID, error) { return testID("position"), nil }
	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER reject_position_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'position_created' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)
	_, err = service.Create(t.Context(), testID("tenant"), CreatePosition{Code: "position", Name: "Position"})
	assert.Error(t, err)
	count, countErr := db.NewSelect().Model((*positionRow)(nil)).Count(t.Context())
	require.NoError(t, countErr)
	assert.Zero(t, count)
	revision, revisionErr := policyx.Current(t.Context(), db, testID("tenant"))
	require.NoError(t, revisionErr)
	assert.Zero(t, revision)

	assert.Panics(t, func() {
		NewPositionService(nil, realOrganizationAuditAppender(db), iam.NewMembershipChecker(db), iam.NewAdministratorGuard(), newTestIDGenerator())
	})
	assert.Panics(t, func() {
		NewPositionService(db, nil, iam.NewMembershipChecker(db), iam.NewAdministratorGuard(), newTestIDGenerator())
	})
	assert.Panics(t, func() {
		NewPositionService(db, realOrganizationAuditAppender(db), nil, iam.NewAdministratorGuard(), newTestIDGenerator())
	})
	assert.Panics(t, func() {
		NewPositionService(db, realOrganizationAuditAppender(db), iam.NewMembershipChecker(db), nil, newTestIDGenerator())
	})
	assert.Panics(t, func() {
		NewPositionService(db, realOrganizationAuditAppender(db), iam.NewMembershipChecker(db), iam.NewAdministratorGuard(), nil)
	})
}

func TestPositionMemberAuditFailureRollsBack(t *testing.T) {
	db, service := newPositionService(t)
	addTenantMember(t, db, "tenant", "principal", "Principal", iam.MemberActive)
	position := createPosition(t.Context(), t, service, testID("tenant"), CreatePosition{Code: "position", Name: "Position"})
	_, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_position_member_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'position_member_assigned' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)

	_, err = service.PutMember(t.Context(), testID("tenant"), position.ID, testID("principal"), PositionMemberWindow{})
	assert.Error(t, err)
	members, listErr := service.ListMembers(t.Context(), testID("tenant"), position.ID)
	require.NoError(t, listErr)
	assert.Empty(t, members)
	revision, revisionErr := policyx.Current(t.Context(), db, testID("tenant"))
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
	service := NewPositionService(db, realOrganizationAuditAppender(db), iam.NewMembershipChecker(db), iam.NewAdministratorGuard(), func() (guid.ID, error) {
		return guid.ID(sequence.Add(1)), nil
	})
	return db, service
}

func addTenantMember(t *testing.T, db *bun.DB, tenantID, principalID, displayName string, status iam.MemberStatus) {
	t.Helper()
	require.NoError(t, EnsureTenant(t.Context(), db, testID(tenantID)))
	repo := iam.NewRepository(db, newTestIDGenerator())
	_, err := repo.PutMember(t.Context(), iam.TenantMember{TenantID: testID(tenantID), PrincipalID: testID(principalID), DisplayName: displayName, Status: status})
	require.NoError(t, err)
}

func addLocalPrincipal(t *testing.T, db *bun.DB, principalID string) {
	t.Helper()
	now := time.Now().UTC().UnixMilli()
	_, err := db.ExecContext(t.Context(), `INSERT INTO iam_principals
		(id,login_name,email,display_name,status,created_at,updated_at,disabled_at)
		VALUES (?,?,?,?,'active',?,?,0)`, testID(principalID), principalID, principalID+"@example.test", principalID, now, now)
	require.NoError(t, err)
}

func bindDirectoryAdministrator(t *testing.T, db *bun.DB, tenantID, assigneeType string, assigneeID guid.ID) {
	t.Helper()
	now := time.Now().UTC().UnixMilli()
	roleID := testID(assigneeType + "-administrator")
	_, err := db.ExecContext(t.Context(), `INSERT INTO iam_roles (tenant_id,id,name,description,created_at,updated_at) VALUES (?,?,?,'',?,?)`, testID(tenantID), roleID, "Administrator", now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_role_permissions (tenant_id,role_id,permission_code,created_at) VALUES (?,?,'tenant_administer',?)`, testID(tenantID), roleID, now)
	require.NoError(t, err)
	switch assigneeType {
	case "group":
		_, err = db.ExecContext(t.Context(), `INSERT INTO iam_group_role_bindings (tenant_id,role_id,group_id,created_at) VALUES (?,?,?,?)`, testID(tenantID), roleID, assigneeID, now)
	case "position":
		_, err = db.ExecContext(t.Context(), `INSERT INTO iam_position_role_bindings (tenant_id,role_id,position_id,created_at) VALUES (?,?,?,?)`, testID(tenantID), roleID, assigneeID, now)
	default:
		t.Fatalf("unsupported directory assignee type %q", assigneeType)
	}
	require.NoError(t, err)
}

func createPosition(ctx context.Context, t *testing.T, service *PositionService, tenantID guid.ID, input CreatePosition) Position {
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
