package provisioning

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSCIMErrorMappingIsLocalizedAndStable(t *testing.T) {
	require.NoError(t, i18n.InitEmbedded(i18n.Base))
	require.NoError(t, RegisterI18n())
	ctx := i18n.WithLocale(context.Background(), "zh-CN")
	tests := []struct {
		err      error
		status   int
		scimType string
	}{
		{identity.ErrLoginConflict, http.StatusConflict, "uniqueness"},
		{organization.ErrGroupNameConflict, http.StatusConflict, "uniqueness"},
		{domain.ErrLastTenantAdministrator, http.StatusConflict, "mutability"},
		{organization.ErrGroupMemberInactive, http.StatusBadRequest, "invalidValue"},
		{identity.ErrInvalid, http.StatusBadRequest, "invalidValue"},
		{organization.ErrGroupInvalid, http.StatusBadRequest, "invalidValue"},
		{ErrUnauthorized, http.StatusUnauthorized, ""},
		{ErrResourceMissing, http.StatusNotFound, ""},
		{ErrResourceConflict, http.StatusConflict, "uniqueness"},
		{ErrResourceVersion, http.StatusPreconditionFailed, ""},
		{ErrInvalidFilter, http.StatusBadRequest, "invalidFilter"},
		{ErrInvalidPath, http.StatusBadRequest, "invalidPath"},
		{ErrTooMany, http.StatusRequestEntityTooLarge, "tooMany"},
		{ErrInvalidSCIM, http.StatusBadRequest, "invalidSyntax"},
		{ErrInvalidDirectory, http.StatusBadRequest, "invalidSyntax"},
		{assert.AnError, http.StatusInternalServerError, ""},
	}
	for _, test := range tests {
		result := mapSCIMError(ctx, test.err)
		assert.Equal(t, test.status, result.GetStatus(), test.err)
		assert.Equal(t, test.scimType, result.ScimType, test.err)
		assert.NotEmpty(t, result.Error(), test.err)
		assert.NotContains(t, result.Detail, "scim_", test.err)
		assert.Equal(t, SCIMContentType, result.GetHeaders().Get("Content-Type"))
	}
	assert.Contains(t, mapSCIMError(ctx, ErrUnauthorized).GetHeaders().Get("WWW-Authenticate"), "Bearer")
	assert.ErrorIs(t, serviceErrorKind(errors.Join(assert.AnError, ErrResourceConflict)), ErrResourceConflict)
	assert.Equal(t, assert.AnError, serviceErrorKind(assert.AnError))
}

func TestSCIMInputNormalizationAndETags(t *testing.T) {
	input := activeUserInput(" external ", " Alice ", "ALICE@example.test")
	input.DisplayName = ""
	input.Name.Formatted = "Alice Name"
	normalized, active, err := normalizeUserInput(input)
	require.NoError(t, err)
	assert.True(t, active)
	assert.Equal(t, "alice", normalized.UserName)
	assert.Equal(t, "Alice Name", normalized.DisplayName)
	assert.Equal(t, "alice@example.test", normalized.Emails[0].Value)

	for _, invalid := range []UserInput{
		{},
		{Schemas: []string{UserSchema, UserSchema}, UserName: "alice"},
		{Schemas: []string{UserSchema}, UserName: "alice", Emails: []UserEmail{{Value: "bad"}}},
		{Schemas: []string{UserSchema}, UserName: "alice", Emails: []UserEmail{{Value: "a@example.test", Primary: true}, {Value: "b@example.test", Primary: true}}},
	} {
		_, _, err := normalizeUserInput(invalid)
		assert.ErrorIs(t, err, ErrInvalidSCIM)
	}

	group, members, err := normalizeGroupInput(GroupInput{Schemas: []string{GroupSchema}, DisplayName: " Team ", Members: []SCIMGroupMember{{Value: wireID("one")}, {Value: wireID("one")}, {Value: wireID("two")}}})
	require.NoError(t, err)
	assert.Equal(t, "Team", group.DisplayName)
	assert.Equal(t, []guid.ID{testID("one"), testID("two")}, members)
	assert.Len(t, group.Members, 2)
	_, _, err = normalizeGroupInput(GroupInput{Schemas: []string{GroupSchema}, DisplayName: ""})
	assert.ErrorIs(t, err, ErrInvalidSCIM)

	assert.Equal(t, `W/"7"`, weakETag(7))
	for _, value := range []string{`W/"7"`, `"7"`} {
		version, err := parseETag(value)
		require.NoError(t, err)
		assert.Equal(t, int64(7), version)
	}
	version, err := parseETag("")
	require.NoError(t, err)
	assert.Zero(t, version)
	for _, value := range []string{"7", `W/"0"`, `W/"bad"`, `"7`} {
		_, err := parseETag(value)
		assert.ErrorIs(t, err, ErrResourceVersion, value)
	}

	request, err := normalizeListRequest(" ", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, request.StartIndex)
	assert.Equal(t, 100, request.Count)
	request, err = normalizeListRequest("", 1, -1)
	require.NoError(t, err)
	assert.Zero(t, request.Count)
	_, err = normalizeListRequest(strings.Repeat("x", maxFilterBytes+1), 1, 1)
	assert.ErrorIs(t, err, ErrInvalidSCIM)

	var decoded struct {
		Name string `json:"name"`
	}
	assert.NoError(t, strictDecode([]byte(`{"name":"ok"}`), &decoded))
	assert.ErrorIs(t, strictDecode([]byte(`{"name":"ok","extra":true}`), &decoded), ErrInvalidSCIM)
	assert.ErrorIs(t, strictDecode([]byte(`{"name":"ok"}{}`), &decoded), ErrInvalidSCIM)
}
