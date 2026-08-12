package iam

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
)

func TestServiceAuthorizationInspectionValidatesCatalog(t *testing.T) {
	repo := newIAMRepository(t)
	service := NewService(authz.DefaultRegistry(), repo, NewAuthorizer(repo.db), newTestAuditAppender(repo.db))
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("principal"), DisplayName: "Principal", Status: MemberActive})
	var err error
	_, err = repo.db.NewInsert().Model(&entityRowForTest{TenantID: testID("tenant"), ID: testID("entity"), Type: "store", Name: "Store", Status: "active", Metadata: "{}", CreatedAt: 1, UpdatedAt: 1}).Exec(t.Context())
	require.NoError(t, err)

	constraint, err := service.AuthorizationConstraint(t.Context(), testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.NotNil(t, constraint.ResourceIDs)
	_, err = service.AuthorizationConstraint(t.Context(), testID("tenant"), "missing_permission", testID("principal"))
	assert.True(t, errors.Is(err, iamdomain.ErrPermissionNotFound))
	_, err = service.AuthorizationConstraint(t.Context(), testID("tenant"), "role_view", testID("principal"))
	assert.True(t, errors.Is(err, iamdomain.ErrInvalidArgument))
	_, err = service.ExplainEntityAuthorization(t.Context(), testID("tenant"), testID("entity"), "store_view", 0)
	assert.True(t, errors.Is(err, iamdomain.ErrInvalidArgument))
	_, err = service.ExplainEntityAuthorization(t.Context(), testID("tenant"), testID("entity"), "role_view", testID("principal"))
	assert.True(t, errors.Is(err, iamdomain.ErrInvalidArgument))
	_, err = service.ExplainResourceAuthorization(t.Context(), testID("tenant"), testID("entity"), "store", 0, "store_view", testID("principal"))
	assert.ErrorIs(t, err, iamdomain.ErrInvalidResourceAuthorization)
	_, err = service.ExplainResourceAuthorization(t.Context(), testID("tenant"), testID("entity"), "store", testID("resource"), "merchant_view", testID("principal"))
	assert.ErrorIs(t, err, iamdomain.ErrInvalidResourceAuthorization)
}
