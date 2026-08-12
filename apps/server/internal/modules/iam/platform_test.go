package iam

import (
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
)

// TestAPIErrorMapsDomainErrorsToStatuses pins the HTTP contract for every
// declared IAM domain error. The mapping is the only thing standing between a
// domain failure and a misleading 500, and most branches were unreachable from
// the handler tests.
func TestAPIErrorMapsDomainErrorsToStatuses(t *testing.T) {
	status := func(err error) int {
		t.Helper()
		var statusErr huma.StatusError
		require.ErrorAs(t, err, &statusErr)
		return statusErr.GetStatus()
	}

	notFound := []error{
		iamdomain.ErrRoleNotFound, iamdomain.ErrMemberNotFound, iamdomain.ErrMenuNotFound,
		iamdomain.ErrDirectoryAssigneeNotFound, iamdomain.ErrEntityNotFound,
		iamdomain.ErrRelationshipSubjectMissing, iamdomain.ErrRoleScopeDepartmentMissing,
		iamdomain.ErrMemberDepartmentMissing, iamdomain.ErrRolePermissionNotGranted,
	}
	for _, err := range notFound {
		assert.Equal(t, http.StatusNotFound, status(apiError("op", err)), err)
	}

	conflict := []error{
		iamdomain.ErrRoleNameConflict, iamdomain.ErrMenuConflict, iamdomain.ErrMenuHasChildren,
		iamdomain.ErrMemberInactive, iamdomain.ErrDirectoryAssigneeInactive, iamdomain.ErrLastTenantAdministrator,
		iamdomain.ErrEntityConflict, iamdomain.ErrEntityHasChildren, iamdomain.ErrEntityHasBindings,
		iamdomain.ErrEntityHasRelationships, iamdomain.ErrEntityHierarchy,
		iamdomain.ErrRelationshipSubjectInactive, iamdomain.ErrRelationshipResourceInactive,
		iamdomain.ErrRelationshipHierarchy, iamdomain.ErrRoleScopeDepartmentInactive,
		iamdomain.ErrMemberDepartmentInactive,
	}
	for _, err := range conflict {
		assert.Equal(t, http.StatusConflict, status(apiError("op", err)), err)
	}

	unprocessable := []error{
		iamdomain.ErrInvalidRelationship, iamdomain.ErrInvalidRelationshipWindow,
		iamdomain.ErrInvalidRelationshipCondition, iamdomain.ErrInvalidResourceAuthorization,
		iamdomain.ErrInvalidRoleDataScope, iamdomain.ErrRoleScopeDepartmentNeeded,
		iamdomain.ErrRoleScopeDepartmentsExtra, iamdomain.ErrPermissionNotFound,
		iamdomain.ErrInvalidRolePermissionCondition, iamdomain.ErrInvalidArgument,
	}
	for _, err := range unprocessable {
		assert.Equal(t, http.StatusUnprocessableEntity, status(apiError("op", err)), err)
	}

	// Escalation is a policy refusal, not a validation problem.
	assert.Equal(t, http.StatusForbidden, status(apiError("op", ErrPrivilegeEscalation)))
	// A concurrent policy change is retryable, so it must not look like a bug.
	assert.Equal(t, http.StatusServiceUnavailable, status(apiError("op", iamdomain.ErrAuthorizationChanged)))
	// Anything undeclared stays a 500 rather than leaking internals.
	assert.Equal(t, http.StatusInternalServerError, status(apiError("op", errors.New("unexpected"))))

	// The platform mapper adds its own cases and delegates the rest.
	assert.Equal(t, http.StatusNotFound, status(platformAPIError("op", iamdomain.ErrPlatformAdministratorNotFound)))
	assert.Equal(t, http.StatusConflict, status(platformAPIError("op", iamdomain.ErrLastPlatformAdministrator)))
	assert.Equal(t, http.StatusUnprocessableEntity, status(platformAPIError("op", iamdomain.ErrPlatformPermissionScope)))
	assert.Equal(t, http.StatusNotFound, status(platformAPIError("op", iamdomain.ErrRoleNotFound)))
}

func TestPaginateListBoundsResults(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	// A zero limit keeps the whole collection.
	assert.Len(t, paginateList(t.Context(), items, 0, 0).Body.Data, 5)
	assert.Len(t, paginateList(t.Context(), items, 1, 2).Body.Data, 2)
	// An offset past the end yields an empty page rather than an error.
	assert.Empty(t, paginateList(t.Context(), items, 99, 2).Body.Data)
	// A trailing partial page is not padded.
	assert.Len(t, paginateList(t.Context(), items, 4, 3).Body.Data, 1)
	// Negative and oversized inputs are clamped instead of panicking.
	assert.Len(t, paginateList(t.Context(), items, -1, -1).Body.Data, 5)
	assert.Len(t, paginateList(t.Context(), items, 2, 0).Body.Data, 3)
}

