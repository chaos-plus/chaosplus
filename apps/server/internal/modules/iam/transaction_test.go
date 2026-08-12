package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestIAMWriteTransactionAuditAndRevision(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	makeTenantAdministrator(t, repo, testID("tenant"), testID("administrator"))
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "administrator", PrincipalID: testID("administrator")})

	role, err := service.CreateRole(ctx, testID("tenant"), "Operators", "")
	require.NoError(t, err)
	changed, err := service.GrantPermission(ctx, testID("tenant"), role.ID, "user_view")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = service.GrantPermission(ctx, testID("tenant"), role.ID, "user_view")
	require.NoError(t, err)
	assert.False(t, changed)
	condition := json.RawMessage(`{"version":1,"gte":[{"context":"auth.acr"},{"value":2}]}`)
	_, changed, err = service.SetPermissionCondition(ctx, testID("tenant"), role.ID, "user_view", condition)
	require.NoError(t, err)
	assert.True(t, changed)
	_, changed, err = service.SetPermissionCondition(ctx, testID("tenant"), role.ID, "user_view", condition)
	require.NoError(t, err)
	assert.False(t, changed)
	_, err = service.PutTenantMember(ctx, testID("tenant"), testID("member"), "Member", "member@example.test", 0, MemberActive)
	require.NoError(t, err)

	revision, err := repo.policyRevision(ctx, testID("tenant"))
	require.NoError(t, err)
	assert.Equal(t, int64(4), revision, "idempotent permission writes must not advance policy revision")

	auditService := auditmod.NewService(repo.db, newTestIDGenerator())
	events, total, err := auditService.List(ctx, auditmod.Filter{TenantID: testID("tenant"), Offset: 0, Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	require.Len(t, events, 5)
	for _, event := range events {
		assert.Equal(t, testID("administrator"), event.PrincipalID)
		assert.NotContains(t, string(event.Detail), "member@example.test")
	}
	integrity, err := auditService.Verify(ctx, testID("tenant"))
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
	assert.Equal(t, int64(5), integrity.VerifiedEvents)
}

func TestIAMWriteTransactionRollsBackEveryManagementMutationWhenAuditFails(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		prepare   func(*testing.T, *Repository) guid.ID
		mutate    func(context.Context, *Service, guid.ID) error
		assert    func(*testing.T, *Repository, guid.ID)
	}{
		{
			name: "create role", eventType: "role_created",
			mutate: func(ctx context.Context, service *Service, _ guid.ID) error {
				_, err := service.CreateRole(ctx, testID("tenant"), "Operators", "")
				return err
			},
			assert: func(t *testing.T, repo *Repository, _ guid.ID) {
				roles, err := repo.ListRoles(t.Context(), testID("tenant"))
				require.NoError(t, err)
				assert.Empty(t, roles)
			},
		},
		{
			name: "update role", eventType: "role_updated", prepare: prepareTransactionalRole,
			mutate: func(ctx context.Context, service *Service, id guid.ID) error {
				name := "Changed"
				_, err := service.UpdateRole(ctx, testID("tenant"), id, &name, nil)
				return err
			},
			assert: func(t *testing.T, repo *Repository, id guid.ID) {
				role, err := repo.GetRole(t.Context(), testID("tenant"), id)
				require.NoError(t, err)
				assert.Equal(t, "Original", role.Name)
			},
		},
		{
			name: "delete role", eventType: "role_deleted", prepare: prepareTransactionalRole,
			mutate: func(ctx context.Context, service *Service, id guid.ID) error {
				return service.DeleteRole(ctx, testID("tenant"), id)
			},
			assert: func(t *testing.T, repo *Repository, id guid.ID) {
				_, err := repo.GetRole(t.Context(), testID("tenant"), id)
				require.NoError(t, err)
			},
		},
		{
			name: "grant permission", eventType: "role_permission_granted", prepare: prepareTransactionalRole,
			mutate: func(ctx context.Context, service *Service, id guid.ID) error {
				_, err := service.GrantPermission(ctx, testID("tenant"), id, "user_view")
				return err
			},
			assert: func(t *testing.T, repo *Repository, id guid.ID) {
				permissions, err := repo.ListPermissions(t.Context(), testID("tenant"), id)
				require.NoError(t, err)
				assert.Empty(t, permissions)
			},
		},
		{
			name: "revoke permission", eventType: "role_permission_revoked", prepare: func(t *testing.T, repo *Repository) guid.ID {
				id := prepareTransactionalRole(t, repo)
				_, err := repo.GrantPermission(t.Context(), testID("tenant"), id, "user_view")
				require.NoError(t, err)
				return id
			},
			mutate: func(ctx context.Context, service *Service, id guid.ID) error {
				_, err := service.RevokePermission(ctx, testID("tenant"), id, "user_view")
				return err
			},
			assert: func(t *testing.T, repo *Repository, id guid.ID) {
				permissions, err := repo.ListPermissions(t.Context(), testID("tenant"), id)
				require.NoError(t, err)
				assert.Equal(t, []string{"user_view"}, permissions)
			},
		},
		{
			name: "condition permission", eventType: "role_permission_condition_updated", prepare: func(t *testing.T, repo *Repository) guid.ID {
				id := prepareTransactionalRole(t, repo)
				_, err := repo.GrantPermission(t.Context(), testID("tenant"), id, "user_view")
				require.NoError(t, err)
				return id
			},
			mutate: func(ctx context.Context, service *Service, id guid.ID) error {
				_, _, err := service.SetPermissionCondition(ctx, testID("tenant"), id, "user_view", json.RawMessage(`{"version":1,"gte":[{"context":"auth.acr"},{"value":2}]}`))
				return err
			},
			assert: func(t *testing.T, repo *Repository, id guid.ID) {
				grants, err := repo.ListPermissionGrants(t.Context(), testID("tenant"), id)
				require.NoError(t, err)
				require.Len(t, grants, 1)
				assert.Empty(t, grants[0].Condition)
			},
		},
		{
			name: "add role member", eventType: "role_member_added", prepare: prepareTransactionalRoleAndMember,
			mutate: func(ctx context.Context, service *Service, id guid.ID) error {
				_, err := service.AddMember(ctx, testID("tenant"), id, testID("member"))
				return err
			},
			assert: func(t *testing.T, repo *Repository, id guid.ID) {
				members, err := repo.ListMembers(t.Context(), testID("tenant"), id)
				require.NoError(t, err)
				assert.Empty(t, members)
			},
		},
		{
			name: "remove role member", eventType: "role_member_removed", prepare: func(t *testing.T, repo *Repository) guid.ID {
				id := prepareTransactionalRoleAndMember(t, repo)
				_, err := repo.AddMember(t.Context(), testID("tenant"), id, testID("member"))
				require.NoError(t, err)
				return id
			},
			mutate: func(ctx context.Context, service *Service, id guid.ID) error {
				_, err := service.RemoveMember(ctx, testID("tenant"), id, testID("member"))
				return err
			},
			assert: func(t *testing.T, repo *Repository, id guid.ID) {
				members, err := repo.ListMembers(t.Context(), testID("tenant"), id)
				require.NoError(t, err)
				assert.Equal(t, []guid.ID{testID("member")}, members)
			},
		},
		{
			name: "upsert tenant member", eventType: "tenant_member_upserted", prepare: func(t *testing.T, repo *Repository) guid.ID {
				putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("member"), DisplayName: "Original", Status: MemberActive})
				return testID("member")
			},
			mutate: func(ctx context.Context, service *Service, _ guid.ID) error {
				_, err := service.PutTenantMember(ctx, testID("tenant"), testID("member"), "Changed", "", 0, MemberActive)
				return err
			},
			assert: func(t *testing.T, repo *Repository, _ guid.ID) {
				member, err := repo.GetMember(t.Context(), testID("tenant"), testID("member"))
				require.NoError(t, err)
				assert.Equal(t, "Original", member.DisplayName)
			},
		},
		{
			name: "disable tenant member", eventType: "tenant_member_status_changed", prepare: prepareTransactionalMember,
			mutate: func(ctx context.Context, service *Service, _ guid.ID) error {
				_, err := service.SetTenantMemberStatus(ctx, testID("tenant"), testID("member"), MemberDisabled)
				return err
			},
			assert: func(t *testing.T, repo *Repository, _ guid.ID) {
				member, err := repo.GetMember(t.Context(), testID("tenant"), testID("member"))
				require.NoError(t, err)
				assert.Equal(t, MemberActive, member.Status)
			},
		},
		{
			name: "create menu", eventType: "menu_created",
			mutate: func(ctx context.Context, service *Service, _ guid.ID) error {
				_, err := service.CreateMenu(ctx, Menu{TenantID: testID("tenant"), Label: "Users", Route: "/users", Status: MenuActive})
				return err
			},
			assert: func(t *testing.T, repo *Repository, _ guid.ID) {
				menus, err := repo.ListMenus(t.Context(), testID("tenant"), false)
				require.NoError(t, err)
				assert.Empty(t, menus)
			},
		},
		{
			name: "update menu", eventType: "menu_updated", prepare: prepareTransactionalMenu,
			mutate: func(ctx context.Context, service *Service, id guid.ID) error {
				_, err := service.UpdateMenu(ctx, Menu{TenantID: testID("tenant"), ID: id, Label: "Changed", Route: "/users", Status: MenuActive})
				return err
			},
			assert: func(t *testing.T, repo *Repository, id guid.ID) {
				menu, err := repo.GetMenu(t.Context(), testID("tenant"), id)
				require.NoError(t, err)
				assert.Equal(t, "Original", menu.Label)
			},
		},
		{
			name: "delete menu", eventType: "menu_deleted", prepare: prepareTransactionalMenu,
			mutate: func(ctx context.Context, service *Service, id guid.ID) error {
				return service.DeleteMenu(ctx, testID("tenant"), id)
			},
			assert: func(t *testing.T, repo *Repository, id guid.ID) {
				_, err := repo.GetMenu(t.Context(), testID("tenant"), id)
				require.NoError(t, err)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newIAMRepository(t)
			service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
			var id guid.ID
			if test.prepare != nil {
				id = test.prepare(t, repo)
			}
			rejectAuditEvent(t, repo, test.eventType)
			err := test.mutate(t.Context(), service, id)
			require.ErrorContains(t, err, "append audit event")
			test.assert(t, repo, id)
			revision, revisionErr := repo.policyRevision(t.Context(), testID("tenant"))
			require.NoError(t, revisionErr)
			assert.Zero(t, revision)
			events, total, auditErr := auditmod.NewService(repo.db, newTestIDGenerator()).List(t.Context(), auditmod.Filter{TenantID: testID("tenant"), Offset: 0, Limit: 10})
			require.NoError(t, auditErr)
			assert.Zero(t, total)
			assert.Empty(t, events)
		})
	}
}

