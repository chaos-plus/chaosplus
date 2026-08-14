package iam

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestAuthorizerImmediateTenantRBAC(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant-a"), PrincipalID: testID("principal-a"), DisplayName: "Principal A", Status: MemberActive})
	var err error
	role := createTestRole(t, repo, testID("tenant-a"), "Operators")
	_, err = repo.GrantPermission(ctx, testID("tenant-a"), role.ID, "store_view")
	require.NoError(t, err)
	_, err = repo.AddMember(ctx, testID("tenant-a"), role.ID, testID("principal-a"))
	require.NoError(t, err)
	authorizer := NewAuthorizer(repo.db)
	allowed, err := authorizer.Check(ctx, testID("tenant-a"), "store_view", testID("principal-a"))
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, err = authorizer.Check(ctx, testID("tenant-b"), "store_view", testID("principal-a"))
	require.NoError(t, err)
	assert.False(t, allowed)
	_, err = repo.SetMemberStatus(ctx, testID("tenant-a"), testID("principal-a"), MemberDisabled)
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, testID("tenant-a"), "store_view", testID("principal-a"))
	require.NoError(t, err)
	assert.False(t, allowed)
	_, err = repo.SetMemberStatus(ctx, testID("tenant-a"), testID("principal-a"), MemberActive)
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, testID("tenant-a"), "store_view", testID("principal-a"))
	require.NoError(t, err)
	assert.True(t, allowed)
	_, err = repo.RevokePermission(ctx, testID("tenant-a"), role.ID, "store_view")
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, testID("tenant-a"), "store_view", testID("principal-a"))
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestAuthorizerFollowsTenantLifecycle(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("principal"), DisplayName: "Principal", Status: MemberActive})
	role := createTestRole(t, repo, testID("tenant"), "Viewer")
	_, err := repo.GrantPermission(ctx, testID("tenant"), role.ID, "store_view")
	require.NoError(t, err)
	_, err = repo.AddMember(ctx, testID("tenant"), role.ID, testID("principal"))
	require.NoError(t, err)
	entity := entityTestRow(testID("tenant"), testID("store"), 0, "store")
	_, err = repo.db.NewInsert().Model(&entity).Exec(ctx)
	require.NoError(t, err)
	now := time.Now().UTC().UnixMilli()
	_, err = repo.db.NewInsert().Model(&roleBindingForTest{TenantID: testID("tenant"), RoleID: role.ID, PrincipalID: testID("principal"), ScopeType: "entity", ScopeID: testID("store"), Effect: "allow", CreatedAt: now}).Exec(ctx)
	require.NoError(t, err)

	authorizer := NewAuthorizer(repo.db)
	allowed, err := authorizer.Check(ctx, testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, err = authorizer.CheckEntity(ctx, testID("tenant"), testID("store"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, allowed)

	for _, status := range []string{"suspended", "active", "deleted"} {
		_, err = repo.db.NewUpdate().Table("iam_tenants").Set("status = ?", status).Where("id = ?", testID("tenant")).Exec(ctx)
		require.NoError(t, err)
		allowed, err = authorizer.Check(ctx, testID("tenant"), "store_view", testID("principal"))
		require.NoError(t, err)
		assert.Equal(t, status == "active", allowed)
		allowed, err = authorizer.CheckEntity(ctx, testID("tenant"), testID("store"), "store_view", testID("principal"))
		require.NoError(t, err)
		assert.Equal(t, status == "active", allowed)
		if status != "active" {
			explanation, explainErr := authorizer.ExplainEntity(ctx, testID("tenant"), testID("store"), "store_view", testID("principal"))
			require.NoError(t, explainErr)
			assert.Equal(t, "inactive_tenant", explanation.Reason)
		}
	}
}

// TestAuthorizerDialectBehaviorContract exercises the authorization SQL that
// differs most between engines: the four-branch UNION CTE in loadTenantGrants,
// the WITH RECURSIVE entity walk in loadScopedGrants, and the deny-wins
// resolution in decideEntity. Every other authorizer test runs on SQLite only,
// so this is the contract that proves MySQL and PostgreSQL agree.
func TestAuthorizerDialectBehaviorContract(t *testing.T) {
	db := newLifecycleDatabase(t)
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, testID("tenant")))

	repo := NewRepository(db, newTestIDGenerator())
	ctx := t.Context()
	authorizer := NewAuthorizer(db)

	_, err := repo.PutMember(ctx, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("principal"), DisplayName: "Principal", Status: MemberActive})
	require.NoError(t, err)

	// Direct role membership resolves through the first CTE branch.
	direct, err := repo.CreateRole(ctx, testID("tenant"), "Direct", "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(ctx, testID("tenant"), direct.ID, "store_view")
	require.NoError(t, err)
	_, err = repo.AddMember(ctx, testID("tenant"), direct.ID, testID("principal"))
	require.NoError(t, err)
	allowed, err := authorizer.Check(ctx, testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, allowed, "direct role membership must grant on every dialect")

	// A temporary grant resolves through the second CTE branch and respects
	// its validity window on both engines.
	temporary, err := repo.CreateRole(ctx, testID("tenant"), "Temporary", "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(ctx, testID("tenant"), temporary.ID, "user_view")
	require.NoError(t, err)
	now := time.Now().UTC()
	require.NoError(t, GrantTemporaryRole(ctx, db, testID("tenant"), testID("grant-active"), temporary.ID, testID("principal"), testID("actor"), now.Add(-time.Hour), now.Add(time.Hour)))
	allowed, err = authorizer.Check(ctx, testID("tenant"), "user_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, allowed, "an active temporary grant must apply")
	revoked, err := RevokeTemporaryRole(ctx, db, testID("tenant"), testID("grant-active"))
	require.NoError(t, err)
	assert.True(t, revoked)
	require.NoError(t, GrantTemporaryRole(ctx, db, testID("tenant"), testID("grant-expired"), temporary.ID, testID("principal"), testID("actor"), now.Add(-2*time.Hour), now.Add(-time.Hour)))
	allowed, err = authorizer.Check(ctx, testID("tenant"), "user_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, allowed, "an expired temporary grant must not apply")

	// Entity scoped bindings walk the recursive CTE from parent to child.
	parent, err := repo.CreateEntity(ctx, iamdomain.Entity{TenantID: testID("tenant"), Type: "company", Name: "Parent", Status: iamdomain.EntityActive})
	require.NoError(t, err)
	child, err := repo.CreateEntity(ctx, iamdomain.Entity{TenantID: testID("tenant"), ParentID: parent.ID, Type: "store", Name: "Child", Status: iamdomain.EntityActive})
	require.NoError(t, err)
	grandchild, err := repo.CreateEntity(ctx, iamdomain.Entity{TenantID: testID("tenant"), ParentID: child.ID, Type: "store", Name: "Grandchild", Status: iamdomain.EntityActive})
	require.NoError(t, err)
	scoped, err := repo.CreateRole(ctx, testID("tenant"), "Scoped", "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(ctx, testID("tenant"), scoped.ID, "merchant_view")
	require.NoError(t, err)
	_, _, err = repo.PutEntityRoleBinding(ctx, testID("tenant"), parent.ID, scoped.ID, testID("principal"), iamdomain.BindingAllow, time.Time{})
	require.NoError(t, err)

	for _, entityID := range []guid.ID{parent.ID, child.ID, grandchild.ID} {
		allowed, err = authorizer.CheckEntity(ctx, testID("tenant"), entityID, "merchant_view", testID("principal"))
		require.NoError(t, err)
		assert.True(t, allowed, "recursive inheritance must reach %s", entityID)
	}

	// An explicit deny on the child wins over the inherited allow and does not
	// revoke the parent.
	_, _, err = repo.PutEntityRoleBinding(ctx, testID("tenant"), child.ID, scoped.ID, testID("principal"), iamdomain.BindingDeny, time.Time{})
	require.NoError(t, err)
	explanation, err := authorizer.ExplainEntity(ctx, testID("tenant"), child.ID, "merchant_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, explanation.Allowed)
	assert.Equal(t, "explicit_deny", explanation.Reason)
	allowed, err = authorizer.CheckEntity(ctx, testID("tenant"), parent.ID, "merchant_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, allowed, "denying a child must not revoke the parent")

	// Constraint compiles the same state into concrete identifiers.
	constraint, err := authorizer.Constraint(ctx, testID("tenant"), "merchant_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, constraint.AllowAll)
	assert.Contains(t, constraint.ResourceIDs, parent.ID)
	assert.Contains(t, constraint.DeniedIDs, child.ID)
	// This test writes through the repository, which never bumps the policy
	// revision, so the value is only required to be consistent across the two
	// evaluation entry points.
	assert.GreaterOrEqual(t, constraint.Revision, int64(0))
	assert.Equal(t, explanation.Revision, constraint.Revision)

	// A tenant-wide administrator short-circuits to AllowAll on every dialect.
	administrator, err := repo.CreateRole(ctx, testID("tenant"), "Administrators", "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(ctx, testID("tenant"), administrator.ID, "tenant_administer")
	require.NoError(t, err)
	_, err = repo.AddMember(ctx, testID("tenant"), administrator.ID, testID("principal"))
	require.NoError(t, err)
	constraint, err = authorizer.Constraint(ctx, testID("tenant"), "merchant_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, constraint.AllowAll)

	// Bulk evaluation must agree with the single-permission path.
	bulk, err := authorizer.CheckBulk(ctx, testID("tenant"), []string{"store_view", "merchant_view", "user_delete"}, testID("principal"))
	require.NoError(t, err)
	assert.True(t, bulk["store_view"])
	assert.True(t, bulk["merchant_view"])
	assert.True(t, bulk["user_delete"], "tenant_administer satisfies every tenant permission")

	// Disabling the membership revokes everything immediately, with no cache
	// to invalidate.
	_, err = repo.SetMemberStatus(ctx, testID("tenant"), testID("principal"), MemberDisabled)
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestAuthorizerPlatformAdministrationIsNotATenantGrant(t *testing.T) {
	repo := newIAMRepository(t)
	now := time.Now().UTC().UnixMilli()
	_, err := repo.db.ExecContext(t.Context(), `INSERT INTO iam_principals
		(id, login_name, email, display_name, status, created_at, updated_at, disabled_at)
		VALUES (?, ?, '', ?, 'active', ?, ?, 0)`, testID("platform-admin"), testID("platform-admin"), "Platform Admin", now, now)
	require.NoError(t, err)
	changed, err := repo.GrantPlatformAdministrator(t.Context(), testID("platform-admin"))
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = repo.GrantPlatformAdministrator(t.Context(), testID("platform-admin"))
	require.NoError(t, err)
	assert.False(t, changed)
	authorizer := NewAuthorizer(repo.db)
	allowed, err := authorizer.CheckPlatform(t.Context(), "tenant_view", testID("platform-admin"))
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, err = authorizer.Check(t.Context(), testID("tenant"), "store_view", testID("platform-admin"))
	require.NoError(t, err)
	assert.False(t, allowed, "platform administrators must still enter a tenant through active membership")

	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("platform-admin"), DisplayName: "Platform Admin", Status: MemberActive})
	allowed, err = authorizer.Check(t.Context(), testID("tenant"), "store_view", testID("platform-admin"))
	require.NoError(t, err)
	assert.True(t, allowed)
	_, err = repo.db.ExecContext(t.Context(), "UPDATE iam_principals SET status = 'disabled' WHERE id = ?", testID("platform-admin"))
	require.NoError(t, err)
	allowed, err = authorizer.CheckPlatform(t.Context(), "tenant_view", testID("platform-admin"))
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestAuthorizerRejectsInvalidRequestsAndStorageFailure(t *testing.T) {
	require.Panics(t, func() { NewAuthorizer(nil) })
	repo := newIAMRepository(t)
	authorizer := NewAuthorizer(repo.db)

	allowed, err := authorizer.CheckBulk(t.Context(), 0, nil, 0)
	require.NoError(t, err)
	assert.Empty(t, allowed)
	_, err = authorizer.CheckBulk(t.Context(), testID("tenant"), []string{""}, testID("principal"))
	assert.Error(t, err)
	_, err = authorizer.Constraint(t.Context(), testID("tenant"), "store_view", 0)
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)
	_, err = authorizer.ExplainEntity(t.Context(), testID("tenant"), testID("missing"), "store_view", testID("principal"))
	assert.ErrorIs(t, err, iamdomain.ErrEntityNotFound)

	_, err = repo.db.ExecContext(t.Context(), "DROP TABLE iam_policy_revisions")
	require.NoError(t, err)
	_, err = authorizer.Constraint(t.Context(), testID("tenant"), "store_view", testID("principal"))
	assert.ErrorContains(t, err, "get IAM policy revision")
}

func TestAuthorizerConstraintAndExplanation(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("principal"), DisplayName: "Principal", Status: MemberActive})
	var err error
	entities := []entityRowForTest{
		entityTestRow(testID("tenant"), testID("company"), 0, "company"),
		entityTestRow(testID("tenant"), testID("store-a"), testID("company"), "store"),
		entityTestRow(testID("tenant"), testID("store-b"), testID("company"), "store"),
		entityTestRow(testID("tenant"), testID("other"), 0, "store"),
		entityTestRow(testID("other-tenant"), testID("foreign"), 0, "store"),
	}
	_, err = repo.db.NewInsert().Model(&entities).Exec(ctx)
	require.NoError(t, err)
	allowRole := createTestRole(t, repo, testID("tenant"), "Scoped viewer")
	denyRole := createTestRole(t, repo, testID("tenant"), "Scoped denial")
	adminRole := createTestRole(t, repo, testID("tenant"), "Tenant administrator")
	for _, role := range []Role{allowRole, denyRole} {
		_, err = repo.GrantPermission(ctx, testID("tenant"), role.ID, "store_view")
		require.NoError(t, err)
	}
	_, err = repo.GrantPermission(ctx, testID("tenant"), adminRole.ID, "tenant_administer")
	require.NoError(t, err)
	now := time.Now().UTC().UnixMilli()
	bindings := []roleBindingForTest{
		{TenantID: testID("tenant"), RoleID: allowRole.ID, PrincipalID: testID("principal"), ScopeType: "entity", ScopeID: testID("company"), Effect: "allow", CreatedAt: now},
		{TenantID: testID("tenant"), RoleID: denyRole.ID, PrincipalID: testID("principal"), ScopeType: "entity", ScopeID: testID("store-b"), Effect: "deny", CreatedAt: now},
	}
	_, err = repo.db.NewInsert().Model(&bindings).Exec(ctx)
	require.NoError(t, err)
	require.NoError(t, policyx.Advance(ctx, repo.db, repo.dialect, testID("tenant"), now))

	authorizer := NewAuthorizer(repo.db)
	constraint, err := authorizer.Constraint(ctx, testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, constraint.AllowAll)
	assert.Equal(t, []string{testID("company").String(), testID("store-a").String()}, constraint.ResourceIDs)
	assert.Equal(t, []string{testID("store-b").String()}, constraint.DeniedIDs)
	assert.Equal(t, []string{testID("company").String()}, []string{constraint.Ancestors[0].ID})
	assert.EqualValues(t, 1, constraint.Revision)
	assert.NotNil(t, constraint.OwnerIDs)
	assert.NotNil(t, constraint.DepartmentIDs)

	allowed, err := authorizer.ExplainEntity(ctx, testID("tenant"), testID("store-a"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, allowed.Allowed)
	assert.Equal(t, "permission_grant", allowed.Reason)
	require.Len(t, allowed.Matches, 1)
	assert.True(t, allowed.Matches[0].Inherited)

	denied, err := authorizer.ExplainEntity(ctx, testID("tenant"), testID("store-b"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, denied.Allowed)
	assert.Equal(t, "explicit_deny", denied.Reason)
	assert.Len(t, denied.Matches, 2)

	_, err = repo.AddMember(ctx, testID("tenant"), adminRole.ID, testID("principal"))
	require.NoError(t, err)
	constraint, err = authorizer.Constraint(ctx, testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, constraint.AllowAll)
	assert.Empty(t, constraint.ResourceIDs)
	assert.Equal(t, []string{testID("store-b").String()}, constraint.DeniedIDs)
	allowed, err = authorizer.ExplainEntity(ctx, testID("tenant"), testID("other"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, allowed.Allowed)
	assert.Equal(t, "administrator_grant", allowed.Reason)
	assert.Contains(t, allowed.Matches[0].SourceType, "role_member")

	_, err = repo.SetMemberStatus(ctx, testID("tenant"), testID("principal"), MemberDisabled)
	require.NoError(t, err)
	constraint, err = authorizer.Constraint(ctx, testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, constraint.AllowAll)
	assert.Empty(t, constraint.ResourceIDs)
	explanation, err := authorizer.ExplainEntity(ctx, testID("tenant"), testID("company"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.Equal(t, "inactive_membership", explanation.Reason)
	_, err = authorizer.ExplainEntity(ctx, testID("tenant"), testID("missing"), "store_view", testID("principal"))
	assert.True(t, errors.Is(err, iamdomain.ErrEntityNotFound))
}

func TestAuthorizerAdministratorExpansion(t *testing.T) {
	repo := newIAMRepository(t)
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("principal"), DisplayName: "Principal", Status: MemberActive})
	var err error
	role := createTestRole(t, repo, testID("tenant"), "Administrators")
	_, err = repo.GrantPermission(context.Background(), testID("tenant"), role.ID, "tenant_administer")
	require.NoError(t, err)
	_, err = repo.AddMember(context.Background(), testID("tenant"), role.ID, testID("principal"))
	require.NoError(t, err)
	allowed, err := NewAuthorizer(repo.db).Check(context.Background(), testID("tenant"), "menu_delete", testID("principal"))
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, err = NewAuthorizer(repo.db).Check(context.Background(), testID("tenant"), "platform_administer", testID("principal"))
	require.NoError(t, err)
	assert.False(t, allowed)
	_, err = repo.GrantPermission(context.Background(), testID("tenant"), role.ID, "platform_administer")
	require.NoError(t, err)
	allowed, err = NewAuthorizer(repo.db).Check(context.Background(), testID("tenant"), "platform_administer", testID("principal"))
	require.NoError(t, err)
	assert.False(t, allowed, "persisted tenant grants must never confer platform authority")
}

func TestAuthorizerEntityInheritanceAndDeny(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("principal"), DisplayName: "Principal", Status: MemberActive})
	var err error
	parent := entityTestRow(testID("tenant"), testID("company"), 0, "company")
	child := entityTestRow(testID("tenant"), testID("store"), testID("company"), "store")
	_, err = repo.db.NewInsert().Model(&[]entityRowForTest{parent, child}).Exec(ctx)
	require.NoError(t, err)
	allowRole := createTestRole(t, repo, testID("tenant"), "Company viewer")
	denyRole := createTestRole(t, repo, testID("tenant"), "Store deny")
	_, err = repo.GrantPermission(ctx, testID("tenant"), allowRole.ID, "store_view")
	require.NoError(t, err)
	_, err = repo.GrantPermission(ctx, testID("tenant"), denyRole.ID, "store_view")
	require.NoError(t, err)
	now := time.Now().UTC().UnixMilli()
	bindings := []roleBindingForTest{
		{TenantID: testID("tenant"), RoleID: allowRole.ID, PrincipalID: testID("principal"), ScopeType: "entity", ScopeID: testID("company"), Effect: "allow", CreatedAt: now},
		{TenantID: testID("tenant"), RoleID: denyRole.ID, PrincipalID: testID("principal"), ScopeType: "entity", ScopeID: testID("store"), Effect: "deny", CreatedAt: now},
	}
	_, err = repo.db.NewInsert().Model(&bindings).Exec(ctx)
	require.NoError(t, err)
	allowed, err := NewAuthorizer(repo.db).CheckEntity(ctx, testID("tenant"), testID("store"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, allowed)
	_, err = repo.db.NewDelete().Model((*roleBindingForTest)(nil)).Where("effect = 'deny'").Exec(ctx)
	require.NoError(t, err)
	allowed, err = NewAuthorizer(repo.db).CheckEntity(ctx, testID("tenant"), testID("store"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, allowed)
	_, err = repo.SetMemberStatus(ctx, testID("tenant"), testID("principal"), MemberDisabled)
	require.NoError(t, err)
	allowed, err = NewAuthorizer(repo.db).CheckEntity(ctx, testID("tenant"), testID("store"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestAuthorizerScopedAdministratorDenyAndExpiredBinding(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("principal"), DisplayName: "Principal", Status: MemberActive})
	var err error
	_, err = repo.db.NewInsert().Model(&[]entityRowForTest{
		entityTestRow(testID("tenant"), testID("company"), 0, "company"),
		entityTestRow(testID("tenant"), testID("store"), testID("company"), "store"),
	}).Exec(ctx)
	require.NoError(t, err)

	adminRole := createTestRole(t, repo, testID("tenant"), "Scoped administrator")
	viewerRole := createTestRole(t, repo, testID("tenant"), "Direct viewer")
	expiredRole := createTestRole(t, repo, testID("tenant"), "Expired viewer")
	_, err = repo.GrantPermission(ctx, testID("tenant"), adminRole.ID, "tenant_administer")
	require.NoError(t, err)
	for _, role := range []Role{viewerRole, expiredRole} {
		_, err = repo.GrantPermission(ctx, testID("tenant"), role.ID, "store_view")
		require.NoError(t, err)
	}
	now := repo.now().UnixMilli()
	bindings := []roleBindingForTest{
		{TenantID: testID("tenant"), RoleID: adminRole.ID, PrincipalID: testID("principal"), ScopeType: "entity", ScopeID: testID("company"), Effect: "allow", CreatedAt: now},
		{TenantID: testID("tenant"), RoleID: adminRole.ID, PrincipalID: testID("principal"), ScopeType: "entity", ScopeID: testID("store"), Effect: "deny", CreatedAt: now},
		{TenantID: testID("tenant"), RoleID: expiredRole.ID, PrincipalID: testID("principal"), ScopeType: "entity", ScopeID: testID("store"), Effect: "allow", ExpiresAt: now, CreatedAt: now},
	}
	_, err = repo.db.NewInsert().Model(&bindings).Exec(ctx)
	require.NoError(t, err)
	authorizer := NewAuthorizer(repo.db)
	authorizer.now = repo.now

	explanation, err := authorizer.ExplainEntity(ctx, testID("tenant"), testID("store"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, explanation.Allowed)
	assert.Equal(t, "administrator_scope_denied", explanation.Reason)
	require.Len(t, explanation.Matches, 2)
	for _, match := range explanation.Matches {
		assert.NotEqual(t, expiredRole.ID, match.RoleID)
	}

	_, err = repo.db.NewInsert().Model(&roleBindingForTest{
		TenantID: testID("tenant"), RoleID: viewerRole.ID, PrincipalID: testID("principal"), ScopeType: "entity", ScopeID: testID("store"), Effect: "allow", CreatedAt: now,
	}).Exec(ctx)
	require.NoError(t, err)
	explanation, err = authorizer.ExplainEntity(ctx, testID("tenant"), testID("store"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, explanation.Allowed)
	assert.Equal(t, "permission_grant", explanation.Reason)
}

func TestAuthorizerAppliesRoleConditionsToEveryAssignmentSource(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("principal"), DisplayName: "Principal", Email: "principal@example.test", Status: MemberActive})
	now := repo.now().UTC().UnixMilli()
	condition := json.RawMessage(`{"version":1,"gte":[{"context":"auth.acr"},{"value":2}]}`)
	type source struct {
		name string
		code string
		bind func(Role)
	}
	sources := []source{
		{name: "Direct", code: "user_view", bind: func(role Role) {
			_, err := repo.AddMember(ctx, testID("tenant"), role.ID, testID("principal"))
			require.NoError(t, err)
		}},
		{name: "Temporary", code: "role_view", bind: func(role Role) {
			_, err := repo.db.ExecContext(ctx, `INSERT INTO iam_temporary_role_grants
				(tenant_id,id,role_id,principal_id,source_type,source_id,starts_at,ends_at,created_by,created_at)
				VALUES (?,?,?,?,'access_request',?,?,?,?,?)`, testID("tenant"), testID("temporary"), role.ID, testID("principal"), testID("request"), now-1, now+time.Hour.Milliseconds(), testID("approver"), now)
			require.NoError(t, err)
		}},
		{name: "Static group", code: "menu_view", bind: func(role Role) {
			_, err := repo.db.ExecContext(ctx, `INSERT INTO iam_groups
				(tenant_id,id,name,name_key,group_type,description,status,sort_order,version,created_at,updated_at)
				VALUES (?,?,?,'static','static','','active',0,1,?,?)`, testID("tenant"), testID("static"), "Static", now, now)
			require.NoError(t, err)
			_, err = repo.db.ExecContext(ctx, `INSERT INTO iam_group_members
				(tenant_id,group_id,principal_id,starts_at,ends_at,created_at,updated_at)
				VALUES (?,?,?,0,0,?,?)`, testID("tenant"), testID("static"), testID("principal"), now, now)
			require.NoError(t, err)
			_, err = repo.db.ExecContext(ctx, "INSERT INTO iam_group_role_bindings (tenant_id,role_id,group_id,created_at) VALUES (?,?,?,?)", testID("tenant"), role.ID, testID("static"), now)
			require.NoError(t, err)
		}},
		{name: "Dynamic group", code: "entity_view", bind: func(role Role) {
			rule := `{"version":1,"match":"all","conditions":[{"field":"member.status","operator":"in","values":["active"]}]}`
			_, err := repo.db.ExecContext(ctx, `INSERT INTO iam_groups
				(tenant_id,id,name,name_key,group_type,rule_json,description,status,sort_order,version,created_at,updated_at)
				VALUES (?,?,?,'dynamic','dynamic',?,'','active',0,1,?,?)`, testID("tenant"), testID("dynamic"), "Dynamic", rule, now, now)
			require.NoError(t, err)
			_, err = repo.db.ExecContext(ctx, "INSERT INTO iam_group_role_bindings (tenant_id,role_id,group_id,created_at) VALUES (?,?,?,?)", testID("tenant"), role.ID, testID("dynamic"), now)
			require.NoError(t, err)
		}},
		{name: "Position", code: "store_view", bind: func(role Role) {
			_, err := repo.db.ExecContext(ctx, `INSERT INTO iam_positions
				(tenant_id,id,code,name,status,sort_order,version,created_at,updated_at)
				VALUES (?,?,?,'Operator','active',0,1,?,?)`, testID("tenant"), testID("position"), "operator", now, now)
			require.NoError(t, err)
			_, err = repo.db.ExecContext(ctx, `INSERT INTO iam_position_members
				(tenant_id,position_id,principal_id,starts_at,ends_at,created_at,updated_at)
				VALUES (?,?,?,0,0,?,?)`, testID("tenant"), testID("position"), testID("principal"), now, now)
			require.NoError(t, err)
			_, err = repo.db.ExecContext(ctx, "INSERT INTO iam_position_role_bindings (tenant_id,role_id,position_id,created_at) VALUES (?,?,?,?)", testID("tenant"), role.ID, testID("position"), now)
			require.NoError(t, err)
		}},
	}
	roles := make(map[string]Role, len(sources))
	codes := make([]string, 0, len(sources))
	for _, item := range sources {
		role := createTestRole(t, repo, testID("tenant"), item.name)
		_, err := repo.GrantPermission(ctx, testID("tenant"), role.ID, item.code)
		require.NoError(t, err)
		_, changed, err := repo.SetPermissionCondition(ctx, testID("tenant"), role.ID, item.code, condition)
		require.NoError(t, err)
		assert.True(t, changed)
		item.bind(role)
		roles[item.code] = role
		codes = append(codes, item.code)
	}
	authorizer := NewAuthorizer(repo.db)
	authorizer.now = repo.now
	low := policyx.WithTrustedContext(ctx, policyx.TrustedContext{ACR: 1})
	allowed, err := authorizer.CheckBulk(low, testID("tenant"), codes, testID("principal"))
	require.NoError(t, err)
	for _, code := range codes {
		assert.False(t, allowed[code], code)
	}
	high := policyx.WithTrustedContext(ctx, policyx.TrustedContext{ACR: 2})
	allowed, err = authorizer.CheckBulk(high, testID("tenant"), codes, testID("principal"))
	require.NoError(t, err)
	for _, code := range codes {
		assert.True(t, allowed[code], code)
	}

	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("scoped"), DisplayName: "Scoped", Status: MemberActive})
	entity := entityTestRow(testID("tenant"), testID("store"), 0, "store")
	_, err = repo.db.NewInsert().Model(&entity).Exec(ctx)
	require.NoError(t, err)
	scopedRole := createTestRole(t, repo, testID("tenant"), "Scoped")
	_, err = repo.GrantPermission(ctx, testID("tenant"), scopedRole.ID, "store_update")
	require.NoError(t, err)
	_, _, err = repo.SetPermissionCondition(ctx, testID("tenant"), scopedRole.ID, "store_update", condition)
	require.NoError(t, err)
	_, err = repo.db.NewInsert().Model(&roleBindingForTest{TenantID: testID("tenant"), RoleID: scopedRole.ID, PrincipalID: testID("scoped"), ScopeType: "entity", ScopeID: testID("store"), Effect: "allow", CreatedAt: now}).Exec(ctx)
	require.NoError(t, err)
	allowedEntity, err := authorizer.CheckEntity(low, testID("tenant"), testID("store"), "store_update", testID("scoped"))
	require.NoError(t, err)
	assert.False(t, allowedEntity)
	allowedEntity, err = authorizer.CheckEntity(high, testID("tenant"), testID("store"), "store_update", testID("scoped"))
	require.NoError(t, err)
	assert.True(t, allowedEntity)

	_, err = repo.db.NewUpdate().Table("iam_role_permissions").Set("condition_json = ?", "{broken").
		Where("tenant_id = ? AND role_id = ? AND permission_code = ?", testID("tenant"), roles["user_view"].ID, "user_view").Exec(ctx)
	require.NoError(t, err)
	allowed, err = authorizer.CheckBulk(high, testID("tenant"), codes, testID("principal"))
	assert.ErrorContains(t, err, "evaluate role permission condition")
	assert.Nil(t, allowed)
}

func TestAuthorizerResourceAttributeABAC(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("principal"), DisplayName: "Principal", Status: MemberActive})
	role := createTestRole(t, repo, testID("tenant"), "Order Operators")
	_, err := repo.GrantPermission(ctx, testID("tenant"), role.ID, "order_view")
	require.NoError(t, err)
	condition, err := policyx.CanonicalCondition(json.RawMessage(`{"version":1,"all":[
		{"eq":[{"context":"resource.type"},{"value":"order"}]},
		{"eq":[{"context":"resource.owner"},{"value":"` + testID("principal").String() + `"}]},
		{"in":[{"context":"resource.attr.region"},{"value":["cn-east"]}]}
	]}`))
	require.NoError(t, err)
	_, changed, err := repo.SetPermissionCondition(ctx, testID("tenant"), role.ID, "order_view", condition)
	require.NoError(t, err)
	assert.True(t, changed)

	entity := entityTestRow(testID("tenant"), testID("store"), 0, "store")
	_, err = repo.db.NewInsert().Model(&entity).Exec(ctx)
	require.NoError(t, err)
	now := time.Now().UTC().UnixMilli()
	_, err = repo.db.NewInsert().Model(&roleBindingForTest{TenantID: testID("tenant"), RoleID: role.ID, PrincipalID: testID("principal"), ScopeType: "entity", ScopeID: testID("store"), Effect: "allow", CreatedAt: now}).Exec(ctx)
	require.NoError(t, err)

	authorizer := NewAuthorizer(repo.db)
	authorizer.now = repo.now

	// Without server-provided resource facts the condition fails closed.
	allowed, err := authorizer.CheckResource(ctx, testID("tenant"), testID("store"), "order", testID("order-1"), "order_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, allowed)

	// Type and id come from the check arguments; owner and attributes come
	// from the server-side resource context.
	resourceCtx := policyx.WithResourceContext(ctx, policyx.ResourceContext{Owner: testID("principal").String(), Attrs: map[string]string{"region": "cn-east"}})
	allowed, err = authorizer.CheckResource(resourceCtx, testID("tenant"), testID("store"), "order", testID("order-1"), "order_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, allowed)

	wrong := policyx.WithResourceContext(ctx, policyx.ResourceContext{Owner: testID("principal").String(), Attrs: map[string]string{"region": "us-west"}})
	allowed, err = authorizer.CheckResource(wrong, testID("tenant"), testID("store"), "order", testID("order-1"), "order_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, allowed)

	// The same condition never leaks to a different resource type.
	other := policyx.WithResourceContext(ctx, policyx.ResourceContext{Owner: testID("principal").String(), Attrs: map[string]string{"region": "cn-east"}})
	allowed, err = authorizer.CheckResource(other, testID("tenant"), testID("store"), "invoice", testID("invoice-1"), "order_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, allowed)
}

type entityRowForTest struct {
	bun.BaseModel `bun:"table:iam_entities"`
	TenantID      guid.ID  `bun:"tenant_id"`
	ID            guid.ID  `bun:"id"`
	ParentID      *guid.ID `bun:"parent_id"`
	Type          string   `bun:"type"`
	Name          string   `bun:"name"`
	Status        string   `bun:"status"`
	Metadata      string   `bun:"metadata"`
	CreatedAt     int64    `bun:"created_at"`
	UpdatedAt     int64    `bun:"updated_at"`
}

func entityTestRow(tenantID, id, parentID guid.ID, entityType string) entityRowForTest {
	var parent *guid.ID
	if !parentID.Zero() {
		parent = &parentID
	}
	return entityRowForTest{TenantID: tenantID, ID: id, ParentID: parent, Type: entityType, Name: id.String(), Status: "active", Metadata: "{}", CreatedAt: 1, UpdatedAt: 1}
}

type roleBindingForTest struct {
	bun.BaseModel `bun:"table:iam_role_bindings"`
	TenantID      guid.ID `bun:"tenant_id"`
	RoleID        guid.ID `bun:"role_id"`
	PrincipalID   guid.ID `bun:"principal_id"`
	ScopeType     string  `bun:"scope_type"`
	ScopeID       guid.ID `bun:"scope_id"`
	Effect        string  `bun:"effect"`
	ExpiresAt     int64   `bun:"expires_at"`
	CreatedAt     int64   `bun:"created_at"`
}
