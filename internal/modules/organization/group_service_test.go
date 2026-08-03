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

func TestGroupLifecycleAndMembership(t *testing.T) {
	db, service := newGroupService(t)
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "administrator"})
	addTenantMember(t, db, "tenant-a", "principal-a", "Alice", iam.MemberActive)
	addTenantMember(t, db, "tenant-a", "principal-disabled", "Disabled", iam.MemberDisabled)
	addTenantMember(t, db, "tenant-b", "principal-b", "Bob", iam.MemberActive)

	operators := createGroup(t, ctx, service, "tenant-a", CreateGroup{Name: " Operators ", Description: "Operators", SortOrder: 20})
	auditors := createGroup(t, ctx, service, "tenant-a", CreateGroup{Name: "Auditors", SortOrder: 10})
	other := createGroup(t, ctx, service, "tenant-b", CreateGroup{Name: "Operators"})
	assert.Equal(t, GroupTypeStatic, operators.Type)

	items, err := service.List(ctx, " tenant-a ")
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, []string{auditors.ID, operators.ID}, []string{items[0].ID, items[1].ID})
	_, err = service.Get(ctx, "tenant-a", other.ID)
	assert.ErrorIs(t, err, ErrGroupNotFound)
	_, err = service.Create(ctx, "tenant-a", CreateGroup{Name: "OPERATORS"})
	assert.ErrorIs(t, err, ErrGroupNameConflict)

	revisionBeforeNoop, err := policyx.Current(ctx, db, "tenant-a")
	require.NoError(t, err)
	operators, err = service.Update(ctx, "tenant-a", operators.ID, UpdateGroup{Name: stringPointer(operators.Name), Version: operators.Version})
	require.NoError(t, err)
	assert.Equal(t, int64(1), operators.Version)
	revisionAfterNoop, err := policyx.Current(ctx, db, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, revisionBeforeNoop, revisionAfterNoop)

	newName, description, disabled, order := "Security Operators", "Privileged operators", StatusDisabled, 5
	operators, err = service.Update(ctx, "tenant-a", operators.ID, UpdateGroup{Name: &newName, Description: &description, Status: &disabled, SortOrder: &order, Version: operators.Version})
	require.NoError(t, err)
	assert.Equal(t, int64(2), operators.Version)
	_, err = service.Update(ctx, "tenant-a", operators.ID, UpdateGroup{Description: &description, Version: 1})
	assert.ErrorIs(t, err, ErrGroupVersionConflict)

	start := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	_, err = service.PutMember(ctx, "tenant-a", operators.ID, "principal-disabled", GroupMemberWindow{})
	assert.ErrorIs(t, err, ErrGroupMemberInactive)
	_, err = service.PutMember(ctx, "tenant-a", operators.ID, "principal-b", GroupMemberWindow{})
	assert.ErrorIs(t, err, ErrGroupMemberInactive)
	member, err := service.PutMember(ctx, "tenant-a", operators.ID, "principal-a", GroupMemberWindow{StartsAt: &start, EndsAt: &end})
	require.NoError(t, err)
	assert.Equal(t, "Alice", member.DisplayName)
	assert.Equal(t, start, *member.StartsAt)

	revisionBeforeNoop, err = policyx.Current(ctx, db, "tenant-a")
	require.NoError(t, err)
	memberAgain, err := service.PutMember(ctx, "tenant-a", operators.ID, "principal-a", GroupMemberWindow{StartsAt: &start, EndsAt: &end})
	require.NoError(t, err)
	assert.Equal(t, member.UpdatedAt, memberAgain.UpdatedAt)
	revisionAfterNoop, err = policyx.Current(ctx, db, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, revisionBeforeNoop, revisionAfterNoop)

	members, err := service.ListMembers(ctx, "tenant-a", operators.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, "principal-a", members[0].PrincipalID)
	assert.ErrorIs(t, service.Delete(ctx, "tenant-a", operators.ID, operators.Version), ErrGroupHasMembers)

	deleted, err := service.DeleteMember(ctx, "tenant-a", operators.ID, "missing")
	require.NoError(t, err)
	assert.False(t, deleted)
	deleted, err = service.DeleteMember(ctx, "tenant-a", operators.ID, "principal-a")
	require.NoError(t, err)
	assert.True(t, deleted)
	require.NoError(t, service.Delete(ctx, "tenant-a", operators.ID, operators.Version))
	_, err = service.Get(ctx, "tenant-a", operators.ID)
	assert.ErrorIs(t, err, ErrGroupNotFound)

	events, total, err := auditmod.NewService(db).List(ctx, auditmod.Filter{TenantID: "tenant-a", Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, int64(6), total)
	assert.Equal(t, []string{"group_created", "group_created", "group_updated", "group_member_assigned", "group_member_removed", "group_deleted"}, groupEventTypes(events))
	integrity, err := auditmod.NewService(db).Verify(ctx, "tenant-a")
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
}