func TestPolicyRevisionFailureRollsBackMutationBeforeAudit(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	_, err := repo.db.ExecContext(t.Context(), "DROP TABLE iam_policy_revisions")
	require.NoError(t, err)

	_, err = service.CreateRole(t.Context(), testID("tenant"), "Operators", "")
	require.ErrorContains(t, err, "advance IAM policy revision")
	roles, listErr := repo.ListRoles(t.Context(), testID("tenant"))
	require.NoError(t, listErr)
	assert.Empty(t, roles)
	events, total, auditErr := auditmod.NewService(repo.db, newTestIDGenerator()).List(t.Context(), auditmod.Filter{TenantID: testID("tenant"), Offset: 0, Limit: 10})
	require.NoError(t, auditErr)
	assert.Zero(t, total)
	assert.Empty(t, events)
}

func rejectAuditEvent(t *testing.T, repo *Repository, eventType string) {
	t.Helper()
	escapedEventType := strings.ReplaceAll(eventType, "'", "''")
	query := fmt.Sprintf(`CREATE TRIGGER reject_test_audit_event BEFORE INSERT ON iam_audit_events
		WHEN NEW.event_type = '%s' BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END`, escapedEventType)
	_, err := repo.db.ExecContext(t.Context(), query)
	require.NoError(t, err)
}

