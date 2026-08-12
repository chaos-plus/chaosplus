package organization

import (
	"errors"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvisionedGroupLifecycle(t *testing.T) {
	db, service := newGroupService(t)
	addTenantMember(t, db, "tenant", "principal-a", "Alice", iam.MemberActive)
	addTenantMember(t, db, "tenant", "principal-b", "Bob", iam.MemberActive)
	addTenantMember(t, db, "tenant", "principal-disabled", "Disabled", iam.MemberDisabled)

	group, err := service.CreateProvisionedTo(t.Context(), db, testID("tenant"), ProvisionedGroupInput{
		DisplayName: " SCIM Operators ", Active: true,
		MemberIDs: []guid.ID{testID("principal-a"), testID("principal-a")},
	})
	require.NoError(t, err)
	assert.Equal(t, "SCIM Operators", group.Name)
	assert.Equal(t, StatusActive, group.Status)
	members, err := service.ListMembers(t.Context(), testID("tenant"), group.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, testID("principal-a"), members[0].PrincipalID)

	_, err = service.CreateProvisionedTo(t.Context(), nil, testID("tenant"), ProvisionedGroupInput{DisplayName: "invalid"})
	assert.ErrorIs(t, err, ErrGroupInvalid)
	_, err = service.CreateProvisionedTo(t.Context(), db, testID("tenant"), ProvisionedGroupInput{DisplayName: "", Active: true})
	assert.ErrorIs(t, err, ErrGroupInvalid)
	_, err = service.CreateProvisionedTo(t.Context(), db, 0, ProvisionedGroupInput{DisplayName: "Invalid tenant", Active: true})
	assert.ErrorIs(t, err, ErrGroupInvalid)
	_, err = service.CreateProvisionedTo(t.Context(), db, testID("tenant"), ProvisionedGroupInput{DisplayName: group.Name, Active: true})
	assert.ErrorIs(t, err, ErrGroupNameConflict)
	_, err = service.CreateProvisionedTo(t.Context(), db, testID("tenant"), ProvisionedGroupInput{DisplayName: "Inactive member", MemberIDs: []guid.ID{testID("principal-disabled")}})
	assert.ErrorIs(t, err, ErrGroupMemberInactive)

	revisionBefore, err := policyx.Current(t.Context(), db, testID("tenant"))
	require.NoError(t, err)
	group, err = service.ReplaceProvisionedTo(t.Context(), db, testID("tenant"), group.ID, ProvisionedGroupInput{
		DisplayName: "SCIM Security", Active: false, MemberIDs: []guid.ID{testID("principal-b")},
	})
	require.NoError(t, err)
	assert.Equal(t, StatusDisabled, group.Status)
	members, err = service.ListMembers(t.Context(), testID("tenant"), group.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, testID("principal-b"), members[0].PrincipalID)
	revisionAfter, err := policyx.Current(t.Context(), db, testID("tenant"))
	require.NoError(t, err)
	assert.Greater(t, revisionAfter, revisionBefore)

	unchanged, err := service.ReplaceProvisionedTo(t.Context(), db, testID("tenant"), group.ID, ProvisionedGroupInput{
		DisplayName: group.Name, Active: false, MemberIDs: []guid.ID{testID("principal-b")},
	})
	require.NoError(t, err)
	assert.Equal(t, group.Version, unchanged.Version)
	_, err = service.ReplaceProvisionedTo(t.Context(), db, testID("tenant"), group.ID, ProvisionedGroupInput{
		DisplayName: "Invalid member", Active: true, MemberIDs: []guid.ID{testID("missing")},
	})
	assert.ErrorIs(t, err, ErrGroupMemberInactive)
	_, err = service.ReplaceProvisionedTo(t.Context(), nil, testID("tenant"), group.ID, ProvisionedGroupInput{DisplayName: "invalid"})
	assert.ErrorIs(t, err, ErrGroupInvalid)
	_, err = service.ReplaceProvisionedTo(t.Context(), db, testID("tenant"), testID("missing"), ProvisionedGroupInput{DisplayName: "missing"})
	assert.ErrorIs(t, err, ErrGroupNotFound)

	group, err = service.ReplaceProvisionedTo(t.Context(), db, testID("tenant"), group.ID, ProvisionedGroupInput{
		DisplayName: group.Name, Active: true, MemberIDs: []guid.ID{testID("principal-b")},
	})
	require.NoError(t, err)
	disabled, err := service.DisableProvisionedTo(t.Context(), db, testID("tenant"), group.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusDisabled, disabled.Status)
	again, err := service.DisableProvisionedTo(t.Context(), db, testID("tenant"), group.ID)
	require.NoError(t, err)
	assert.Equal(t, disabled.Version, again.Version)
	_, err = service.DisableProvisionedTo(t.Context(), nil, testID("tenant"), group.ID)
	assert.ErrorIs(t, err, ErrGroupInvalid)
	_, err = service.DisableProvisionedTo(t.Context(), db, testID("tenant"), testID("missing"))
	assert.ErrorIs(t, err, ErrGroupNotFound)
	rule := MembershipRule(`{"version":1,"match":"all","conditions":[{"field":"member.status","operator":"in","values":["ACTIVE"]}]}`)
	dynamic, err := service.Create(t.Context(), testID("tenant"), CreateGroup{Name: "Dynamic", Type: GroupTypeDynamic, Rule: rule, Status: StatusActive})
	require.NoError(t, err)
	_, err = service.ReplaceProvisionedTo(t.Context(), db, testID("tenant"), dynamic.ID, ProvisionedGroupInput{DisplayName: dynamic.Name, Active: true})
	assert.ErrorIs(t, err, ErrGroupInvalid)
	service.nextID = func() (guid.ID, error) { return 0, errors.New("entropy unavailable") }
	_, err = service.CreateProvisionedTo(t.Context(), db, testID("tenant"), ProvisionedGroupInput{DisplayName: "No ID", Active: true})
	assert.ErrorContains(t, err, "entropy unavailable")

	invalidMembers := make([]guid.ID, 1001)
	_, _, err = normalizeProvisionedGroup(ProvisionedGroupInput{DisplayName: "Too many", MemberIDs: invalidMembers})
	assert.ErrorIs(t, err, ErrGroupInvalid)
	_, _, err = normalizeProvisionedGroup(ProvisionedGroupInput{DisplayName: "Invalid", MemberIDs: []guid.ID{0}})
	assert.ErrorIs(t, err, ErrGroupInvalid)
	assert.Equal(t, StatusActive, provisionedGroupStatus(true))
	assert.Equal(t, StatusDisabled, provisionedGroupStatus(false))
	assert.EqualError(t, errorsOr(errors.New("id failed"), ErrGroupInvalid), "id failed")
	assert.ErrorIs(t, errorsOr(nil, ErrGroupInvalid), ErrGroupInvalid)
}