func seedPlatformPrincipal(t *testing.T, repo *Repository, principalID guid.ID) {
	t.Helper()
	now := repo.now().UTC().UnixMilli()
	_, err := repo.db.ExecContext(t.Context(), `INSERT INTO iam_principals
		(id, login_name, email, display_name, status, created_at, updated_at, disabled_at)
		VALUES (?, ?, ?, ?, 'active', ?, ?, 0)`,
		principalID, principalID.String(), principalID.String()+"@example.test", principalID.String(), now, now)
	require.NoError(t, err)
}

func newPlatformService(t *testing.T) (*Service, *Repository) {
	t.Helper()
	repo := newIAMRepository(t)
	require.NoError(t, organization.EnsureTenant(t.Context(), repo.db, testID("t1")))
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	return service, repo
}

func TestCheckPlatformHonorsPermissionCode(t *testing.T) {
	service, repo := newPlatformService(t)
	seedPlatformPrincipal(t, repo, testID("restricted"))
	seedPlatformPrincipal(t, repo, testID("full"))
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "operator"})
	authorizer := NewAuthorizer(repo.db)

	_, err := service.SetPlatformAdministrator(ctx, testID("full"), true, nil)
	require.NoError(t, err)
	_, err = service.SetPlatformAdministrator(ctx, testID("restricted"), false, []string{"tenant_view"})
	require.NoError(t, err)

	// A restricted principal holds exactly the granted code and nothing else.
	allowed, err := authorizer.CheckPlatform(t.Context(), "tenant_view", testID("restricted"))
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, err = authorizer.CheckPlatform(t.Context(), "tenant_delete", testID("restricted"))
	require.NoError(t, err)
	assert.False(t, allowed, "restricted platform principal must not inherit other platform permissions")
	allowed, err = authorizer.CheckPlatform(t.Context(), "platform_administer", testID("restricted"))
	require.NoError(t, err)
	assert.False(t, allowed)

	// A full administrator still holds every declared platform permission.
	for _, code := range []string{"tenant_view", "tenant_create", "tenant_update", "tenant_delete", "platform_administer"} {
		allowed, err = authorizer.CheckPlatform(t.Context(), code, testID("full"))
		require.NoError(t, err)
		assert.True(t, allowed, code)
	}
}

func TestCheckPlatformFailsClosedForNonPlatformCodes(t *testing.T) {
	service, repo := newPlatformService(t)
	seedPlatformPrincipal(t, repo, testID("full"))
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "operator"})
	_, err := service.SetPlatformAdministrator(ctx, testID("full"), true, nil)
	require.NoError(t, err)
	authorizer := NewAuthorizer(repo.db)

	// Tenant-scoped and unknown codes must never be satisfied by platform state.
	for _, code := range []string{"store_view", "tenant_administer", "not_a_real_permission"} {
		allowed, err := authorizer.CheckPlatform(t.Context(), code, testID("full"))
		require.NoError(t, err)
		assert.False(t, allowed, code)
	}
	_, err = authorizer.CheckPlatform(t.Context(), "", testID("full"))
	assert.Error(t, err)
}

