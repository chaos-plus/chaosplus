package organization

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupNormalization(t *testing.T) {
	tenant, input, err := normalizeGroupCreate(" tenant ", CreateGroup{Name: " Operators ", Description: " IAM operators "})
	require.NoError(t, err)
	assert.Equal(t, "tenant", tenant)
	assert.Equal(t, "Operators", input.Name)
	assert.Equal(t, "IAM operators", input.Description)
	assert.Equal(t, GroupTypeStatic, input.Type)
	assert.Equal(t, StatusActive, input.Status)

	for _, candidate := range []CreateGroup{
		{},
		{Name: "Group", Type: "unknown"},
		{Name: "Group", Status: "unknown"},
		{Name: "Group", SortOrder: -1},
		{Name: "Group", Description: strings.Repeat("x", maxGroupDescriptionLength+1)},
	} {
		_, _, err := normalizeGroupCreate("tenant", candidate)
		assert.ErrorIs(t, err, ErrGroupInvalid)
	}
	dynamicRule := MembershipRule(`{"version":1,"match":"all","conditions":[{"field":"member.status","operator":"in","values":["ACTIVE"]}]}`)
	_, dynamic, err := normalizeGroupCreate("tenant", CreateGroup{Name: "Dynamic", Type: GroupTypeDynamic, Rule: dynamicRule})
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":1,"match":"all","conditions":[{"field":"member.status","operator":"in","values":["active"]}]}`, string(dynamic.Rule))
	_, _, err = normalizeGroupCreate("tenant", CreateGroup{Name: "Dynamic", Type: GroupTypeDynamic})
	assert.ErrorIs(t, err, ErrGroupRuleInvalid)
	_, _, err = normalizeGroupCreate("tenant", CreateGroup{Name: "Static", Rule: dynamicRule})
	assert.ErrorIs(t, err, ErrGroupRuleType)

	_, _, _, err = normalizeGroupUpdate("tenant", "group", UpdateGroup{Version: 1})
	assert.ErrorIs(t, err, ErrGroupInvalid)
	name, description, status, order := " Security ", " Updated ", StatusDisabled, 10
	_, _, update, err := normalizeGroupUpdate("tenant", "group", UpdateGroup{Name: &name, Description: &description, Status: &status, SortOrder: &order, Version: 1})
	require.NoError(t, err)
	assert.Equal(t, "Security", *update.Name)
	assert.Equal(t, "Updated", *update.Description)
	rule := MembershipRule(`{"version":1,"match":"any","conditions":[{"field":"member.department_id","operator":"in","values":["engineering"]}]}`)
	_, _, update, err = normalizeGroupUpdate("tenant", "group", UpdateGroup{Rule: &rule, Version: 1})
	require.NoError(t, err)
	assert.JSONEq(t, string(rule), string(*update.Rule))
}

func TestGroupMemberWindowNormalization(t *testing.T) {
	start := time.Date(2026, 8, 2, 12, 0, 0, 123456789, time.FixedZone("test", 3600))
	end := start.Add(time.Hour)
	_, _, _, window, err := normalizeGroupMember(" tenant ", " group ", " principal ", GroupMemberWindow{StartsAt: &start, EndsAt: &end})
	require.NoError(t, err)
	assert.Equal(t, time.UTC, window.StartsAt.Location())
	assert.Zero(t, window.StartsAt.Nanosecond()%int(time.Millisecond))

	for _, item := range []struct {
		tenant, group, principal string
		window                   GroupMemberWindow
	}{
		{"", "group", "principal", GroupMemberWindow{}},
		{"tenant", "", "principal", GroupMemberWindow{}},
		{"tenant", "group", "", GroupMemberWindow{}},
		{"tenant", "group", strings.Repeat("p", 256), GroupMemberWindow{}},
		{"tenant", "group", "principal", GroupMemberWindow{StartsAt: &end, EndsAt: &start}},
		{"tenant", "group", "principal", GroupMemberWindow{StartsAt: &start, EndsAt: &start}},
	} {
		_, _, _, _, err := normalizeGroupMember(item.tenant, item.group, item.principal, item.window)
		assert.ErrorIs(t, err, ErrGroupInvalid)
	}
}
