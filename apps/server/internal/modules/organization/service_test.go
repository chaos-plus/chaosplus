package organization

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

var organizationAuditGenerators sync.Map

func TestDepartmentLifecycle(t *testing.T) {
	db, service := newOrganizationService(t)
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "administrator", PrincipalID: testID("administrator")})

	rootA := createDepartment(ctx, t, service, testID("tenant-a"), CreateDepartment{Name: "Engineering", SortOrder: 10})
	rootB := createDepartment(ctx, t, service, testID("tenant-a"), CreateDepartment{Name: "Operations", SortOrder: 5})
	child := createDepartment(ctx, t, service, testID("tenant-a"), CreateDepartment{ParentID: rootA.ID, Name: "Platform", SortOrder: 2})
	grandchild := createDepartment(ctx, t, service, testID("tenant-a"), CreateDepartment{ParentID: child.ID, Name: "Runtime"})
	otherTenant := createDepartment(ctx, t, service, testID("tenant-b"), CreateDepartment{Name: "Engineering"})

	items, err := service.List(ctx, testID("tenant-a"))
	require.NoError(t, err)
	require.Len(t, items, 4)
	assert.Equal(t, []string{rootB.ID.String(), rootA.ID.String(), child.ID.String(), grandchild.ID.String()}, departmentIDs(items))
	assert.Equal(t, []int{0, 0, 1, 2}, departmentDepths(items))

	got, err := service.Get(ctx, testID("tenant-a"), grandchild.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, got.Depth)
	_, err = service.Get(ctx, testID("tenant-a"), otherTenant.ID)
	assert.ErrorIs(t, err, ErrNotFound)

	_, err = service.Create(ctx, testID("tenant-a"), CreateDepartment{Name: " engineering "})
	assert.ErrorIs(t, err, ErrNameConflict)
	_, err = service.Create(ctx, testID("tenant-a"), CreateDepartment{ParentID: otherTenant.ID, Name: "Cross tenant"})
	assert.ErrorIs(t, err, ErrNotFound)

	newParent := rootB.ID
	newName := "Platform Engineering"
	disabled := StatusDisabled
	sortOrder := 7
	child, err = service.Update(ctx, testID("tenant-a"), child.ID, UpdateDepartment{
		ParentID: &newParent, Name: &newName, Status: &disabled, SortOrder: &sortOrder, Version: child.Version,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), child.Version)
	assert.Equal(t, 1, child.Depth)
	assert.Equal(t, StatusDisabled, child.Status)
	grandchild, err = service.Get(ctx, testID("tenant-a"), grandchild.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, grandchild.Depth)

	cycleParent := grandchild.ID
	_, err = service.Update(ctx, testID("tenant-a"), rootB.ID, UpdateDepartment{ParentID: &cycleParent, Version: rootB.Version})
	assert.ErrorIs(t, err, ErrHierarchyCycle)
	_, err = service.Update(ctx, testID("tenant-a"), child.ID, UpdateDepartment{Name: &newName, Version: 1})
	assert.ErrorIs(t, err, ErrVersionConflict)

	revisionBeforeNoop, err := policyx.Current(ctx, db, testID("tenant-a"))
	require.NoError(t, err)
	child, err = service.Update(ctx, testID("tenant-a"), child.ID, UpdateDepartment{Name: &newName, Version: child.Version})
	require.NoError(t, err)
	assert.Equal(t, int64(2), child.Version)
	revisionAfterNoop, err := policyx.Current(ctx, db, testID("tenant-a"))
	require.NoError(t, err)
	assert.Equal(t, revisionBeforeNoop, revisionAfterNoop)

	err = service.Delete(ctx, testID("tenant-a"), rootB.ID, rootB.Version)
	assert.ErrorIs(t, err, ErrHasChildren)
	require.NoError(t, service.Delete(ctx, testID("tenant-a"), grandchild.ID, grandchild.Version))
	require.NoError(t, service.Delete(ctx, testID("tenant-a"), child.ID, child.Version))
	require.NoError(t, service.Delete(ctx, testID("tenant-a"), rootB.ID, rootB.Version))
	_, err = service.Get(ctx, testID("tenant-a"), rootB.ID)
	assert.ErrorIs(t, err, ErrNotFound)

	events, total, err := auditmod.NewService(db, newTestIDGenerator()).List(ctx, auditmod.Filter{TenantID: testID("tenant-a"), Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, int64(8), total)
	require.Len(t, events, 8)
	for _, event := range events {
		assert.Equal(t, testID("administrator"), event.PrincipalID)
		assert.Equal(t, "department", event.TargetType)
		assert.NotContains(t, string(event.Detail), "Engineering")
	}
	integrity, err := auditmod.NewService(db, newTestIDGenerator()).Verify(ctx, testID("tenant-a"))
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
	assert.Equal(t, int64(8), integrity.VerifiedEvents)
	revision, err := policyx.Current(ctx, db, testID("tenant-a"))
	require.NoError(t, err)
	assert.Equal(t, int64(8), revision)
	otherRevision, err := policyx.Current(ctx, db, testID("tenant-b"))
	require.NoError(t, err)
	assert.Equal(t, int64(1), otherRevision)
}

