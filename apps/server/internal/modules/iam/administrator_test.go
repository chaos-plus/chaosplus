package iam

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestAdministratorGuardPreservesDirectAdministrator(t *testing.T) {
	repo := newIAMRepository(t)
	guard := NewAdministratorGuard()
	seedAdministratorPrincipal(t, repo, "tenant", "principal-a")
	role := createTestRole(t, repo, "tenant", "Administrators")
	_, err := repo.GrantPermission(t.Context(), "tenant", role.ID, "tenant_administer")
	require.NoError(t, err)
	_, err = repo.AddMember(t.Context(), "tenant", role.ID, "principal-a")
	require.NoError(t, err)

	err = repo.db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		verify, err := guard.Protect(ctx, tx, "sqlite", "tenant")
		if err != nil {
			return err
		}
		if _, err := repo.withExecutor(tx).RemoveMember(ctx, "tenant", role.ID, "principal-a"); err != nil {
			return err
		}
		return verify()
	})
	assert.ErrorIs(t, err, ErrLastTenantAdministrator)
	members, err := repo.ListMembers(t.Context(), "tenant", role.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"principal-a"}, members)

	seedAdministratorPrincipal(t, repo, "tenant", "principal-b")
	_, err = repo.AddMember(t.Context(), "tenant", role.ID, "principal-b")
	require.NoError(t, err)
	err = repo.db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		verify, err := guard.Protect(ctx, tx, "sqlite", "tenant")
		if err != nil {
			return err
		}
		if _, err := repo.withExecutor(tx).RemoveMember(ctx, "tenant", role.ID, "principal-a"); err != nil {
			return err
		}
		return verify()
	})
	require.NoError(t, err)
	members, err = repo.ListMembers(t.Context(), "tenant", role.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"principal-b"}, members)
}

func TestAdministratorGuardRemovesExactReviewedMembership(t *testing.T) {
	repo := newIAMRepository(t)
	guard := NewAdministratorGuard()
	seedAdministratorPrincipal(t, repo, "tenant", "principal-a")
	seedAdministratorPrincipal(t, repo, "tenant", "principal-b")
	role := createTestRole(t, repo, "tenant", "Operators")
	_, err := repo.AddMember(t.Context(), "tenant", role.ID, "principal-a")
	require.NoError(t, err)
	var createdAt int64
	require.NoError(t, repo.db.NewSelect().Table("iam_role_members").Column("created_at").
		Where("tenant_id = 'tenant' AND role_id = ? AND user_subject = 'principal-a'", role.ID).Scan(t.Context(), &createdAt))

	changed, err := guard.RemoveRoleMember(t.Context(), repo.db, "sqlite", "tenant", role.ID, "principal-a", createdAt+1)
	require.NoError(t, err)
	assert.False(t, changed)
	changed, err = guard.RemoveRoleMember(t.Context(), repo.db, "sqlite", "tenant", role.ID, "principal-a", createdAt)
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = guard.RemoveRoleMember(t.Context(), nil, "sqlite", "tenant", role.ID, "principal-a", createdAt)
	assert.Error(t, err)
	assert.False(t, changed)
}

