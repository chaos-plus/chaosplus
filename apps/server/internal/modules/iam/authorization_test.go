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
	putTestMember(t, repo, TenantMember{TenantID: "tenant", Subject: "principal", DisplayName: "Principal", Status: MemberActive})
	var err error
	_, err = repo.db.NewInsert().Model(&entityRowForTest{TenantID: "tenant", ID: "entity", Type: "store", Name: "Store", Status: "active", Metadata: "{}", CreatedAt: 1, UpdatedAt: 1}).Exec(t.Context())
	require.NoError(t, err)

	constraint, err := service.AuthorizationConstraint(t.Context(), "tenant", "store_view", "principal")
	require.NoError(t, err)
	assert.NotNil(t, constraint.ResourceIDs)
	_, err = service.AuthorizationConstraint(t.Context(), "tenant", "missing_permission", "principal")
	assert.True(t, errors.Is(err, iamdomain.ErrPermissionNotFound))
	_, err = service.AuthorizationConstraint(t.Context(), "tenant", "role_view", "principal")
	assert.True(t, errors.Is(err, iamdomain.ErrInvalidArgument))
	_, err = service.ExplainEntityAuthorization(t.Context(), "tenant", "entity", "store_view", "")
	assert.True(t, errors.Is(err, iamdomain.ErrInvalidArgument))
	_, err = service.ExplainEntityAuthorization(t.Context(), "tenant", "entity", "role_view", "principal")
	assert.True(t, errors.Is(err, iamdomain.ErrInvalidArgument))
	_, err = service.ExplainResourceAuthorization(t.Context(), "tenant", "entity", "store", "", "store_view", "principal")
	assert.ErrorIs(t, err, iamdomain.ErrInvalidResourceAuthorization)
	_, err = service.ExplainResourceAuthorization(t.Context(), "tenant", "entity", "store", "resource", "merchant_view", "principal")
	assert.ErrorIs(t, err, iamdomain.ErrInvalidResourceAuthorization)
}
