package federation

import (
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFederationModuleDeclaresManagementAndBrowserOperations(t *testing.T) {
	registry := authz.DefaultRegistry()
	registrar := authz.NewDeclarationOnlyRegistrar(registry)
	module := NewDeclarationOnlyModule(registrar)
	_, api := humatest.New(t)
	module.RegisterREST(api)
	require.NoError(t, authz.ValidateOperations(api, registry))

	for _, path := range []string{
		"/iam/identity-providers", "/iam/identity-providers/{provider_id}",
		"/federation/{provider_id}/start", "/federation/{provider_id}/callback",
		"/iam/saml/service-providers", "/iam/saml/service-providers/{sp_id}",
		"/iam/saml/signing-key/rotate",
		"/federation/saml/{tenant_id}/metadata", "/federation/saml/{tenant_id}/sso", "/federation/saml/{tenant_id}/slo",
	} {
		assert.Contains(t, api.OpenAPI().Paths, path)
	}
	create := api.OpenAPI().Paths["/iam/identity-providers"].Post
	assert.Equal(t, "identity_provider_create", create.Extensions[authz.GuardExtensionKey])
	list := api.OpenAPI().Paths["/iam/identity-providers"].Get
	assert.Equal(t, "identity_provider_view", list.Extensions[authz.GuardExtensionKey])
	start := api.OpenAPI().Paths["/federation/{provider_id}/start"].Get
	assert.Equal(t, true, start.Metadata["authz.public"])
	callback := api.OpenAPI().Paths["/federation/{provider_id}/callback"].Get
	assert.Equal(t, true, callback.Metadata["authz.public"])
	assert.Contains(t, callback.Responses, "302")
	sso := api.OpenAPI().Paths["/federation/saml/{tenant_id}/sso"].Get
	assert.Equal(t, true, sso.Metadata["authz.public"])
	metadata := api.OpenAPI().Paths["/federation/saml/{tenant_id}/metadata"].Get
	assert.Equal(t, true, metadata.Metadata["authz.public"])
	samlCreate := api.OpenAPI().Paths["/iam/saml/service-providers"].Post
	assert.Equal(t, "identity_provider_create", samlCreate.Extensions[authz.GuardExtensionKey])
	samlList := api.OpenAPI().Paths["/iam/saml/service-providers"].Get
	assert.Equal(t, "identity_provider_view", samlList.Extensions[authz.GuardExtensionKey])
	require.NoError(t, module.Migrate(t.Context()))
}

func TestFederationRuntimeModuleUsesRealServices(t *testing.T) {
	env := newFederationEnvironment(t)
	registrar := authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry())
	module := NewModule(env.db, registrar, env.audit, env.identities, env.web, Config{HTTPTimeout: 5 * time.Second, ClockSkew: 30 * time.Second, StateTTL: 10 * time.Minute}, env.key, newTestIDGenerator())
	require.NoError(t, module.Migrate(t.Context()))
	_, api := humatest.New(t)
	module.RegisterREST(api)
	assert.Contains(t, api.OpenAPI().Paths, "/iam/identity-providers")
	assert.Panics(t, func() {
		NewModule(nil, registrar, env.audit, env.identities, env.web, Config{}, env.key, newTestIDGenerator())
	})
	assert.Panics(t, func() { NewDeclarationOnlyModule(nil) })
	assert.Panics(t, func() { NewDeclarationOnlyModule(authz.NewRegistrar(authz.DefaultRegistry(), nil, nil, nil)) })
}