func TestAdministratorGuardRequiresDurableDirectoryAssignment(t *testing.T) {
	repo := newIAMRepository(t)
	guard := NewAdministratorGuard()
	guard.now = func() time.Time { return time.UnixMilli(1_700_000_000_000).UTC() }
	seedAdministratorPrincipal(t, repo, "tenant", "group-admin")
	seedAdministratorPrincipal(t, repo, "tenant", "expiring-position-admin")
	role := createTestRole(t, repo, "tenant", "Administrators")
	_, err := repo.GrantPermission(t.Context(), "tenant", role.ID, "platform_administer")
	require.NoError(t, err)
	now := guard.now().UnixMilli()
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_groups
		(tenant_id,id,name,name_key,group_type,description,status,sort_order,version,created_at,updated_at)
		VALUES ('tenant','group','Administrators','administrators','static','','active',0,1,?,?)`, now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_group_members
		(tenant_id,group_id,principal_id,starts_at,ends_at,created_at,updated_at)
		VALUES ('tenant','group','group-admin',0,0,?,?)`, now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_group_role_bindings
		(tenant_id,role_id,group_id,created_at) VALUES ('tenant',?,'group',?)`, role.ID, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_positions
		(tenant_id,id,code,name,status,sort_order,version,created_at,updated_at)
		VALUES ('tenant','position','administrator','Administrator','active',0,1,?,?)`, now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_position_members
		(tenant_id,position_id,principal_id,starts_at,ends_at,created_at,updated_at)
		VALUES ('tenant','position','expiring-position-admin',0,?, ?,?)`, now+time.Hour.Milliseconds(), now, now)
	require.NoError(t, err)
	_, err = repo.db.ExecContext(t.Context(), `INSERT INTO iam_position_role_bindings
		(tenant_id,role_id,position_id,created_at) VALUES ('tenant',?,'position',?)`, role.ID, now)
	require.NoError(t, err)

	err = repo.db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		verify, err := guard.Protect(ctx, tx, "sqlite", "tenant")
		if err != nil {
			return err
		}
		if _, err := tx.NewDelete().Table("iam_group_role_bindings").Where("tenant_id = 'tenant' AND group_id = 'group'").Exec(ctx); err != nil {
			return err
		}
		return verify()
	})
	assert.ErrorIs(t, err, ErrLastTenantAdministrator)

	_, err = repo.db.NewUpdate().Table("iam_position_members").Set("ends_at = 0").Where("tenant_id = 'tenant' AND position_id = 'position'").Exec(t.Context())
	require.NoError(t, err)
	err = repo.db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		verify, err := guard.Protect(ctx, tx, "sqlite", "tenant")
		if err != nil {
			return err
		}
		if _, err := tx.NewDelete().Table("iam_group_role_bindings").Where("tenant_id = 'tenant' AND group_id = 'group'").Exec(ctx); err != nil {
			return err
		}
		return verify()
	})
	require.NoError(t, err)

	_, err = guard.Protect(t.Context(), nil, "sqlite", "tenant")
	assert.Error(t, err)
	_, err = guard.Protect(t.Context(), repo.db, "unknown", "tenant")
	assert.Error(t, err)
	var nilGuard *AdministratorGuard
	_, err = nilGuard.Protect(t.Context(), repo.db, "sqlite", "tenant")
	assert.Error(t, err)
}

func TestServiceRejectsLastAdministratorMutations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(context.Context, *Service, string) error
	}{
		{name: "remove role member", mutate: func(ctx context.Context, service *Service, roleID string) error {
			_, err := service.RemoveMember(ctx, "tenant", roleID, "administrator")
			return err
		}},
		{name: "revoke permission", mutate: func(ctx context.Context, service *Service, roleID string) error {
			_, err := service.RevokePermission(ctx, "tenant", roleID, "tenant_administer")
			return err
		}},
		{name: "condition permission", mutate: func(ctx context.Context, service *Service, roleID string) error {
			_, _, err := service.SetPermissionCondition(ctx, "tenant", roleID, "tenant_administer", json.RawMessage(`{"version":1,"gte":[{"context":"auth.acr"},{"value":2}]}`))
			return err
		}},
		{name: "delete role", mutate: func(ctx context.Context, service *Service, roleID string) error {
			return service.DeleteRole(ctx, "tenant", roleID)
		}},
		{name: "disable membership with put", mutate: func(ctx context.Context, service *Service, _ string) error {
			_, err := service.PutTenantMember(ctx, "tenant", "administrator", "administrator", "", "", MemberDisabled)
			return err
		}},
		{name: "disable membership status", mutate: func(ctx context.Context, service *Service, _ string) error {
			_, err := service.SetTenantMemberStatus(ctx, "tenant", "administrator", MemberDisabled)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newTestService(t)
			seedAdministratorPrincipal(t, service.repo, "tenant", "administrator")
			role, err := service.CreateRole(t.Context(), "tenant", "Administrators", "")
			require.NoError(t, err)
			_, err = service.GrantPermission(t.Context(), "tenant", role.ID, "tenant_administer")
			require.NoError(t, err)
			_, err = service.AddMember(t.Context(), "tenant", role.ID, "administrator")
			require.NoError(t, err)

			err = test.mutate(t.Context(), service, role.ID)
			assert.ErrorIs(t, err, ErrLastTenantAdministrator)
			allowed, checkErr := service.checker.CheckBulk(t.Context(), "tenant", []string{"tenant_administer"}, "administrator")
			require.NoError(t, checkErr)
			assert.True(t, allowed["tenant_administer"])
		})
	}
}

func TestServiceRejectsLastDirectoryAdministratorRemoval(t *testing.T) {
	service := newTestService(t)
	seedAdministratorPrincipal(t, service.repo, "tenant", "administrator")
	role, err := service.CreateRole(t.Context(), "tenant", "Administrators", "")
	require.NoError(t, err)
	_, err = service.GrantPermission(t.Context(), "tenant", role.ID, "tenant_administer")
	require.NoError(t, err)
	now := service.repo.now().UTC().UnixMilli()
	_, err = service.repo.db.ExecContext(t.Context(), `INSERT INTO iam_groups
		(tenant_id,id,name,name_key,group_type,description,status,sort_order,version,created_at,updated_at)
		VALUES ('tenant','administrators','Administrators','administrators','static','','active',0,1,?,?)`, now, now)
	require.NoError(t, err)
	_, err = service.repo.db.ExecContext(t.Context(), `INSERT INTO iam_group_members
		(tenant_id,group_id,principal_id,starts_at,ends_at,created_at,updated_at)
		VALUES ('tenant','administrators','administrator',0,0,?,?)`, now, now)
	require.NoError(t, err)
	_, err = service.AddDirectoryBinding(t.Context(), "tenant", role.ID, DirectoryAssigneeGroup, "administrators")
	require.NoError(t, err)

	_, err = service.RemoveDirectoryBinding(t.Context(), "tenant", role.ID, DirectoryAssigneeGroup, "administrators")
	assert.ErrorIs(t, err, ErrLastTenantAdministrator)
	bindings, listErr := service.ListDirectoryBindings(t.Context(), "tenant", role.ID)
	require.NoError(t, listErr)
	assert.Len(t, bindings, 1)
}

func TestConcurrentAdministratorRemovalLeavesOneAdministrator(t *testing.T) {
	service := newTestService(t)
	role, err := service.CreateRole(t.Context(), "tenant", "Administrators", "")
	require.NoError(t, err)
	_, err = service.GrantPermission(t.Context(), "tenant", role.ID, "tenant_administer")
	require.NoError(t, err)
	for _, principalID := range []string{"administrator-a", "administrator-b"} {
		seedAdministratorPrincipal(t, service.repo, "tenant", principalID)
		_, err = service.AddMember(t.Context(), "tenant", role.ID, principalID)
		require.NoError(t, err)
	}

	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, principalID := range []string{"administrator-a", "administrator-b"} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, removeErr := service.RemoveMember(t.Context(), "tenant", role.ID, principalID)
			results <- removeErr
		}()
	}
	workers.Wait()
	close(results)
	succeeded, rejected := 0, 0
	for result := range results {
		switch {
		case result == nil:
			succeeded++
		case assert.ErrorIs(t, result, ErrLastTenantAdministrator):
			rejected++
		}
	}
	assert.Equal(t, 1, succeeded)
	assert.Equal(t, 1, rejected)
	members, err := service.ListMembers(t.Context(), "tenant", role.ID)
	require.NoError(t, err)
	assert.Len(t, members, 1)
}

func seedAdministratorPrincipal(t *testing.T, repo *Repository, tenantID, principalID string) {
	t.Helper()
	now := repo.now().UTC().UnixMilli()
	_, err := repo.db.ExecContext(t.Context(), `INSERT INTO iam_principals
		(id,login_name,email,display_name,status,created_at,updated_at,disabled_at)
		VALUES (?,?,?,?,'active',?,?,0)`, principalID, principalID, principalID+"@example.test", principalID, now, now)
	require.NoError(t, err)
	putTestMember(t, repo, TenantMember{TenantID: tenantID, Subject: principalID, DisplayName: principalID, Status: MemberActive})
}
