package governance

import (
	"context"
	"hash/fnv"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testID(value string) guid.ID {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(value))
	id, err := guid.Parse(strconv.FormatUint(hash.Sum64()>>1, 10))
	if err != nil {
		panic(err)
	}
	return id
}

func wireID(value string) string { return strconv.FormatInt(int64(testID(value)), 10) }

func parseGUID(value string) guid.ID {
	id, _ := guid.Parse(value)
	return id
}

func guidString(value guid.ID) string { return value.String() }

func newTestIDGenerator() func() (guid.ID, error) {
	var next atomic.Int64
	return func() (guid.ID, error) { return guid.ID(next.Add(1)), nil }
}

func TestGovernanceModuleLifecycleAndDeclarations(t *testing.T) {
	fixture := newGovernanceFixture(t)
	registry := authz.DefaultRegistry()
	registrar := authz.NewDeclarationOnlyRegistrar(registry)
	module := NewModule(fixture.db, registrar, governanceAuditAppender(fixture.db), governanceRoleGrantStore(), newTestIDGenerator())
	require.NoError(t, module.Migrate(t.Context()))
	_, api := humatest.New(t)
	module.RegisterREST(api)
	require.NoError(t, authz.ValidateOperations(api, registry))
	assert.Equal(t, "access_request_view", api.OpenAPI().Paths["/iam/access-requests"].Get.Extensions[authz.GuardExtensionKey])
	assert.Equal(t, "access_request_approve", api.OpenAPI().Paths["/iam/access-requests/{id}/approve"].Post.Extensions[authz.GuardExtensionKey])
	assert.Equal(t, "access_review_decide", api.OpenAPI().Paths["/iam/access-reviews/{id}/items/{item_id}/decide"].Post.Extensions[authz.GuardExtensionKey])

	declaration := NewDeclarationOnlyModule(authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	assert.NoError(t, declaration.Migrate(t.Context()))
	assert.Panics(t, func() { NewModule(nil, registrar, governanceAuditAppender(fixture.db), RoleGrantStore{}, nil) })
	assert.Panics(t, func() { NewDeclarationOnlyModule(nil) })
	assert.Panics(t, func() { NewDeclarationOnlyModule(registrarWithRuntimeDependencies(t, fixture)) })
}

func registrarWithRuntimeDependencies(t *testing.T, fixture governanceFixture) *authz.Registrar {
	t.Helper()
	return authz.NewRegistrar(authz.DefaultRegistry(), &governanceVerifier{}, iam.NewAuthorizer(fixture.db), iam.NewMembershipChecker(fixture.db))
}

type governanceVerifier struct{}

func (*governanceVerifier) Authenticate(_ context.Context, _, _ string) (*authn.Claims, error) {
	return &authn.Claims{Subject: "requester", SubjectType: authn.SubjectTypePrincipal}, nil
}