func TestDepartmentValidationAndFailures(t *testing.T) {
	db, service := newOrganizationService(t)
	for _, input := range []struct {
		tenant guid.ID
		value  CreateDepartment
	}{
		{0, CreateDepartment{Name: "Engineering"}},
		{testID("tenant"), CreateDepartment{}},
		{testID("tenant"), CreateDepartment{Name: strings.Repeat("x", 129)}},
		{testID("tenant"), CreateDepartment{Name: "bad\nname"}},
		{testID("tenant"), CreateDepartment{Name: "Engineering", Status: "archived"}},
		{testID("tenant"), CreateDepartment{Name: "Engineering", SortOrder: -1}},
		{testID("tenant"), CreateDepartment{Name: "Engineering", SortOrder: maxSortOrder + 1}},
	} {
		_, err := service.Create(t.Context(), input.tenant, input.value)
		assert.ErrorIs(t, err, ErrInvalid)
	}
	_, err := service.List(t.Context(), 0)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.Get(t.Context(), testID("tenant"), 0)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.Update(t.Context(), testID("tenant"), testID("id"), UpdateDepartment{})
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.Update(t.Context(), testID("tenant"), testID("id"), UpdateDepartment{Version: 1})
	assert.ErrorIs(t, err, ErrInvalid)
	assert.ErrorIs(t, service.Delete(t.Context(), testID("tenant"), testID("id"), 0), ErrInvalid)

	service.nextID = func() (guid.ID, error) { return 0, errors.New("entropy unavailable") }
	_, err = service.Create(t.Context(), testID("tenant"), CreateDepartment{Name: "Engineering"})
	assert.ErrorContains(t, err, "entropy unavailable")
	service.nextID = func() (guid.ID, error) { return 0, nil }
	_, err = service.Create(t.Context(), testID("tenant"), CreateDepartment{Name: "Engineering"})
	assert.ErrorIs(t, err, ErrInvalid)

	require.NoError(t, db.Close())
	_, err = service.List(t.Context(), testID("tenant"))
	assert.Error(t, err)
	_, err = service.Get(t.Context(), testID("tenant"), testID("id"))
	assert.Error(t, err)
	assert.Panics(t, func() { NewService(nil, realOrganizationAuditAppender(db), newTestIDGenerator()) })
	assert.Panics(t, func() { NewService(db, nil, newTestIDGenerator()) })
	assert.Panics(t, func() { NewService(db, realOrganizationAuditAppender(db), nil) })
	assert.Panics(t, func() { NewRepository(nil) })
	assert.True(t, bunx.IsUniqueViolation(errors.New("duplicate key value")))
	assert.False(t, bunx.IsUniqueViolation(errors.New("connection closed")))
}

func TestDepartmentCreateRollsBackWhenAuditFails(t *testing.T) {
	db, service := newOrganizationService(t)
	_, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_department_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'department_created' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)

	_, err = service.Create(t.Context(), testID("tenant"), CreateDepartment{Name: "Engineering"})
	assert.Error(t, err)
	count, countErr := db.NewSelect().Model((*departmentRow)(nil)).Count(t.Context())
	require.NoError(t, countErr)
	assert.Zero(t, count)
	revision, revisionErr := policyx.Current(t.Context(), db, testID("tenant"))
	require.NoError(t, revisionErr)
	assert.Zero(t, revision)
}

