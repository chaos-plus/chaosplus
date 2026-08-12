package iam

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDynamicGroupAuthorizationAndRelationshipRoots(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := t.Context()
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("alice"), DisplayName: "Alice", Email: "alice@example.com", Status: MemberActive})
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("bob"), DisplayName: "Bob", Email: "bob@example.net", Status: MemberActive})
	rule := `{"version":1,"match":"all","conditions":[{"field":"member.email_domain","operator":"in","values":["example.com"]}]}`
	now := repo.now().UTC().UnixMilli()
	_, err := repo.db.ExecContext(ctx, `INSERT INTO iam_groups
		(tenant_id,id,name,name_key,group_type,rule_json,description,status,sort_order,version,created_at,updated_at)
		VALUES (?,?,?,'dynamic','dynamic',?,'','active',0,1,?,?)`, testID("tenant"), testID("dynamic"), "Dynamic", rule, now, now)
	require.NoError(t, err)
	role := createTestRole(t, repo, testID("tenant"), "Dynamic viewers")
	_, err = repo.GrantPermission(ctx, testID("tenant"), role.ID, "store_view")
	require.NoError(t, err)
	_, err = repo.db.ExecContext(ctx, "INSERT INTO iam_group_role_bindings (tenant_id,role_id,group_id,created_at) VALUES (?,?,?,?)", testID("tenant"), role.ID, testID("dynamic"), now)
	require.NoError(t, err)
	entity := entityTestRow(testID("tenant"), testID("store"), 0, "store")
	_, err = repo.db.NewInsert().Model(&entity).Exec(ctx)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(ctx, `INSERT INTO iam_relationships
		(tenant_id,subject_type,subject_id,subject_relation,relation,resource_type,resource_id,created_at)
		VALUES (?,?,?, 'member', 'viewer', ?, ?, ?)`, testID("tenant"), "group", testID("dynamic"), "store", testID("store"), now)
	require.NoError(t, err)

	authorizer := NewAuthorizer(repo.db)
	for _, check := range []struct {
		subject guid.ID
		allowed bool
	}{{testID("alice"), true}, {testID("bob"), false}} {
		allowed, checkErr := authorizer.Check(ctx, testID("tenant"), "store_view", check.subject)
		require.NoError(t, checkErr)
		assert.Equal(t, check.allowed, allowed)
		allowed, checkErr = authorizer.CheckEntity(ctx, testID("tenant"), testID("store"), "store_view", check.subject)
		require.NoError(t, checkErr)
		assert.Equal(t, check.allowed, allowed)
	}

	_, err = repo.db.ExecContext(ctx, "UPDATE iam_tenant_members SET email = ? WHERE tenant_id = ? AND principal_id = ?", testID("alice@example.net"), testID("tenant"), testID("alice"))
	require.NoError(t, err)
	allowed, err := authorizer.Check(ctx, testID("tenant"), "store_view", testID("alice"))
	require.NoError(t, err)
	assert.False(t, allowed)

	_, err = repo.db.ExecContext(ctx, "UPDATE iam_groups SET rule_json = '{broken' WHERE tenant_id = ? AND id = ?", testID("tenant"), testID("dynamic"))
	require.NoError(t, err)
	allowed, err = authorizer.Check(ctx, testID("tenant"), "store_view", testID("alice"))
	assert.False(t, allowed)
	assert.ErrorContains(t, err, "evaluate dynamic group")
}

func TestMatchingDynamicGroupsRejectsInactiveMemberAndStorageFailure(t *testing.T) {
	repo := newIAMRepository(t)
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("disabled"), DisplayName: "Disabled", Status: MemberDisabled})
	ids, err := matchingDynamicGroupIDs(t.Context(), repo.db, testID("tenant"), testID("disabled"))
	require.NoError(t, err)
	assert.Empty(t, ids)
	ids, err = matchingDynamicGroupIDs(t.Context(), repo.db, testID("tenant"), testID("missing"))
	require.NoError(t, err)
	assert.NotNil(t, ids)
	assert.Empty(t, ids)

	require.NoError(t, repo.db.Close())
	_, err = matchingDynamicGroupIDs(context.Background(), repo.db, testID("tenant"), testID("disabled"))
	assert.ErrorContains(t, err, "dynamic group candidates")
}
