package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/governance"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// seedGuardTenant builds a tenant whose only administrator is reachable through
// the returned role, which is what the access-review guards must protect.
func seedGuardTenant(t *testing.T, db *bun.DB) (*iam.Repository, iam.Role) {
	t.Helper()
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, testID("tenant")))
	repo := iam.NewRepository(db, newTestIDGenerator())
	// The guard only counts administrators that resolve to an active principal,
	// so the principal rows are part of a realistic tenant.
	for _, principalID := range []guid.ID{testID("root"), testID("backup")} {
		_, err := db.ExecContext(t.Context(), `INSERT INTO iam_principals
			(id, login_name, email, display_name, status, created_at, updated_at, disabled_at)
			VALUES (?, ?, ?, ?, 'active', ?, ?, 0)`,
			principalID, principalID, principalID.String()+"@example.test", principalID.String(), 1_700_000_000_000, 1_700_000_000_000)
		require.NoError(t, err)
	}
	_, err := repo.PutMember(t.Context(), iam.TenantMember{TenantID: testID("tenant"), PrincipalID: testID("root"), DisplayName: "Root", Status: iam.MemberActive})
	require.NoError(t, err)
	role, err := repo.CreateRole(t.Context(), testID("tenant"), "Administrators", "")
	require.NoError(t, err)
	_, err = repo.GrantPermission(t.Context(), testID("tenant"), role.ID, "tenant_administer")
	require.NoError(t, err)
	_, err = repo.AddMember(t.Context(), testID("tenant"), role.ID, testID("root"))
	require.NoError(t, err)
	return repo, role
}

func TestGuardGroupPositionRemoveProtectsLastAdministrator(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo, role := seedGuardTenant(t, db)

	// A removal that changes nothing is passed straight through.
	noop := guardGroupPositionRemove(iam.NewAdministratorGuard(), "sqlite",
		func(context.Context, bun.IDB, guid.ID, guid.ID, guid.ID) (bool, error) { return false, nil })
	changed, err := noop(t.Context(), db, testID("tenant"), testID("group"), testID("root"))
	require.NoError(t, err)
	assert.False(t, changed)

	// A removal that reports an error is propagated verbatim.
	failing := guardGroupPositionRemove(iam.NewAdministratorGuard(), "sqlite",
		func(context.Context, bun.IDB, guid.ID, guid.ID, guid.ID) (bool, error) {
			return false, errors.New("removal exploded")
		})
	_, err = failing(t.Context(), db, testID("tenant"), testID("group"), testID("root"))
	assert.ErrorContains(t, err, "removal exploded")

	// Removing the only path to tenant_administer is converted into the
	// review-specific refusal instead of silently stranding the tenant.
	stripping := guardGroupPositionRemove(iam.NewAdministratorGuard(), "sqlite",
		func(ctx context.Context, executor bun.IDB, tenantID, _, principalID guid.ID) (bool, error) {
			_, removeErr := executor.NewDelete().Table("iam_role_members").
				Where("tenant_id = ? AND principal_id = ?", tenantID, principalID).Exec(ctx)
			return true, removeErr
		})
	_, err = stripping(t.Context(), db, testID("tenant"), testID("group"), testID("root"))
	assert.ErrorIs(t, err, governance.ErrReviewLastAdministrator)

	// With a second administrator present the removal is allowed to complete.
	_, err = repo.PutMember(t.Context(), iam.TenantMember{TenantID: testID("tenant"), PrincipalID: testID("backup"), DisplayName: "Backup", Status: iam.MemberActive})
	require.NoError(t, err)
	_, err = repo.AddMember(t.Context(), testID("tenant"), role.ID, testID("backup"))
	require.NoError(t, err)
	_, err = repo.AddMember(t.Context(), testID("tenant"), role.ID, testID("root"))
	require.NoError(t, err)
	changed, err = stripping(t.Context(), db, testID("tenant"), testID("group"), testID("root"))
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestGuardEntityRoleRemoveProtectsLastAdministrator(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	seedGuardTenant(t, db)

	noop := guardEntityRoleRemove(iam.NewAdministratorGuard(), "sqlite",
		func(context.Context, bun.IDB, guid.ID, guid.ID, guid.ID, guid.ID) (bool, error) { return false, nil })
	changed, err := noop(t.Context(), db, testID("tenant"), testID("entity"), testID("role"), testID("root"))
	require.NoError(t, err)
	assert.False(t, changed)

	failing := guardEntityRoleRemove(iam.NewAdministratorGuard(), "sqlite",
		func(context.Context, bun.IDB, guid.ID, guid.ID, guid.ID, guid.ID) (bool, error) {
			return false, errors.New("binding removal exploded")
		})
	_, err = failing(t.Context(), db, testID("tenant"), testID("entity"), testID("role"), testID("root"))
	assert.ErrorContains(t, err, "binding removal exploded")

	stripping := guardEntityRoleRemove(iam.NewAdministratorGuard(), "sqlite",
		func(ctx context.Context, executor bun.IDB, tenantID, _, _, principalID guid.ID) (bool, error) {
			_, removeErr := executor.NewDelete().Table("iam_role_members").
				Where("tenant_id = ? AND principal_id = ?", tenantID, principalID).Exec(ctx)
			return true, removeErr
		})
	_, err = stripping(t.Context(), db, testID("tenant"), testID("entity"), testID("role"), testID("root"))
	assert.ErrorIs(t, err, governance.ErrReviewLastAdministrator)
}

func TestAuditAppenderUsesCallerDatabase(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))

	appendAudit := auditAppender(audit.NewService(db, newTestIDGenerator()))
	event := auditx.NewEvent(t.Context(), testID("tenant"), "tested", "module", testID("modules"))
	require.NoError(t, appendAudit(t.Context(), db, event))

	var count int
	require.NoError(t, db.NewSelect().Table("iam_audit_events").ColumnExpr("COUNT(*)").Where("tenant_id = ? AND event_type = ?", testID("tenant"), "tested").Scan(t.Context(), &count))
	require.Equal(t, 1, count)
}

func TestRegistrationPrincipalCreatorMapsIdentityErrors(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))

	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	id, err := registrationPrincipalCreator(t.Context(), db, "user@example.com", "password-hash", "User", now, newTestIDGenerator())
	require.NoError(t, err)
	require.NotEmpty(t, id)

	_, err = registrationPrincipalCreator(t.Context(), db, "user@example.com", "password-hash", "User", now, newTestIDGenerator())
	require.ErrorIs(t, err, authnext.ErrRegistrationConflict)
	_, err = registrationPrincipalCreator(t.Context(), db, "invalid", "password-hash", "User", now, newTestIDGenerator())
	require.ErrorIs(t, err, authnext.ErrInvalidRegistration)
}
