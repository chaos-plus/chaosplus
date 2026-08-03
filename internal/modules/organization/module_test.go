package organization

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrganizationModuleLifecycleAndRouteDeclarations(t *testing.T) {
	db, service := newOrganizationService(t)
	registrar := authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry())
	key := base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.SeedSize))
	credentials, err := authnmod.NewWebService(authnext.Config{Enabled: true, Issuer: "https://iam.example", SigningKey: key, MFA: authnext.MFAConfig{EncryptionKey: key}, Web: authnext.WebConfig{Enabled: true}}, db)
	require.NoError(t, err)
	principalService := identity.NewService(db, realOrganizationAuditAppender(db), iam.NewAdministratorGuard())
	module := NewModule(db, registrar, realOrganizationAuditAppender(db), iam.NewMembershipChecker(db), iam.NewAdministratorGuard(), service.nextID, credentials, principalService.CreateInvitedPrincipal)
	require.NoError(t, module.Migrate(t.Context()))

	_, api := humatest.New(t)
	module.RegisterREST(api)
	require.NoError(t, authz.ValidateOperations(api, registrar.Registry()))
	operation := api.OpenAPI().Paths["/iam/departments"].Get
	require.NotNil(t, operation)
	assert.Equal(t, "organization-list-departments", operation.OperationID)
	assert.Equal(t, "dept_view", operation.Extensions[authz.GuardExtensionKey])
	positionOperation := api.OpenAPI().Paths["/iam/positions"].Get
	require.NotNil(t, positionOperation)
	assert.Equal(t, "organization-list-positions", positionOperation.OperationID)
	assert.Equal(t, "position_view", positionOperation.Extensions[authz.GuardExtensionKey])
	groupOperation := api.OpenAPI().Paths["/iam/groups"].Get
	require.NotNil(t, groupOperation)
	assert.Equal(t, "organization-list-groups", groupOperation.OperationID)
	assert.Equal(t, "group_view", groupOperation.Extensions[authz.GuardExtensionKey])

	declaration := NewDeclarationOnlyModule(authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	require.NoError(t, declaration.Migrate(t.Context()))
	_, declarationAPI := humatest.New(t)
	declaration.RegisterREST(declarationAPI)
	assert.Equal(t, "organization-create-department", declarationAPI.OpenAPI().Paths["/iam/departments"].Post.OperationID)
	assert.Equal(t, "organization-create-position", declarationAPI.OpenAPI().Paths["/iam/positions"].Post.OperationID)
	assert.Equal(t, "organization-create-group", declarationAPI.OpenAPI().Paths["/iam/groups"].Post.OperationID)

	assert.Panics(t, func() { NewModule(nil, nil, nil, nil, nil, nil, nil, nil) })
	assert.Panics(t, func() { NewDeclarationOnlyModule(nil) })
	assert.Panics(t, func() { NewDeclarationOnlyModule(authz.NewRegistrar(authz.DefaultRegistry(), nil, nil, nil)) })
}
