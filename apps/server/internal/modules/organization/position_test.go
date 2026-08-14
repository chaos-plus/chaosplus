package organization

import (
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPositionNormalization(t *testing.T) {
	tenant, input, err := normalizePositionCreate(testID("tenant"), CreatePosition{Code: " Platform.Engineer ", Name: " Platform Engineer "})
	require.NoError(t, err)
	assert.Equal(t, testID("tenant"), tenant)
	assert.Equal(t, "platform.engineer", input.Code)
	assert.Equal(t, "Platform Engineer", input.Name)
	assert.Equal(t, StatusActive, input.Status)

	for _, candidate := range []CreatePosition{
		{},
		{Code: "1invalid", Name: "Name"},
		{Code: "invalid code", Name: "Name"},
		{Code: strings.Repeat("a", maxPositionCodeLength+1), Name: "Name"},
		{Code: "valid", Name: " "},
		{Code: "valid", Name: "bad\nname"},
		{Code: "valid", Name: "Name", Status: "archived"},
		{Code: "valid", Name: "Name", SortOrder: -1},
	} {
		_, _, err := normalizePositionCreate(testID("tenant"), candidate)
		assert.ErrorIs(t, err, ErrPositionInvalid)
	}

	_, _, _, err = normalizePositionUpdate(testID("tenant"), testID("position"), UpdatePosition{Version: 1})
	assert.ErrorIs(t, err, ErrPositionInvalid)
	code := "Updated_Code"
	name := "Updated"
	status := StatusDisabled
	sortOrder := 20
	_, _, update, err := normalizePositionUpdate(testID("tenant"), testID("position"), UpdatePosition{Code: &code, Name: &name, Status: &status, SortOrder: &sortOrder, Version: 1})
	require.NoError(t, err)
	assert.Equal(t, "updated_code", *update.Code)
}

func TestPositionMemberWindowNormalization(t *testing.T) {
	start := time.Date(2026, 8, 2, 12, 0, 0, 123456789, time.FixedZone("test", 3600))
	end := start.Add(time.Hour)
	_, _, _, window, err := normalizePositionMember(testID("tenant"), testID("position"), testID("principal"), PositionMemberWindow{StartsAt: &start, EndsAt: &end})
	require.NoError(t, err)
	assert.Equal(t, time.UTC, window.StartsAt.Location())
	assert.Equal(t, int64(123000000), int64(window.StartsAt.Nanosecond()))

	for _, item := range []struct {
		tenant, position, principal guid.ID
		window                      PositionMemberWindow
	}{
		{0, testID("position"), testID("principal"), PositionMemberWindow{}},
		{testID("tenant"), 0, testID("principal"), PositionMemberWindow{}},
		{testID("tenant"), testID("position"), 0, PositionMemberWindow{}},
		{testID("tenant"), testID("position"), testID("principal"), PositionMemberWindow{StartsAt: &end, EndsAt: &start}},
		{testID("tenant"), testID("position"), testID("principal"), PositionMemberWindow{StartsAt: &start, EndsAt: &start}},
	} {
		_, _, _, _, err := normalizePositionMember(item.tenant, item.position, item.principal, item.window)
		assert.ErrorIs(t, err, ErrPositionInvalid)
	}
}
