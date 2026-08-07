package provisioning

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPatchValidationAndApplication(t *testing.T) {
	active := true
	input := UserInput{Schemas: []string{UserSchema}, UserName: "alice", DisplayName: "Alice", Active: &active, Emails: []UserEmail{{Value: "old@example.test", Primary: true}}}
	operations := []PatchOperation{
		{Op: "replace", Value: json.RawMessage(`{"userName":"alice-one","displayName":"Alice One","name":{"formatted":"Alice Object"},"active":true,"externalId":"external-1","emails":[{"value":"object@example.test","primary":true}]}`)},
		{Op: "replace", Path: "userName", Value: json.RawMessage(`"alice"`)},
		{Op: "replace", Path: UserSchema + ":name.formatted", Value: json.RawMessage(`"Alice Formatted"`)},
		{Op: "replace", Path: "active", Value: json.RawMessage(`false`)},
		{Op: "replace", Path: "emails", Value: json.RawMessage(`[{"value":"new@example.test","primary":true}]`)},
		{Op: "add", Path: "emails", Value: json.RawMessage(`{"value":"second@example.test"}`)},
		{Op: "remove", Path: "displayName"},
		{Op: "remove", Path: "name.formatted"},
		{Op: "remove", Path: "externalId"},
		{Op: "add", Path: "externalId", Value: json.RawMessage(`"external-2"`)},
	}
	for _, operation := range operations {
		require.NoError(t, applyUserPatch(&input, operation))
	}
	assert.Empty(t, input.DisplayName)
	assert.Empty(t, input.Name.Formatted)
	assert.False(t, *input.Active)
	assert.Equal(t, "external-2", input.ExternalID)
	require.Len(t, input.Emails, 2)
	assert.Equal(t, "new@example.test", input.Emails[0].Value)

	for _, operation := range []PatchOperation{
		{Op: "remove", Path: "userName"},
		{Op: "replace", Path: "active", Value: json.RawMessage(`"false"`)},
		{Op: "replace", Path: GroupSchema + ":displayName", Value: json.RawMessage(`"wrong schema"`)},
		{Op: "replace", Path: `emails[value eq "old@example.test"]`, Value: json.RawMessage(`[]`)},
		{Op: "replace", Path: "[", Value: json.RawMessage(`"x"`)},
		{Op: "replace", Path: "emails", Value: json.RawMessage(`true`)},
		{Op: "replace", Value: json.RawMessage(`{"unknown":true}`)},
		{Op: "remove"},
		{Op: "copy", Path: "displayName", Value: json.RawMessage(`"x"`)},
	} {
		assert.ErrorIs(t, applyUserPatch(&input, operation), ErrInvalidPath)
	}
}

func TestGroupPatchMemberOperations(t *testing.T) {
	input := GroupInput{Schemas: []string{GroupSchema}, DisplayName: "Team", Members: []SCIMGroupMember{{Value: "one"}, {Value: "two"}}}
	require.NoError(t, applyGroupPatch(&input, PatchOperation{Op: "add", Value: json.RawMessage(`{"displayName":"Team Added","externalId":"external","members":[{"value":"zero"}]}`)}))
	assert.Equal(t, "Team Added", input.DisplayName)
	assert.Equal(t, "external", input.ExternalID)
	require.Len(t, input.Members, 3)
	require.NoError(t, applyGroupPatch(&input, PatchOperation{Op: "replace", Path: "displayName", Value: json.RawMessage(`"Team"`)}))
	require.NoError(t, applyGroupPatch(&input, PatchOperation{Op: "replace", Path: "externalId", Value: json.RawMessage(`"external-2"`)}))
	require.NoError(t, applyGroupPatch(&input, PatchOperation{Op: "remove", Path: "externalId"}))
	assert.Empty(t, input.ExternalID)
	require.NoError(t, applyGroupPatch(&input, PatchOperation{Op: "add", Path: "members", Value: json.RawMessage(`{"value":"three"}`)}))
	require.Len(t, input.Members, 4)
	require.NoError(t, applyGroupPatch(&input, PatchOperation{Op: "remove", Path: `members[value eq "two"]`}))
	require.Len(t, input.Members, 3)
	assert.Equal(t, "three", input.Members[2].Value)
	require.NoError(t, applyGroupPatch(&input, PatchOperation{Op: "replace", Path: "members", Value: json.RawMessage(`[{"value":"one"},{"value":"three"}]`)}))
	require.Len(t, input.Members, 2)
	require.NoError(t, applyGroupPatch(&input, PatchOperation{Op: "replace", Value: json.RawMessage(`{"displayName":"Renamed","members":[{"value":"one"}]}`)}))
	assert.Equal(t, "Renamed", input.DisplayName)
	require.Len(t, input.Members, 1)
	require.NoError(t, applyGroupPatch(&input, PatchOperation{Op: "remove", Path: "members"}))
	assert.Empty(t, input.Members)

	for _, operation := range []PatchOperation{
		{Op: "remove", Path: "displayName"},
		{Op: "replace", Path: `members[value eq "one"]`, Value: json.RawMessage(`{"value":"two"}`)},
		{Op: "remove", Path: `members[value co "one"]`},
		{Op: "remove", Path: `members[value eq 1]`},
		{Op: "replace", Path: "members", Value: json.RawMessage(`true`)},
		{Op: "replace", Path: "[", Value: json.RawMessage(`"x"`)},
		{Op: "replace", Path: "unknown", Value: json.RawMessage(`"x"`)},
		{Op: "replace", Path: UserSchema + ":displayName", Value: json.RawMessage(`"x"`)},
	} {
		assert.ErrorIs(t, applyGroupPatch(&input, operation), ErrInvalidPath)
	}
}

func TestPatchEnvelopeLimits(t *testing.T) {
	assert.True(t, validPatch(PatchRequest{Schemas: []string{PatchSchema}, Operations: []PatchOperation{{Op: "remove", Path: "members"}}}))
	assert.False(t, validPatch(PatchRequest{Schemas: []string{"wrong"}, Operations: []PatchOperation{{}}}))
	assert.False(t, validPatch(PatchRequest{Schemas: []string{PatchSchema}}))
	assert.False(t, validPatch(PatchRequest{Schemas: []string{PatchSchema}, Operations: make([]PatchOperation, maxPatchOps+1)}))
}