func TestDepartmentMoveToRootAndMutationRollback(t *testing.T) {
	db, service := newOrganizationService(t)
	root := createDepartment(t.Context(), t, service, testID("tenant"), CreateDepartment{Name: "Root"})
	child := createDepartment(t.Context(), t, service, testID("tenant"), CreateDepartment{ParentID: root.ID, Name: "Child"})

	emptyParent := guid.ID(0)
	child, err := service.Update(t.Context(), testID("tenant"), child.ID, UpdateDepartment{ParentID: &emptyParent, Version: child.Version})
	require.NoError(t, err)
	assert.Empty(t, child.ParentID)
	assert.Zero(t, child.Depth)

	missingParent := testID("missing")
	_, err = service.Update(t.Context(), testID("tenant"), child.ID, UpdateDepartment{ParentID: &missingParent, Version: child.Version})
	assert.ErrorIs(t, err, ErrNotFound)
	selfParent := child.ID
	_, err = service.Update(t.Context(), testID("tenant"), child.ID, UpdateDepartment{ParentID: &selfParent, Version: child.Version})
	assert.ErrorIs(t, err, ErrHierarchyCycle)
	_, err = service.Update(t.Context(), testID("tenant"), testID("missing"), UpdateDepartment{Name: stringPointer("Missing"), Version: 1})
	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorIs(t, service.Delete(t.Context(), testID("tenant"), testID("missing"), 1), ErrNotFound)

	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER reject_department_update_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'department_updated' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)
	newName := "Changed"
	_, err = service.Update(t.Context(), testID("tenant"), child.ID, UpdateDepartment{Name: &newName, Version: child.Version})
	assert.Error(t, err)
	current, getErr := service.Get(t.Context(), testID("tenant"), child.ID)
	require.NoError(t, getErr)
	assert.Equal(t, "Child", current.Name)
	assert.Equal(t, child.Version, current.Version)
	require.NoError(t, dropSQLiteTrigger(t, db, "reject_department_update_audit"))

	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER reject_department_delete_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'department_deleted' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)
	err = service.Delete(t.Context(), testID("tenant"), child.ID, child.Version)
	assert.Error(t, err)
	_, getErr = service.Get(t.Context(), testID("tenant"), child.ID)
	assert.NoError(t, getErr)
}

func TestDepartmentUpdateValidation(t *testing.T) {
	_, service := newOrganizationService(t)
	cases := []UpdateDepartment{
		{ParentID: guidPointer(guid.ID(-1)), Version: 1},
		{Name: stringPointer(" "), Version: 1},
		{Name: stringPointer(strings.Repeat("n", 129)), Version: 1},
		{Status: stringPointer("archived"), Version: 1},
		{SortOrder: intPointer(-1), Version: 1},
		{SortOrder: intPointer(maxSortOrder + 1), Version: 1},
	}
	for _, input := range cases {
		_, err := service.Update(t.Context(), testID("tenant"), testID("department"), input)
		assert.ErrorIs(t, err, ErrInvalid)
	}
	_, err := service.Update(t.Context(), 0, testID("department"), UpdateDepartment{Name: stringPointer("Name"), Version: 1})
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.Update(t.Context(), testID("tenant"), 0, UpdateDepartment{Name: stringPointer("Name"), Version: 1})
	assert.ErrorIs(t, err, ErrInvalid)
}

func newOrganizationService(t *testing.T) (*bun.DB, *Service) {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	var sequence atomic.Int64
	service := NewService(db, realOrganizationAuditAppender(db), func() (guid.ID, error) {
		return guid.ID(sequence.Add(1)), nil
	})
	return db, service
}

func realOrganizationAuditAppender(db *bun.DB) auditx.Appender {
	value, _ := organizationAuditGenerators.LoadOrStore(db, newTestIDGenerator())
	nextID := value.(func() (guid.ID, error))
	service := auditmod.NewService(db, nextID)
	return func(ctx context.Context, executor bun.IDB, event auditx.Event) error {
		_, err := service.AppendTo(ctx, executor, auditmod.EventInput{
			TenantID: event.TenantID, PrincipalID: event.PrincipalID, EventType: event.EventType,
			TargetType: event.TargetType, TargetID: event.TargetID, Outcome: "success", Detail: event.Detail,
		})
		return err
	}
}

func createDepartment(ctx context.Context, t *testing.T, service *Service, tenantID guid.ID, input CreateDepartment) Department {
	t.Helper()
	department, err := service.Create(ctx, tenantID, input)
	require.NoError(t, err)
	return department
}

func departmentIDs(items []Department) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.ID.String())
	}
	return result
}

func departmentDepths(items []Department) []int {
	result := make([]int, 0, len(items))
	for _, item := range items {
		result = append(result, item.Depth)
	}
	return result
}

func stringPointer(value string) *string { return &value }
func intPointer(value int) *int          { return &value }

func guidPointer(value guid.ID) *guid.ID { return &value }

func dropSQLiteTrigger(t *testing.T, db *bun.DB, name string) error {
	t.Helper()
	_, err := db.ExecContext(t.Context(), "DROP TRIGGER "+name)
	return err
}