func newTestAuditAppender(db *bun.DB) auditx.Appender {
	service := auditmod.NewService(db, newTestIDGenerator())
	return func(ctx context.Context, executor bun.IDB, event auditx.Event) error {
		_, err := service.AppendTo(ctx, executor, auditmod.EventInput{
			TenantID: event.TenantID, PrincipalID: event.PrincipalID,
			EventType: event.EventType, TargetType: event.TargetType, TargetID: event.TargetID,
			Outcome: "success", Detail: event.Detail,
		})
		return err
	}
}

func prepareTransactionalRole(t *testing.T, repo *Repository) guid.ID {
	t.Helper()
	role, err := repo.CreateRole(t.Context(), testID("tenant"), "Original", "")
	require.NoError(t, err)
	return role.ID
}

func prepareTransactionalMember(t *testing.T, repo *Repository) guid.ID {
	t.Helper()
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("member"), DisplayName: "Member", Status: MemberActive})
	return testID("member")
}

func prepareTransactionalRoleAndMember(t *testing.T, repo *Repository) guid.ID {
	t.Helper()
	id := prepareTransactionalRole(t, repo)
	prepareTransactionalMember(t, repo)
	return id
}

func prepareTransactionalMenu(t *testing.T, repo *Repository) guid.ID {
	t.Helper()
	menu, err := repo.CreateMenu(t.Context(), Menu{TenantID: testID("tenant"), Label: "Original", Route: "/users", Status: MenuActive})
	require.NoError(t, err)
	return menu.ID
}
