package organization

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepositoryRejectsCorruptHierarchy(t *testing.T) {
	db, service := newOrganizationService(t)
	row := departmentRow{
		TenantID: "tenant", ID: "orphan", ParentID: "missing", Name: "Orphan",
		NameKey: "orphan", Status: StatusActive, Version: 1, CreatedAt: 1, UpdatedAt: 1,
	}
	_, err := db.NewInsert().Model(&row).Exec(t.Context())
	require.NoError(t, err)
	_, err = service.List(t.Context(), "tenant")
	assert.ErrorIs(t, err, ErrHierarchyCorrupt)

	_, err = db.NewDelete().Model(&row).Where("tenant_id = ? AND id = ?", row.TenantID, row.ID).Exec(t.Context())
	require.NoError(t, err)
	department := createDepartment(t.Context(), t, service, "tenant", CreateDepartment{Name: "Root"})
	_, err = db.NewDelete().Model((*closureRow)(nil)).Where("tenant_id = ? AND ancestor_id = ? AND descendant_id = ?", "tenant", department.ID, department.ID).Exec(t.Context())
	require.NoError(t, err)
	_, err = service.Get(t.Context(), "tenant", department.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRepositoryDatabaseFailures(t *testing.T) {
	db, service := newOrganizationService(t)
	repository := service.repo
	require.NoError(t, db.Close())
	row := departmentRow{TenantID: "tenant", ID: "id", Name: "Name", NameKey: "name", Status: StatusActive, Version: 1}

	assert.Error(t, repository.insert(t.Context(), &row))
	assert.Error(t, repository.update(t.Context(), &row, 1))
	assert.Error(t, repository.move(t.Context(), "tenant", "id", ""))
	assert.Error(t, repository.delete(t.Context(), "tenant", "id", 1))
	_, err := repository.isDescendant(t.Context(), "tenant", "a", "b")
	assert.Error(t, err)
	_, err = repository.ancestors(t.Context(), "tenant", "id")
	assert.Error(t, err)
	_, err = repository.descendants(t.Context(), "tenant", "id")
	assert.Error(t, err)
}

func TestOrderDepartmentsRejectsDuplicateOrUnreachableRows(t *testing.T) {
	rows := []departmentRow{
		{TenantID: "tenant", ID: "same", Name: "First", NameKey: "first", Status: StatusActive},
		{TenantID: "tenant", ID: "same", Name: "Second", NameKey: "second", Status: StatusActive},
	}
	_, err := orderDepartments(rows)
	assert.ErrorIs(t, err, ErrHierarchyCorrupt)
}
