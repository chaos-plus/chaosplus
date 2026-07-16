package authz

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRegistryContainsBasicIAM(t *testing.T) {
	r := DefaultRegistry()

	for _, code := range []string{
		"platform_administer",
		"tenant_administer",
		"merchant_view",
		"store_view",
		"user_view",
		"role_view",
		"dept_view",
		"menu_view",
		"menu_bind_permission",
	} {
		_, ok := r.Find(code)
		assert.True(t, ok, "missing %s", code)
	}
}

func TestRegistryRejectsDuplicateCodes(t *testing.T) {
	_, err := NewRegistry(
		Action{Resource: "store", Verb: "view"},
		Action{Resource: "store", Verb: "view"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestRegistryValidation(t *testing.T) {
	cases := []Action{
		{},
		{Resource: "Store", Verb: "view"},
		{Resource: "store", Verb: "View"},
		{Resource: "store", Verb: "view", Code: "store:view"},
		{Resource: "store", Verb: "view", Code: "administer"},
	}
	for _, tc := range cases {
		_, err := NewRegistry(tc)
		require.Error(t, err)
	}
}

func TestRegistryFindSortAndGuardCode(t *testing.T) {
	r, err := NewRegistry(
		Action{Resource: "store", Verb: "update"},
		Action{Resource: "merchant", Verb: "view"},
	)
	require.NoError(t, err)

	assert.Equal(t, "store_view", PermissionCode("store", "view"))
	assert.Equal(t, "store_update", Guard{Resource: "store", Verb: "update"}.Code())

	found, ok := r.Find("merchant_view")
	require.True(t, ok)
	assert.Equal(t, "merchant", found.Resource)
	assert.Equal(t, "merchant_view", r.MustFind("merchant_view").Code)
	assert.Equal(t, []string{"merchant_view", "store_update"}, []string{r.All()[0].Code, r.All()[1].Code})
	assert.Panics(t, func() { r.MustFind("missing") })
}

func TestRegisterInitializesZeroRegistry(t *testing.T) {
	var r Registry
	require.NoError(t, r.Register(Action{Resource: "store", Verb: "view"}))
	_, ok := r.Find("store_view")
	assert.True(t, ok)
}

func TestDefaultSchemaIncludesGeneratedTenantGrants(t *testing.T) {
	schema := DefaultSchema()

	assert.Contains(t, schema, "definition tenant")
	assert.Contains(t, schema, "relation store_view_role: role#member")
	assert.Contains(t, schema, "permission store_view = store_view_role + administer")
	assert.Contains(t, schema, "definition merchant")
	assert.Contains(t, schema, "definition store")
	assert.Contains(t, schema, "definition dept")
	assert.Contains(t, schema, "definition menu")
	assert.False(t, strings.Contains(schema, "relation _role"))
}

func TestGenerateSchemaUsesCustomCatalog(t *testing.T) {
	schema := GenerateSchema([]Action{{Resource: "device", Verb: "view"}})
	assert.Contains(t, schema, "relation device_view_role: role#member")
	assert.Contains(t, schema, "permission device_view = device_view_role + administer")
}