func TestGroupDeletionRejectsRoleBinding(t *testing.T) {
	db, service := newGroupService(t)
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "administrator"})
	group := createGroup(t, ctx, service, "tenant", CreateGroup{Name: "Operators"})
	now := time.Now().UTC().UnixMilli()
	_, err := db.ExecContext(ctx, "INSERT INTO iam_roles (tenant_id,id,name,description,created_at,updated_at) VALUES (?,?,?,?,?,?)", "tenant", "role", "Role", "", now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO iam_group_role_bindings (tenant_id,role_id,group_id,created_at) VALUES (?,?,?,?)", "tenant", "role", group.ID, now)
	require.NoError(t, err)
	assert.ErrorIs(t, service.Delete(ctx, "tenant", group.ID, group.Version), ErrGroupRoleBound)
	_, err = db.ExecContext(ctx, "DELETE FROM iam_group_role_bindings WHERE tenant_id = ? AND role_id = ? AND group_id = ?", "tenant", "role", group.ID)
	require.NoError(t, err)
	assert.NoError(t, service.Delete(ctx, "tenant", group.ID, group.Version))
}

func TestGroupDeletionRejectsRelationship(t *testing.T) {
	db, service := newGroupService(t)
	group := createGroup(t, t.Context(), service, "tenant", CreateGroup{Name: "Operators"})
	_, err := db.ExecContext(t.Context(), `INSERT INTO iam_entities
		(tenant_id,id,parent_id,type,name,status,metadata,created_at,updated_at)
		VALUES ('tenant','company',NULL,'company','Company','active','{}',1,1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_relationships
		(tenant_id,subject_type,subject_id,subject_relation,relation,resource_type,resource_id,created_at)
		VALUES ('tenant','group',?,'member','viewer','company','company',1)`, group.ID)
	require.NoError(t, err)

	assert.ErrorIs(t, service.Delete(t.Context(), "tenant", group.ID, group.Version), ErrGroupRelationshipBound)
	_, err = db.ExecContext(t.Context(), "DELETE FROM iam_relationships WHERE tenant_id = 'tenant' AND subject_type = 'group' AND subject_id = ?", group.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_resource_relationships
		(tenant_id,entity_id,subject_type,subject_id,subject_relation,relation,resource_type,resource_id,created_at)
		VALUES ('tenant','company','group',?,'member','viewer','store','store-1',1)`, group.ID)
	require.NoError(t, err)
	assert.ErrorIs(t, service.Delete(t.Context(), "tenant", group.ID, group.Version), ErrGroupRelationshipBound)
	_, err = db.ExecContext(t.Context(), "DELETE FROM iam_resource_relationships WHERE tenant_id = 'tenant' AND subject_type = 'group' AND subject_id = ?", group.ID)
	require.NoError(t, err)
	assert.NoError(t, service.Delete(t.Context(), "tenant", group.ID, group.Version))
}

func TestGroupValidationAndTransactionalRollback(t *testing.T) {
	db, service := newGroupService(t)
	_, err := service.List(t.Context(), "")
	assert.ErrorIs(t, err, ErrGroupInvalid)
	_, err = service.Get(t.Context(), "tenant", "")
	assert.ErrorIs(t, err, ErrGroupInvalid)
	_, err = service.Update(t.Context(), "tenant", "group", UpdateGroup{})
	assert.ErrorIs(t, err, ErrGroupInvalid)
	assert.ErrorIs(t, service.Delete(t.Context(), "tenant", "group", 0), ErrGroupInvalid)
	_, err = service.ListMembers(t.Context(), "tenant", "")
	assert.ErrorIs(t, err, ErrGroupInvalid)

	service.nextID = func() (string, error) { return "", errors.New("entropy unavailable") }
	_, err = service.Create(t.Context(), "tenant", CreateGroup{Name: "Group"})
	assert.ErrorContains(t, err, "entropy unavailable")
	service.nextID = func() (string, error) { return "", nil }
	_, err = service.Create(t.Context(), "tenant", CreateGroup{Name: "Group"})
	assert.ErrorIs(t, err, ErrGroupInvalid)

	service.nextID = func() (string, error) { return "group", nil }
	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER reject_group_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'group_created' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)
	_, err = service.Create(t.Context(), "tenant", CreateGroup{Name: "Group"})
	assert.Error(t, err)
	count, countErr := db.NewSelect().Model((*groupRow)(nil)).Count(t.Context())
	require.NoError(t, countErr)
	assert.Zero(t, count)
	revision, revisionErr := policyx.Current(t.Context(), db, "tenant")
	require.NoError(t, revisionErr)
	assert.Zero(t, revision)

	assert.Panics(t, func() {
		NewGroupService(nil, realOrganizationAuditAppender(db), iam.NewMembershipChecker(db), iam.NewAdministratorGuard(), func() (string, error) { return "id", nil })
	})
	assert.Panics(t, func() {
		NewGroupService(db, nil, iam.NewMembershipChecker(db), iam.NewAdministratorGuard(), func() (string, error) { return "id", nil })
	})
	assert.Panics(t, func() {
		NewGroupService(db, realOrganizationAuditAppender(db), nil, iam.NewAdministratorGuard(), func() (string, error) { return "id", nil })
	})
	assert.Panics(t, func() {
		NewGroupService(db, realOrganizationAuditAppender(db), iam.NewMembershipChecker(db), nil, func() (string, error) { return "id", nil })
	})
	assert.Panics(t, func() {
		NewGroupService(db, realOrganizationAuditAppender(db), iam.NewMembershipChecker(db), iam.NewAdministratorGuard(), nil)
	})
}

func TestGroupMemberAuditFailureRollsBack(t *testing.T) {
	db, service := newGroupService(t)
	addTenantMember(t, db, "tenant", "principal", "Principal", iam.MemberActive)
	group := createGroup(t, t.Context(), service, "tenant", CreateGroup{Name: "Group"})
	_, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_group_member_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'group_member_assigned' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)

	_, err = service.PutMember(t.Context(), "tenant", group.ID, "principal", GroupMemberWindow{})
	assert.Error(t, err)
	members, listErr := service.ListMembers(t.Context(), "tenant", group.ID)
	require.NoError(t, listErr)
	assert.Empty(t, members)
	revision, revisionErr := policyx.Current(t.Context(), db, "tenant")
	require.NoError(t, revisionErr)
	assert.Equal(t, int64(1), revision)
}

func TestDynamicGroupMembersAreComputedFromCurrentAttributes(t *testing.T) {
	db, service := newGroupService(t)
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "administrator"})
	addTenantMember(t, db, "tenant", "alice", "Alice", iam.MemberActive)
	addTenantMember(t, db, "tenant", "bob", "Bob", iam.MemberActive)
	_, err := db.ExecContext(ctx, "UPDATE iam_tenant_members SET email = CASE user_subject WHEN 'alice' THEN 'alice@example.com' ELSE 'bob@example.net' END WHERE tenant_id = 'tenant'")
	require.NoError(t, err)
	rule := MembershipRule(`{"version":1,"match":"all","conditions":[{"field":"member.email_domain","operator":"in","values":["example.com"]}]}`)
	group := createGroup(t, ctx, service, "tenant", CreateGroup{Name: "Example users", Type: GroupTypeDynamic, Rule: rule})
	assert.Equal(t, GroupTypeDynamic, group.Type)
	assert.JSONEq(t, string(rule), string(group.Rule))

	members, err := service.ListMembers(ctx, "tenant", group.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, "alice", members[0].PrincipalID)
	_, err = service.PutMember(ctx, "tenant", group.ID, "bob", GroupMemberWindow{})
	assert.ErrorIs(t, err, ErrDynamicGroupMembers)
	_, err = service.DeleteMember(ctx, "tenant", group.ID, "alice")
	assert.ErrorIs(t, err, ErrDynamicGroupMembers)

	updatedRule := MembershipRule(`{"version":1,"match":"any","conditions":[{"field":"member.subject","operator":"in","values":["bob"]}]}`)
	group, err = service.Update(ctx, "tenant", group.ID, UpdateGroup{Rule: &updatedRule, Version: group.Version})
	require.NoError(t, err)
	assert.Equal(t, int64(2), group.Version)
	members, err = service.ListMembers(ctx, "tenant", group.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, "bob", members[0].PrincipalID)

	_, err = db.ExecContext(ctx, "UPDATE iam_groups SET rule_json = '{broken' WHERE tenant_id = ? AND id = ?", "tenant", group.ID)
	require.NoError(t, err)
	_, err = service.ListMembers(ctx, "tenant", group.ID)
	assert.ErrorContains(t, err, "persisted dynamic group rule")
}

func newGroupService(t *testing.T) (*bun.DB, *GroupService) {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	var sequence atomic.Int64
	service := NewGroupService(db, realOrganizationAuditAppender(db), iam.NewMembershipChecker(db), iam.NewAdministratorGuard(), func() (string, error) {
		return fmt.Sprintf("group-%d", sequence.Add(1)), nil
	})
	return db, service
}

func createGroup(t *testing.T, ctx context.Context, service *GroupService, tenantID string, input CreateGroup) Group {
	t.Helper()
	group, err := service.Create(ctx, tenantID, input)
	require.NoError(t, err)
	return group
}

func groupEventTypes(events []auditmod.Event) []string {
	sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
	result := make([]string, len(events))
	for index := range events {
		result[index] = events[index].EventType
	}
	return result
}
