package organization

import (
	"sync/atomic"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestTenantLifecycle(t *testing.T) {
	db, service := newTenantService(t)
	tenant, err := service.Create(t.Context(), CreateTenant{Slug: "acme", Name: "Acme"})
	require.NoError(t, err)
	assert.Equal(t, TenantActive, tenant.Status)
	assert.EqualValues(t, 1, tenant.Version)

	items, err := service.List(t.Context(), false)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, tenant.ID, items[0].ID)

	name, suspended := "Acme Holdings", TenantSuspended
	tenant, err = service.Update(t.Context(), tenant.ID, UpdateTenant{Name: &name, Status: &suspended, Version: tenant.Version})
	require.NoError(t, err)
	assert.Equal(t, name, tenant.Name)
	assert.Equal(t, TenantSuspended, tenant.Status)
	assert.EqualValues(t, 2, tenant.Version)

	_, err = service.Update(t.Context(), tenant.ID, UpdateTenant{Name: &name, Version: 1})
	assert.ErrorIs(t, err, ErrTenantVersionConflict)
	active := TenantActive
	tenant, err = service.Update(t.Context(), tenant.ID, UpdateTenant{Status: &active, Version: tenant.Version})
	require.NoError(t, err)
	require.NoError(t, service.Delete(t.Context(), tenant.ID, tenant.Version))

	items, err = service.List(t.Context(), false)
	require.NoError(t, err)
	assert.Empty(t, items)
	items, err = service.List(t.Context(), true)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, TenantDeleted, items[0].Status)

	revision, err := policyx.Current(t.Context(), db, tenant.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 4, revision)
}

func TestTenantValidationConflictsAndAuditRollback(t *testing.T) {
	db, service := newTenantService(t)
	_, err := service.Create(t.Context(), CreateTenant{Slug: "acme", Name: "Acme"})
	require.NoError(t, err)
	_, err = service.Create(t.Context(), CreateTenant{Slug: "acme", Name: "Other"})
	assert.ErrorIs(t, err, ErrTenantSlugConflict)
	_, err = service.Create(t.Context(), CreateTenant{Slug: "Bad Slug", Name: "Other"})
	assert.ErrorIs(t, err, ErrInvalidTenant)

	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER reject_tenant_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'tenant_created' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)
	_, err = service.Create(t.Context(), CreateTenant{Slug: "rollback", Name: "Rollback"})
	assert.Error(t, err)
	count, countErr := db.NewSelect().Table("iam_tenants").Where("slug = ?", "rollback").Count(t.Context())
	require.NoError(t, countErr)
	assert.Zero(t, count)
}

func TestTenantUpdateDeleteValidationAndAuditRollback(t *testing.T) {
	db, service := newTenantService(t)
	tenant, err := service.Create(t.Context(), CreateTenant{Slug: "protected", Name: "Protected"})
	require.NoError(t, err)

	for _, input := range []UpdateTenant{
		{},
		{Name: stringPointer(" "), Version: tenant.Version},
		{Status: stringPointer("unknown"), Version: tenant.Version},
	} {
		_, err = service.Update(t.Context(), tenant.ID, input)
		assert.ErrorIs(t, err, ErrInvalidTenant)
	}
	_, err = service.Update(t.Context(), 0, UpdateTenant{Name: stringPointer("Name"), Version: 1})
	assert.ErrorIs(t, err, ErrInvalidTenant)
	_, err = service.Update(t.Context(), testID("missing"), UpdateTenant{Name: stringPointer("Name"), Version: 1})
	assert.ErrorIs(t, err, ErrTenantNotFound)
	assert.ErrorIs(t, service.Delete(t.Context(), 0, 1), ErrInvalidTenant)
	assert.ErrorIs(t, service.Delete(t.Context(), testID("missing"), 1), ErrTenantNotFound)

	unchanged, err := service.Update(t.Context(), tenant.ID, UpdateTenant{Name: &tenant.Name, Version: tenant.Version})
	require.NoError(t, err)
	assert.Equal(t, tenant.Version, unchanged.Version)

	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER reject_tenant_update_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'tenant_updated' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)
	suspended := TenantSuspended
	_, err = service.Update(t.Context(), tenant.ID, UpdateTenant{Status: &suspended, Version: tenant.Version})
	assert.Error(t, err)
	stored, getErr := service.Get(t.Context(), tenant.ID)
	require.NoError(t, getErr)
	assert.Equal(t, TenantActive, stored.Status)
	assert.Equal(t, tenant.Version, stored.Version)
	_, err = db.ExecContext(t.Context(), "DROP TRIGGER reject_tenant_update_audit")
	require.NoError(t, err)

	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER reject_tenant_delete_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'tenant_deleted' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)
	assert.Error(t, service.Delete(t.Context(), tenant.ID, tenant.Version))
	stored, getErr = service.Get(t.Context(), tenant.ID)
	require.NoError(t, getErr)
	assert.Equal(t, TenantActive, stored.Status)
	assert.Equal(t, tenant.Version, stored.Version)
}

func TestTenantStorageFailures(t *testing.T) {
	db, service := newTenantService(t)
	_, healthy := newTenantService(t)
	require.NoError(t, db.Close())
	assert.Error(t, EnsureTenant(t.Context(), db, testID("tenant")))
	_, err := service.Get(t.Context(), testID("tenant"))
	assert.Error(t, err)
	_, err = service.Create(t.Context(), CreateTenant{Slug: "tenant", Name: "Tenant"})
	assert.Error(t, err)
	assert.Error(t, service.Delete(t.Context(), testID("tenant"), 1))
	assert.Error(t, EnsureTenant(t.Context(), nil, testID("tenant")))
	assert.ErrorIs(t, EnsureTenant(t.Context(), db, 0), ErrInvalidTenant)
	assert.Panics(t, func() { NewTenantService(nil, healthy.audit, healthy.nextID) })
	assert.Panics(t, func() { NewTenantService(healthy.repo.db, nil, healthy.nextID) })
	assert.Panics(t, func() { NewTenantService(healthy.repo.db, healthy.audit, nil) })
}

func TestEnsureTenantDerivesStableValidSlugs(t *testing.T) {
	db, _ := newTenantService(t)
	for _, id := range []guid.ID{testID("tenant-a"), testID("t1"), testID("long")} {
		require.NoError(t, EnsureTenant(t.Context(), db, id))
		stored, err := getTenantRow(t.Context(), db, id)
		require.NoError(t, err)
		assert.True(t, validTenantSlug(stored.Slug), stored.Slug)
		assert.Equal(t, id.String(), stored.Slug)
		assert.LessOrEqual(t, len(stored.Slug), 63)
	}
	row, err := getTenantRow(t.Context(), db, testID("t1"))
	require.NoError(t, err)
	assert.Equal(t, row.ID.String(), row.Slug)
	require.NoError(t, EnsureTenant(t.Context(), db, testID("t1")))
}

func newTenantService(t *testing.T) (*bun.DB, *TenantService) {
	t.Helper()
	db, _ := newOrganizationService(t)
	var sequence atomic.Int64
	return db, NewTenantService(db, realOrganizationAuditAppender(db), func() (guid.ID, error) {
		return guid.ID(sequence.Add(1)), nil
	})
}
