package iam

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemporaryRoleGrantControlsAuthorizationWindow(t *testing.T) {
	repo := newIAMRepository(t)
	putTestMember(t, repo, TenantMember{TenantID: testID("tenant"), PrincipalID: testID("principal"), DisplayName: "Principal", Status: MemberActive})
	role := createTestRole(t, repo, testID("tenant"), "Temporary viewer")
	_, err := repo.GrantPermission(t.Context(), testID("tenant"), role.ID, "store_view")
	require.NoError(t, err)
	start := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	require.NoError(t, GrantTemporaryRole(t.Context(), repo.db, testID("tenant"), testID("grant"), role.ID, testID("principal"), testID("approver"), start, end))

	authorizer := NewAuthorizer(repo.db)
	authorizer.now = func() time.Time { return start.Add(time.Minute) }
	allowed, err := authorizer.Check(t.Context(), testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.True(t, allowed)
	authorizer.now = func() time.Time { return end }
	allowed, err = authorizer.Check(t.Context(), testID("tenant"), "store_view", testID("principal"))
	require.NoError(t, err)
	assert.False(t, allowed)

	changed, err := RevokeTemporaryRole(t.Context(), repo.db, testID("tenant"), testID("grant"))
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = RevokeTemporaryRole(t.Context(), repo.db, testID("tenant"), testID("grant"))
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestTemporaryRoleGrantValidation(t *testing.T) {
	repo := newIAMRepository(t)
	role := createTestRole(t, repo, testID("tenant"), "Temporary viewer")
	now := time.Now().UTC()
	assert.Error(t, GrantTemporaryRole(t.Context(), nil, testID("tenant"), testID("grant"), role.ID, testID("principal"), testID("approver"), now, now.Add(time.Hour)))
	assert.Error(t, GrantTemporaryRole(t.Context(), repo.db, testID("tenant"), testID("grant"), role.ID, testID("principal"), testID("approver"), now, now))
	assert.Error(t, GrantTemporaryRole(t.Context(), repo.db, testID("tenant"), testID("grant"), testID("missing"), testID("principal"), testID("approver"), now, now.Add(time.Hour)))
	assert.ErrorIs(t, GrantTemporaryRole(t.Context(), repo.db, testID("tenant"), testID("grant"), role.ID, testID("principal"), testID("approver"), now, now.Add(time.Hour)), ErrMemberInactive)
	_, err := RevokeTemporaryRole(t.Context(), nil, testID("tenant"), testID("grant"))
	assert.Error(t, err)
}
