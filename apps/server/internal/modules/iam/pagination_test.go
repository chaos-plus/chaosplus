package iam

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
)

func TestNormalizePageClampsCallerInput(t *testing.T) {
	cases := []struct {
		name                  string
		offset, limit         int
		wantOffset, wantLimit int
	}{
		{"defaults mean everything", 0, 0, 0, 0},
		{"negative offset floors at zero", -5, 10, 0, 10},
		{"negative limit means everything", 0, -1, 0, 0},
		{"limit is capped", 0, 5000, 0, maxPageLimit},
		{"limit at the cap is kept", 10, maxPageLimit, 10, maxPageLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			offset, limit := normalizePage(tc.offset, tc.limit)
			assert.Equal(t, tc.wantOffset, offset)
			assert.Equal(t, tc.wantLimit, limit)
		})
	}
}

func TestListEntitiesPagePushesPagingIntoTheDatabase(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("tenant")))
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("other")))
	for i := range 7 {
		_, err := repo.CreateEntity(t.Context(), iamdomain.Entity{
			TenantID: testID("tenant"), Type: "store", Name: fmt.Sprintf("Store %02d", i), Status: iamdomain.EntityActive,
		})
		require.NoError(t, err)
	}
	_, err := repo.CreateEntity(t.Context(), iamdomain.Entity{
		TenantID: testID("other"), Type: "store", Name: "Foreign", Status: iamdomain.EntityActive,
	})
	require.NoError(t, err)

	// The total reflects the whole tenant even when a page is requested.
	page, total, err := repo.ListEntitiesPage(t.Context(), testID("tenant"), 0, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(7), total, "total must not leak the other tenant")
	require.Len(t, page, 3)
	assert.Equal(t, "Store 00", page[0].Name)

	second, total, err := repo.ListEntitiesPage(t.Context(), testID("tenant"), 3, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(7), total)
	require.Len(t, second, 3)
	assert.Equal(t, "Store 03", second[0].Name)

	tail, _, err := repo.ListEntitiesPage(t.Context(), testID("tenant"), 6, 3)
	require.NoError(t, err)
	assert.Len(t, tail, 1, "the final short page is not padded")

	past, _, err := repo.ListEntitiesPage(t.Context(), testID("tenant"), 99, 3)
	require.NoError(t, err)
	assert.Empty(t, past)

	// A zero limit keeps the historical "everything" contract.
	all, total, err := repo.ListEntitiesPage(t.Context(), testID("tenant"), 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(7), total)
	assert.Len(t, all, 7)
}

func TestListRolesPagePushesPagingIntoTheDatabase(t *testing.T) {
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("tenant")))
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("other")))
	for i := range 5 {
		_, err := repo.CreateRole(t.Context(), testID("tenant"), fmt.Sprintf("Role %02d", i), "")
		require.NoError(t, err)
	}
	_, err := repo.CreateRole(t.Context(), testID("other"), "Foreign", "")
	require.NoError(t, err)

	page, total, err := repo.ListRolesPage(t.Context(), testID("tenant"), 1, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	require.Len(t, page, 2)
	assert.Equal(t, "Role 01", page[0].Name)
	assert.Equal(t, "Role 02", page[1].Name)

	all, total, err := repo.ListRolesPage(t.Context(), testID("tenant"), 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	assert.Len(t, all, 5)
}

func TestServicePagingValidatesTenant(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("tenant")))

	_, _, err := service.ListEntitiesPage(t.Context(), 0, 0, 10)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, _, err = service.ListRolesPage(t.Context(), 0, 0, 10)
	assert.ErrorIs(t, err, ErrInvalidArgument)

	entities, total, err := service.ListEntitiesPage(t.Context(), testID("tenant"), 0, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, entities)

	roles, total, err := service.ListRolesPage(t.Context(), testID("tenant"), 0, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, roles)
}
