package iam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestAuthorizerImmediateTenantRBAC(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	putTestMember(t, repo, TenantMember{TenantID: "tenant-a", Subject: "principal-a", DisplayName: "Principal A", Status: MemberActive})
	var err error
	role := createTestRole(t, repo, "tenant-a", "Operators")
	_, err = repo.GrantPermission(ctx, "tenant-a", role.ID, "store_view")
	require.NoError(t, err)
	_, err = repo.AddMember(ctx, "tenant-a", role.ID, "principal-a")
	require.NoError(t, err)
	authorizer := NewAuthorizer(repo.db)
	allowed, err := authorizer.Check(ctx, "tenant-a", "store_view", "principal-a")
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, err = authorizer.Check(ctx, "tenant-b", "store_view", "principal-a")
	require.NoError(t, err)
	assert.False(t, allowed)
	_, err = repo.SetMemberStatus(ctx, "tenant-a", "principal-a", MemberDisabled)
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, "tenant-a", "store_view", "principal-a")
	require.NoError(t, err)
	assert.False(t, allowed)
	_, err = repo.SetMemberStatus(ctx, "tenant-a", "principal-a", MemberActive)
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, "tenant-a", "store_view", "principal-a")
	require.NoError(t, err)
	assert.True(t, allowed)
	_, err = repo.RevokePermission(ctx, "tenant-a", role.ID, "store_view")
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, "tenant-a", "store_view", "principal-a")
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestAuthorizerFollowsTenantLifecycle(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	putTestMember(t, repo, TenantMember{TenantID: "tenant", Subject: "principal", DisplayName: "Principal", Status: MemberActive})
	role := createTestRole(t, repo, "tenant", "Viewer")
	_, err := repo.GrantPermission(ctx, "tenant", role.ID, "store_view")
	require.NoError(t, err)
	_, err = repo.AddMember(ctx, "tenant", role.ID, "principal")
	require.NoError(t, err)
	entity := entityTestRow("tenant", "store", "", "store")
	_, err = repo.db.NewInsert().Model(&entity).Exec(ctx)
	require.NoError(t, err)
	now := time.Now().UTC().UnixMilli()
	_, err = repo.db.NewInsert().Model(&roleBindingForTest{TenantID: "tenant", RoleID: role.ID, PrincipalID: "principal", ScopeType: "entity", ScopeID: "store", Effect: "allow", CreatedAt: now}).Exec(ctx)
	require.NoError(t, err)

	authorizer := NewAuthorizer(repo.db)
	allowed, err := authorizer.Check(ctx, "tenant", "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, err = authorizer.CheckEntity(ctx, "tenant", "store", "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed)

	for _, status := range []string{"suspended", "active", "deleted"} {
		_, err = repo.db.NewUpdate().Table("iam_tenants").Set("status = ?", status).Where("id = ?", "tenant").Exec(ctx)
		require.NoError(t, err)
		allowed, err = authorizer.Check(ctx, "tenant", "store_view", "principal")
		require.NoError(t, err)
		assert.Equal(t, status == "active", allowed)
		allowed, err = authorizer.CheckEntity(ctx, "tenant", "store", "store_view", "principal")
		require.NoError(t, err)
		assert.Equal(t, status == "active", allowed)
		if status != "active" {
			explanation, explainErr := authorizer.ExplainEntity(ctx, "tenant", "store", "store_view", "principal")
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
	require.NoError(t, organization.EnsureTenant(t.Context(), db, "tenant"))

	var sequence atomic.Int64
	repo := NewRepository(db, func() (string, error) { return fmt.Sprintf("d%d", sequence.Add(1)), nil })
	ctx := t.Context()
	authorizer := NewAuthorizer(db)

	_, err := repo.PutMember(ctx, TenantMember{TenantID: "tenant", Subject: "principal", DisplayName: "Principal", Status: MemberActive})
	require.NoError(t, err)

	// Direct role membership resolves through the first CTE branch.
	direct, err := repo.CreateRole(ctx, "tenant", "Direct", "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(ctx, "tenant", direct.ID, "store_view")
	require.NoError(t, err)
	_, err = repo.AddMember(ctx, "tenant", direct.ID, "principal")
	require.NoError(t, err)
	allowed, err := authorizer.Check(ctx, "tenant", "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed, "direct role membership must grant on every dialect")

	// A temporary grant resolves through the second CTE branch and respects
	// its validity window on both engines.
	temporary, err := repo.CreateRole(ctx, "tenant", "Temporary", "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(ctx, "tenant", temporary.ID, "user_view")
	require.NoError(t, err)
	now := time.Now().UTC()
	require.NoError(t, GrantTemporaryRole(ctx, db, "tenant", "grant-active", temporary.ID, "principal", "actor", now.Add(-time.Hour), now.Add(time.Hour)))
	allowed, err = authorizer.Check(ctx, "tenant", "user_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed, "an active temporary grant must apply")
	revoked, err := RevokeTemporaryRole(ctx, db, "tenant", "grant-active")
	require.NoError(t, err)
	assert.True(t, revoked)
	require.NoError(t, GrantTemporaryRole(ctx, db, "tenant", "grant-expired", temporary.ID, "principal", "actor", now.Add(-2*time.Hour), now.Add(-time.Hour)))
	allowed, err = authorizer.Check(ctx, "tenant", "user_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed, "an expired temporary grant must not apply")

	// Entity scoped bindings walk the recursive CTE from parent to child.
	parent, err := repo.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant", Type: "company", Name: "Parent", Status: iamdomain.EntityActive})
	require.NoError(t, err)
	child, err := repo.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant", ParentID: parent.ID, Type: "store", Name: "Child", Status: iamdomain.EntityActive})
	require.NoError(t, err)
	grandchild, err := repo.CreateEntity(ctx, iamdomain.Entity{TenantID: "tenant", ParentID: child.ID, Type: "store", Name: "Grandchild", Status: iamdomain.EntityActive})
	require.NoError(t, err)
	scoped, err := repo.CreateRole(ctx, "tenant", "Scoped", "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(ctx, "tenant", scoped.ID, "merchant_view")
	require.NoError(t, err)
	_, _, err = repo.PutEntityRoleBinding(ctx, "tenant", parent.ID, scoped.ID, "principal", iamdomain.BindingAllow, time.Time{})
	require.NoError(t, err)

	for _, entityID := range []string{parent.ID, child.ID, grandchild.ID} {
		allowed, err = authorizer.CheckEntity(ctx, "tenant", entityID, "merchant_view", "principal")
		require.NoError(t, err)
		assert.True(t, allowed, "recursive inheritance must reach %s", entityID)
	}

	// An explicit deny on the child wins over the inherited allow and does not
	// revoke the parent.
	_, _, err = repo.PutEntityRoleBinding(ctx, "tenant", child.ID, scoped.ID, "principal", iamdomain.BindingDeny, time.Time{})
	require.NoError(t, err)
	explanation, err := authorizer.ExplainEntity(ctx, "tenant", child.ID, "merchant_view", "principal")
	require.NoError(t, err)
	assert.False(t, explanation.Allowed)
	assert.Equal(t, "explicit_deny", explanation.Reason)
	allowed, err = authorizer.CheckEntity(ctx, "tenant", parent.ID, "merchant_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed, "denying a child must not revoke the parent")

	// Constraint compiles the same state into concrete identifiers.
	constraint, err := authorizer.Constraint(ctx, "tenant", "merchant_view", "principal")
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
	administrator, err := repo.CreateRole(ctx, "tenant", "Administrators", "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(ctx, "tenant", administrator.ID, "tenant_administer")
	require.NoError(t, err)
	_, err = repo.AddMember(ctx, "tenant", administrator.ID, "principal")
	require.NoError(t, err)
	constraint, err = authorizer.Constraint(ctx, "tenant", "merchant_view", "principal")
	require.NoError(t, err)
	assert.True(t, constraint.AllowAll)

	// Bulk evaluation must agree with the single-permission path.
	bulk, err := authorizer.CheckBulk(ctx, "tenant", []string{"store_view", "merchant_view", "user_delete"}, "principal")
	require.NoError(t, err)
	assert.True(t, bulk["store_view"])
	assert.True(t, bulk["merchant_view"])
	assert.True(t, bulk["user_delete"], "tenant_administer satisfies every tenant permission")

	// Disabling the membership revokes everything immediately, with no cache
	// to invalidate.
	_, err = repo.SetMemberStatus(ctx, "tenant", "principal", MemberDisabled)
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, "tenant", "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestAuthorizerPlatformAdministrationIsNotATenantGrant(t *testing.T) {
	repo := newIAMRepository(t)
	now := time.Now().UTC().UnixMilli()
	_, err := repo.db.ExecContext(t.Context(), `INSERT INTO iam_principals
		(id, login_name, email, display_name, status, created_at, updated_at, disabled_at)
		VALUES (?, ?, '', ?, 'active', ?, ?, 0)`, "platform-admin", "platform-admin", "Platform Admin", now, now)
	require.NoError(t, err)
	changed, err := repo.GrantPlatformAdministrator(t.Context(), "platform-admin")
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = repo.GrantPlatformAdministrator(t.Context(), "platform-admin")
	require.NoError(t, err)
	assert.False(t, changed)
	authorizer := NewAuthorizer(repo.db)
	allowed, err := authorizer.CheckPlatform(t.Context(), "tenant_view", "platform-admin")
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, err = authorizer.Check(t.Context(), "tenant", "store_view", "platform-admin")
	require.NoError(t, err)
	assert.False(t, allowed, "platform administrators must still enter a tenant through active membership")

	putTestMember(t, repo, TenantMember{TenantID: "tenant", Subject: "platform-admin", DisplayName: "Platform Admin", Status: MemberActive})
	allowed, err = authorizer.Check(t.Context(), "tenant", "store_view", "platform-admin")
	require.NoError(t, err)
	assert.True(t, allowed)
	_, err = repo.db.ExecContext(t.Context(), "UPDATE iam_principals SET status = 'disabled' WHERE id = ?", "platform-admin")
	require.NoError(t, err)
	allowed, err = authorizer.CheckPlatform(t.Context(), "tenant_view", "platform-admin")
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestAuthorizerRejectsInvalidRequestsAndStorageFailure(t *testing.T) {
	require.Panics(t, func() { NewAuthorizer(nil) })
	repo := newIAMRepository(t)
	authorizer := NewAuthorizer(repo.db)

	allowed, err := authorizer.CheckBulk(t.Context(), "", nil, "")
	require.NoError(t, err)
	assert.Empty(t, allowed)
	_, err = authorizer.CheckBulk(t.Context(), "tenant", []string{""}, "principal")
	assert.Error(t, err)
	_, err = authorizer.Constraint(t.Context(), "tenant", "store_view", "")
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)
	_, err = authorizer.ExplainEntity(t.Context(), "tenant", strings.Repeat("e", 65), "store_view", "principal")
	assert.ErrorIs(t, err, iamdomain.ErrInvalidArgument)

	_, err = repo.db.ExecContext(t.Context(), "DROP TABLE iam_policy_revisions")
	require.NoError(t, err)
	_, err = authorizer.Constraint(t.Context(), "tenant", "store_view", "principal")
	assert.ErrorContains(t, err, "get IAM policy revision")
}

func TestAuthorizerConstraintAndExplanation(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	putTestMember(t, repo, TenantMember{TenantID: "tenant", Subject: "principal", DisplayName: "Principal", Status: MemberActive})
	var err error
	entities := []entityRowForTest{
		entityTestRow("tenant", "company", "", "company"),
		entityTestRow("tenant", "store-a", "company", "store"),
		entityTestRow("tenant", "store-b", "company", "store"),
		entityTestRow("tenant", "other", "", "store"),
		entityTestRow("other-tenant", "foreign", "", "store"),
	}
	_, err = repo.db.NewInsert().Model(&entities).Exec(ctx)
	require.NoError(t, err)
	allowRole := createTestRole(t, repo, "tenant", "Scoped viewer")
	denyRole := createTestRole(t, repo, "tenant", "Scoped denial")
	adminRole := createTestRole(t, repo, "tenant", "Tenant administrator")
	for _, role := range []Role{allowRole, denyRole} {
		_, err = repo.GrantPermission(ctx, "tenant", role.ID, "store_view")
		require.NoError(t, err)
	}
	_, err = repo.GrantPermission(ctx, "tenant", adminRole.ID, "tenant_administer")
	require.NoError(t, err)
	now := time.Now().UTC().UnixMilli()
	bindings := []roleBindingForTest{
		{TenantID: "tenant", RoleID: allowRole.ID, PrincipalID: "principal", ScopeType: "entity", ScopeID: "company", Effect: "allow", CreatedAt: now},
		{TenantID: "tenant", RoleID: denyRole.ID, PrincipalID: "principal", ScopeType: "entity", ScopeID: "store-b", Effect: "deny", CreatedAt: now},
	}
	_, err = repo.db.NewInsert().Model(&bindings).Exec(ctx)
	require.NoError(t, err)
	require.NoError(t, policyx.Advance(ctx, repo.db, repo.dialect, "tenant", now))

	authorizer := NewAuthorizer(repo.db)
	constraint, err := authorizer.Constraint(ctx, "tenant", "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, constraint.AllowAll)
	assert.Equal(t, []string{"company", "store-a"}, constraint.ResourceIDs)
	assert.Equal(t, []string{"store-b"}, constraint.DeniedIDs)
	assert.Equal(t, []string{"company"}, []string{constraint.Ancestors[0].ID})
	assert.EqualValues(t, 1, constraint.Revision)
	assert.NotNil(t, constraint.OwnerIDs)
	assert.NotNil(t, constraint.DepartmentIDs)

	allowed, err := authorizer.ExplainEntity(ctx, "tenant", "store-a", "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed.Allowed)
	assert.Equal(t, "permission_grant", allowed.Reason)
	require.Len(t, allowed.Matches, 1)
	assert.True(t, allowed.Matches[0].Inherited)

	denied, err := authorizer.ExplainEntity(ctx, "tenant", "store-b", "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, denied.Allowed)
	assert.Equal(t, "explicit_deny", denied.Reason)
	assert.Len(t, denied.Matches, 2)

	_, err = repo.AddMember(ctx, "tenant", adminRole.ID, "principal")
	require.NoError(t, err)
	constraint, err = authorizer.Constraint(ctx, "tenant", "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, constraint.AllowAll)
	assert.Empty(t, constraint.ResourceIDs)
	assert.Equal(t, []string{"store-b"}, constraint.DeniedIDs)
	allowed, err = authorizer.ExplainEntity(ctx, "tenant", "other", "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed.Allowed)
	assert.Equal(t, "administrator_grant", allowed.Reason)
	assert.Contains(t, allowed.Matches[0].SourceType, "role_member")

	_, err = repo.SetMemberStatus(ctx, "tenant", "principal", MemberDisabled)
	require.NoError(t, err)
	constraint, err = authorizer.Constraint(ctx, "tenant", "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, constraint.AllowAll)
	assert.Empty(t, constraint.ResourceIDs)
	explanation, err := authorizer.ExplainEntity(ctx, "tenant", "company", "store_view", "principal")
	require.NoError(t, err)
	assert.Equal(t, "inactive_membership", explanation.Reason)
	_, err = authorizer.ExplainEntity(ctx, "tenant", "missing", "store_view", "principal")
	assert.True(t, errors.Is(err, iamdomain.ErrEntityNotFound))
}

func TestAuthorizerAdministratorExpansion(t *testing.T) {
	repo := newIAMRepository(t)
	putTestMember(t, repo, TenantMember{TenantID: "tenant", Subject: "principal", DisplayName: "Principal", Status: MemberActive})
	var err error
	role := createTestRole(t, repo, "tenant", "Administrators")
	_, err = repo.GrantPermission(context.Background(), "tenant", role.ID, "tenant_administer")
	require.NoError(t, err)
	_, err = repo.AddMember(context.Background(), "tenant", role.ID, "principal")
	require.NoError(t, err)
	allowed, err := NewAuthorizer(repo.db).Check(context.Background(), "tenant", "menu_delete", "principal")
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, err = NewAuthorizer(repo.db).Check(context.Background(), "tenant", "platform_administer", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)
	_, err = repo.GrantPermission(context.Background(), "tenant", role.ID, "platform_administer")
	require.NoError(t, err)
	allowed, err = NewAuthorizer(repo.db).Check(context.Background(), "tenant", "platform_administer", "principal")
	require.NoError(t, err)
	assert.False(t, allowed, "persisted tenant grants must never confer platform authority")
}

func TestAuthorizerEntityInheritanceAndDeny(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	putTestMember(t, repo, TenantMember{TenantID: "tenant", Subject: "principal", DisplayName: "Principal", Status: MemberActive})
	var err error
	parent := entityTestRow("tenant", "company", "", "company")
	child := entityTestRow("tenant", "store", "company", "store")
	_, err = repo.db.NewInsert().Model(&[]entityRowForTest{parent, child}).Exec(ctx)
	require.NoError(t, err)
	allowRole := createTestRole(t, repo, "tenant", "Company viewer")
	denyRole := createTestRole(t, repo, "tenant", "Store deny")
	_, err = repo.GrantPermission(ctx, "tenant", allowRole.ID, "store_view")
	require.NoError(t, err)
	_, err = repo.GrantPermission(ctx, "tenant", denyRole.ID, "store_view")
	require.NoError(t, err)
	now := time.Now().UTC().UnixMilli()
	bindings := []roleBindingForTest{
		{TenantID: "tenant", RoleID: allowRole.ID, PrincipalID: "principal", ScopeType: "entity", ScopeID: "company", Effect: "allow", CreatedAt: now},
		{TenantID: "tenant", RoleID: denyRole.ID, PrincipalID: "principal", ScopeType: "entity", ScopeID: "store", Effect: "deny", CreatedAt: now},
	}
	_, err = repo.db.NewInsert().Model(&bindings).Exec(ctx)
	require.NoError(t, err)
	allowed, err := NewAuthorizer(repo.db).CheckEntity(ctx, "tenant", "store", "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)
	_, err = repo.db.NewDelete().Model((*roleBindingForTest)(nil)).Where("effect = 'deny'").Exec(ctx)
	require.NoError(t, err)
	allowed, err = NewAuthorizer(repo.db).CheckEntity(ctx, "tenant", "store", "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed)
	_, err = repo.SetMemberStatus(ctx, "tenant", "principal", MemberDisabled)
	require.NoError(t, err)
	allowed, err = NewAuthorizer(repo.db).CheckEntity(ctx, "tenant", "store", "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestAuthorizerScopedAdministratorDenyAndExpiredBinding(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	putTestMember(t, repo, TenantMember{TenantID: "tenant", Subject: "principal", DisplayName: "Principal", Status: MemberActive})
	var err error
	_, err = repo.db.NewInsert().Model(&[]entityRowForTest{
		entityTestRow("tenant", "company", "", "company"),
		entityTestRow("tenant", "store", "company", "store"),
	}).Exec(ctx)
	require.NoError(t, err)

	adminRole := createTestRole(t, repo, "tenant", "Scoped administrator")
	viewerRole := createTestRole(t, repo, "tenant", "Direct viewer")
	expiredRole := createTestRole(t, repo, "tenant", "Expired viewer")
	_, err = repo.GrantPermission(ctx, "tenant", adminRole.ID, "tenant_administer")
	require.NoError(t, err)
	for _, role := range []Role{viewerRole, expiredRole} {
		_, err = repo.GrantPermission(ctx, "tenant", role.ID, "store_view")
		require.NoError(t, err)
	}
	now := repo.now().UnixMilli()
	bindings := []roleBindingForTest{
		{TenantID: "tenant", RoleID: adminRole.ID, PrincipalID: "principal", ScopeType: "entity", ScopeID: "company", Effect: "allow", CreatedAt: now},
		{TenantID: "tenant", RoleID: adminRole.ID, PrincipalID: "principal", ScopeType: "entity", ScopeID: "store", Effect: "deny", CreatedAt: now},
		{TenantID: "tenant", RoleID: expiredRole.ID, PrincipalID: "principal", ScopeType: "entity", ScopeID: "store", Effect: "allow", ExpiresAt: now, CreatedAt: now},
	}
	_, err = repo.db.NewInsert().Model(&bindings).Exec(ctx)
	require.NoError(t, err)
	authorizer := NewAuthorizer(repo.db)
	authorizer.now = repo.now

	explanation, err := authorizer.ExplainEntity(ctx, "tenant", "store", "store_view", "principal")
	require.NoError(t, err)
	assert.False(t, explanation.Allowed)
	assert.Equal(t, "administrator_scope_denied", explanation.Reason)
	require.Len(t, explanation.Matches, 2)
	for _, match := range explanation.Matches {
		assert.NotEqual(t, expiredRole.ID, match.RoleID)
	}

	_, err = repo.db.NewInsert().Model(&roleBindingForTest{
		TenantID: "tenant", RoleID: viewerRole.ID, PrincipalID: "principal", ScopeType: "entity", ScopeID: "store", Effect: "allow", CreatedAt: now,
	}).Exec(ctx)
	require.NoError(t, err)
	explanation, err = authorizer.ExplainEntity(ctx, "tenant", "store", "store_view", "principal")
	require.NoError(t, err)
	assert.True(t, explanation.Allowed)
	assert.Equal(t, "permission_grant", explanation.Reason)
}

func TestAuthorizerAppliesRoleConditionsToEveryAssignmentSource(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	putTestMember(t, repo, TenantMember{TenantID: "tenant", Subject: "principal", DisplayName: "Principal", Email: "principal@example.test", Status: MemberActive})
	now := repo.now().UTC().UnixMilli()
	condition := json.RawMessage(`{"version":1,"gte":[{"context":"auth.acr"},{"value":2}]}`)
	type source struct {
		name string
		code string
		bind func(Role)
	}
	sources := []source{
		{name: "Direct", code: "user_view", bind: func(role Role) {
			_, err := repo.AddMember(ctx, "tenant", role.ID, "principal")
			require.NoError(t, err)
		}},
		{name: "Temporary", code: "role_view", bind: func(role Role) {
			_, err := repo.db.ExecContext(ctx, `INSERT INTO iam_temporary_role_grants
				(tenant_id,id,role_id,principal_id,source_type,source_id,starts_at,ends_at,created_by,created_at)
				VALUES ('tenant','temporary',?,'principal','access_request','request',?,?, 'approver',?)`, role.ID, now-1, now+time.Hour.Milliseconds(), now)
			require.NoError(t, err)
		}},
		{name: "Static group", code: "menu_view", bind: func(role Role) {
			_, err := repo.db.ExecContext(ctx, `INSERT INTO iam_groups
				(tenant_id,id,name,name_key,group_type,description,status,sort_order,version,created_at,updated_at)
				VALUES ('tenant','static','Static','static','static','','active',0,1,?,?)`, now, now)
			require.NoError(t, err)
			_, err = repo.db.ExecContext(ctx, `INSERT INTO iam_group_members
				(tenant_id,group_id,principal_id,starts_at,ends_at,created_at,updated_at)
				VALUES ('tenant','static','principal',0,0,?,?)`, now, now)
			require.NoError(t, err)
			_, err = repo.db.ExecContext(ctx, "INSERT INTO iam_group_role_bindings (tenant_id,role_id,group_id,created_at) VALUES ('tenant',?,'static',?)", role.ID, now)
			require.NoError(t, err)
		}},
		{name: "Dynamic group", code: "entity_view", bind: func(role Role) {
			rule := `{"version":1,"match":"all","conditions":[{"field":"member.status","operator":"in","values":["active"]}]}`
			_, err := repo.db.ExecContext(ctx, `INSERT INTO iam_groups
				(tenant_id,id,name,name_key,group_type,rule_json,description,status,sort_order,version,created_at,updated_at)
				VALUES ('tenant','dynamic','Dynamic','dynamic','dynamic',?,'','active',0,1,?,?)`, rule, now, now)
			require.NoError(t, err)
			_, err = repo.db.ExecContext(ctx, "INSERT INTO iam_group_role_bindings (tenant_id,role_id,group_id,created_at) VALUES ('tenant',?,'dynamic',?)", role.ID, now)
			require.NoError(t, err)
		}},
		{name: "Position", code: "store_view", bind: func(role Role) {
			_, err := repo.db.ExecContext(ctx, `INSERT INTO iam_positions
				(tenant_id,id,code,name,status,sort_order,version,created_at,updated_at)
				VALUES ('tenant','position','operator','Operator','active',0,1,?,?)`, now, now)
			require.NoError(t, err)
			_, err = repo.db.ExecContext(ctx, `INSERT INTO iam_position_members
				(tenant_id,position_id,principal_id,starts_at,ends_at,created_at,updated_at)
				VALUES ('tenant','position','principal',0,0,?,?)`, now, now)
			require.NoError(t, err)
			_, err = repo.db.ExecContext(ctx, "INSERT INTO iam_position_role_bindings (tenant_id,role_id,position_id,created_at) VALUES ('tenant',?,'position',?)", role.ID, now)
			require.NoError(t, err)
		}},
	}
	roles := make(map[string]Role, len(sources))
	codes := make([]string, 0, len(sources))
	for _, item := range sources {
		role := createTestRole(t, repo, "tenant", item.name)
		_, err := repo.GrantPermission(ctx, "tenant", role.ID, item.code)
		require.NoError(t, err)
		_, changed, err := repo.SetPermissionCondition(ctx, "tenant", role.ID, item.code, condition)
		require.NoError(t, err)
		assert.True(t, changed)
		item.bind(role)
		roles[item.code] = role
		codes = append(codes, item.code)
	}
	authorizer := NewAuthorizer(repo.db)
	authorizer.now = repo.now
	low := policyx.WithTrustedContext(ctx, policyx.TrustedContext{ACR: 1})
	allowed, err := authorizer.CheckBulk(low, "tenant", codes, "principal")
	require.NoError(t, err)
	for _, code := range codes {
		assert.False(t, allowed[code], code)
	}
	high := policyx.WithTrustedContext(ctx, policyx.TrustedContext{ACR: 2})
	allowed, err = authorizer.CheckBulk(high, "tenant", codes, "principal")
	require.NoError(t, err)
	for _, code := range codes {
		assert.True(t, allowed[code], code)
	}

	putTestMember(t, repo, TenantMember{TenantID: "tenant", Subject: "scoped", DisplayName: "Scoped", Status: MemberActive})
	entity := entityTestRow("tenant", "store", "", "store")
	_, err = repo.db.NewInsert().Model(&entity).Exec(ctx)
	require.NoError(t, err)
	scopedRole := createTestRole(t, repo, "tenant", "Scoped")
	_, err = repo.GrantPermission(ctx, "tenant", scopedRole.ID, "store_update")
	require.NoError(t, err)
	_, _, err = repo.SetPermissionCondition(ctx, "tenant", scopedRole.ID, "store_update", condition)
	require.NoError(t, err)
	_, err = repo.db.NewInsert().Model(&roleBindingForTest{TenantID: "tenant", RoleID: scopedRole.ID, PrincipalID: "scoped", ScopeType: "entity", ScopeID: "store", Effect: "allow", CreatedAt: now}).Exec(ctx)
	require.NoError(t, err)
	allowedEntity, err := authorizer.CheckEntity(low, "tenant", "store", "store_update", "scoped")
	require.NoError(t, err)
	assert.False(t, allowedEntity)
	allowedEntity, err = authorizer.CheckEntity(high, "tenant", "store", "store_update", "scoped")
	require.NoError(t, err)
	assert.True(t, allowedEntity)

	_, err = repo.db.NewUpdate().Table("iam_role_permissions").Set("condition_json = ?", "{broken").
		Where("tenant_id = ? AND role_id = ? AND permission_code = ?", "tenant", roles["user_view"].ID, "user_view").Exec(ctx)
	require.NoError(t, err)
	allowed, err = authorizer.CheckBulk(high, "tenant", codes, "principal")
	assert.ErrorContains(t, err, "evaluate role permission condition")
	assert.Nil(t, allowed)
}

func TestAuthorizerResourceAttributeABAC(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	putTestMember(t, repo, TenantMember{TenantID: "tenant", Subject: "principal", DisplayName: "Principal", Status: MemberActive})
	role := createTestRole(t, repo, "tenant", "Order Operators")
	_, err := repo.GrantPermission(ctx, "tenant", role.ID, "order_view")
	require.NoError(t, err)
	condition, err := policyx.CanonicalCondition(json.RawMessage(`{"version":1,"all":[
		{"eq":[{"context":"resource.type"},{"value":"order"}]},
		{"eq":[{"context":"resource.owner"},{"value":"principal"}]},
		{"in":[{"context":"resource.attr.region"},{"value":["cn-east"]}]}
	]}`))
	require.NoError(t, err)
	_, changed, err := repo.SetPermissionCondition(ctx, "tenant", role.ID, "order_view", condition)
	require.NoError(t, err)
	assert.True(t, changed)

	entity := entityTestRow("tenant", "store", "", "store")
	_, err = repo.db.NewInsert().Model(&entity).Exec(ctx)
	require.NoError(t, err)
	now := time.Now().UTC().UnixMilli()
	_, err = repo.db.NewInsert().Model(&roleBindingForTest{TenantID: "tenant", RoleID: role.ID, PrincipalID: "principal", ScopeType: "entity", ScopeID: "store", Effect: "allow", CreatedAt: now}).Exec(ctx)
	require.NoError(t, err)

	authorizer := NewAuthorizer(repo.db)
	authorizer.now = repo.now

	// Without server-provided resource facts the condition fails closed.
	allowed, err := authorizer.CheckResource(ctx, "tenant", "store", "order", "order-1", "order_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)

	// Type and id come from the check arguments; owner and attributes come
	// from the server-side resource context.
	resourceCtx := policyx.WithResourceContext(ctx, policyx.ResourceContext{Owner: "principal", Attrs: map[string]string{"region": "cn-east"}})
	allowed, err = authorizer.CheckResource(resourceCtx, "tenant", "store", "order", "order-1", "order_view", "principal")
	require.NoError(t, err)
	assert.True(t, allowed)

	wrong := policyx.WithResourceContext(ctx, policyx.ResourceContext{Owner: "principal", Attrs: map[string]string{"region": "us-west"}})
	allowed, err = authorizer.CheckResource(wrong, "tenant", "store", "order", "order-1", "order_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)

	// The same condition never leaks to a different resource type.
	other := policyx.WithResourceContext(ctx, policyx.ResourceContext{Owner: "principal", Attrs: map[string]string{"region": "cn-east"}})
	allowed, err = authorizer.CheckResource(other, "tenant", "store", "invoice", "invoice-1", "order_view", "principal")
	require.NoError(t, err)
	assert.False(t, allowed)
}

type entityRowForTest struct {
	bun.BaseModel `bun:"table:iam_entities"`
	TenantID      string  `bun:"tenant_id"`
	ID            string  `bun:"id"`
	ParentID      *string `bun:"parent_id"`
	Type          string  `bun:"type"`
	Name          string  `bun:"name"`
	Status        string  `bun:"status"`
	Metadata      string  `bun:"metadata"`
	CreatedAt     int64   `bun:"created_at"`
	UpdatedAt     int64   `bun:"updated_at"`
}

func entityTestRow(tenantID, id, parentID, entityType string) entityRowForTest {
	var parent *string
	if parentID != "" {
		parent = &parentID
	}
	return entityRowForTest{TenantID: tenantID, ID: id, ParentID: parent, Type: entityType, Name: id, Status: "active", Metadata: "{}", CreatedAt: 1, UpdatedAt: 1}
}

type roleBindingForTest struct {
	bun.BaseModel `bun:"table:iam_role_bindings"`
	TenantID      string `bun:"tenant_id"`
	RoleID        string `bun:"role_id"`
	PrincipalID   string `bun:"principal_id"`
	ScopeType     string `bun:"scope_type"`
	ScopeID       string `bun:"scope_id"`
	Effect        string `bun:"effect"`
	ExpiresAt     int64  `bun:"expires_at"`
	CreatedAt     int64  `bun:"created_at"`
}