func TestCheckPlatformIgnoresDisabledPrincipal(t *testing.T) {
	service, repo := newPlatformService(t)
	seedPlatformPrincipal(t, repo, testID("restricted"))
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "operator"})
	_, err := service.SetPlatformAdministrator(ctx, testID("restricted"), false, []string{"tenant_view"})
	require.NoError(t, err)
	authorizer := NewAuthorizer(repo.db)

	allowed, err := authorizer.CheckPlatform(t.Context(), "tenant_view", testID("restricted"))
	require.NoError(t, err)
	assert.True(t, allowed)
	_, err = repo.db.ExecContext(t.Context(), "UPDATE iam_principals SET status = 'disabled' WHERE id = ?", testID("restricted"))
	require.NoError(t, err)
	allowed, err = authorizer.CheckPlatform(t.Context(), "tenant_view", testID("restricted"))
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestServiceSetPlatformAdministratorRejectsInvalidPermissions(t *testing.T) {
	service, repo := newPlatformService(t)
	seedPlatformPrincipal(t, repo, testID("candidate"))
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "operator"})

	_, err := service.SetPlatformAdministrator(ctx, testID("candidate"), false, []string{"store_view"})
	assert.ErrorIs(t, err, ErrPlatformPermissionScope)
	_, err = service.SetPlatformAdministrator(ctx, testID("candidate"), false, []string{"not_a_real_permission"})
	assert.ErrorIs(t, err, ErrPermissionNotFound)
	_, err = service.SetPlatformAdministrator(ctx, testID("candidate"), false, nil)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = service.SetPlatformAdministrator(ctx, testID("candidate"), true, []string{"tenant_view"})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = service.SetPlatformAdministrator(ctx, 0, true, nil)
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestServicePlatformAdministratorLifecycleKeepsOneFullAdministrator(t *testing.T) {
	service, repo := newPlatformService(t)
	seedPlatformPrincipal(t, repo, testID("first"))
	seedPlatformPrincipal(t, repo, testID("second"))
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "operator"})

	first, err := service.SetPlatformAdministrator(ctx, testID("first"), true, nil)
	require.NoError(t, err)
	assert.True(t, first.FullAdministrator)
	assert.Empty(t, first.Permissions)

	// The only full administrator can neither be demoted nor removed.
	_, err = service.SetPlatformAdministrator(ctx, testID("first"), false, []string{"tenant_view"})
	assert.ErrorIs(t, err, ErrLastPlatformAdministrator)
	_, err = service.DeletePlatformAdministrator(ctx, testID("first"))
	assert.ErrorIs(t, err, ErrLastPlatformAdministrator)
	stored, err := service.ListPlatformAdministrators(t.Context())
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.True(t, stored[0].FullAdministrator, "a refused demotion must roll back")

	// A second full administrator unlocks both operations.
	_, err = service.SetPlatformAdministrator(ctx, testID("second"), true, nil)
	require.NoError(t, err)
	demoted, err := service.SetPlatformAdministrator(ctx, testID("first"), false, []string{"tenant_view", "tenant_view", "tenant_create"})
	require.NoError(t, err)
	assert.False(t, demoted.FullAdministrator)
	assert.Equal(t, []string{"tenant_create", "tenant_view"}, demoted.Permissions, "codes are deduplicated and sorted")

	changed, err := service.DeletePlatformAdministrator(ctx, testID("first"))
	require.NoError(t, err)
	assert.True(t, changed)
	_, err = service.DeletePlatformAdministrator(ctx, testID("first"))
	assert.ErrorIs(t, err, ErrPlatformAdministratorNotFound)

	remaining, err := service.ListPlatformAdministrators(t.Context())
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, testID("second"), remaining[0].PrincipalID)
}

func TestRepositoryPlatformAdministratorLookups(t *testing.T) {
	_, repo := newPlatformService(t)
	seedPlatformPrincipal(t, repo, testID("someone"))

	_, err := repo.GetPlatformAdministrator(t.Context(), testID("someone"))
	assert.ErrorIs(t, err, ErrPlatformAdministratorNotFound)
	_, err = repo.GetPlatformAdministrator(t.Context(), 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = repo.SetPlatformAdministrator(t.Context(), 0, false, []string{"tenant_view"})
	assert.ErrorIs(t, err, ErrInvalidArgument)
	_, err = repo.SetPlatformAdministrator(t.Context(), testID("someone"), false, nil)
	assert.ErrorIs(t, err, ErrPlatformAdministratorNotFound)
	_, err = repo.DeletePlatformAdministrator(t.Context(), 0)
	assert.ErrorIs(t, err, ErrInvalidArgument)

	changed, err := repo.DeletePlatformAdministrator(t.Context(), testID("someone"))
	require.NoError(t, err)
	assert.False(t, changed)

	count, err := repo.CountFullPlatformAdministrators(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Switching between full and restricted must not leave stale rows behind.
	_, err = repo.SetPlatformAdministrator(t.Context(), testID("someone"), true, nil)
	require.NoError(t, err)
	restricted, err := repo.SetPlatformAdministrator(t.Context(), testID("someone"), false, []string{"tenant_view"})
	require.NoError(t, err)
	assert.False(t, restricted.FullAdministrator)
	count, err = repo.CountFullPlatformAdministrators(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	promoted, err := repo.SetPlatformAdministrator(t.Context(), testID("someone"), true, nil)
	require.NoError(t, err)
	assert.True(t, promoted.FullAdministrator)
	assert.Empty(t, promoted.Permissions, "promotion clears explicit grants")
}

func TestPlatformPermissionCatalogExposesOnlyPlatformScope(t *testing.T) {
	service, _ := newPlatformService(t)
	catalog := service.PlatformPermissionCatalog(t.Context())
	require.NotEmpty(t, catalog)
	codes := make([]string, 0, len(catalog))
	for _, action := range catalog {
		assert.Equal(t, "platform", action.Scope, action.Code)
		codes = append(codes, action.Code)
	}
	assert.Contains(t, codes, "platform_administer")
	assert.NotContains(t, codes, "tenant_administer", "tenant_administer is tenant scoped")
	assert.Equal(t, codes, PlatformPermissionCodes(authz.DefaultRegistry()))
	assert.Empty(t, PlatformPermissionCodes(nil))

	// The tenant catalog must stay disjoint from the platform catalog.
	for _, action := range service.PermissionCatalog(t.Context()) {
		assert.NotContains(t, codes, action.Code)
	}
}
