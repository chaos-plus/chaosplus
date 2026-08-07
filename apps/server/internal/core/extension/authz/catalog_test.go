package authz

import (
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
		"role_manage_assignee",
		"dept_view",
		"position_manage_member",
		"group_manage_member",
		"menu_view",
		"menu_bind_permission",
		"access_review_view",
		"access_review_create",
		"access_review_decide",
		"access_review_manage",
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
		{Resource: "store", Verb: "view", AllowedRelations: []string{"unknown"}},
		{Resource: "store", Verb: "view", AllowedRelations: []string{"owner", "owner"}},
	}
	for _, tc := range cases {
		_, err := NewRegistry(tc)
		require.Error(t, err)
	}
}

func TestRegistryRelationshipGrants(t *testing.T) {
	action := DefaultRegistry().MustFind("store_view")
	assert.Equal(t, []string{"editor", "owner", "viewer"}, action.AllowedRelations)
	assert.Empty(t, DefaultRegistry().MustFind("store_create").AllowedRelations)
}

func TestRegistryRejectsGeneratedRelationCollision(t *testing.T) {
	_, err := NewRegistry(Action{Resource: "store", Verb: "view_role"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "suffix _role is reserved")
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
