package authn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvitationCredentialLifecycle(t *testing.T) {
	service, _ := newLocalService(t)
	token, digest, err := service.IssueInvitationToken("invitation-id")
	require.NoError(t, err)
	assert.Contains(t, token, "cpi1_invitation-id.")
	assert.NotContains(t, digest, token)
	recomputed, err := service.InvitationTokenDigest(token)
	require.NoError(t, err)
	assert.Equal(t, digest, recomputed)

	otherToken, otherDigest, err := service.IssueInvitationToken("invitation-id")
	require.NoError(t, err)
	assert.NotEqual(t, token, otherToken)
	assert.NotEqual(t, digest, otherDigest)
	_, err = service.InvitationTokenDigest("invalid")
	assert.Error(t, err)
	_, _, err = service.IssueInvitationToken("")
	assert.Error(t, err)
}
