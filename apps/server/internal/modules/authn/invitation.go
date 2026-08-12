package authn

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

const invitationTokenPrefix = "inv1_"

func (s *WebService) IssueInvitationToken(id guid.ID) (string, string, error) {
	if s == nil || len(s.mfaKey) == 0 || id.Zero() {
		return "", "", fmt.Errorf("invalid invitation credential request")
	}
	secret, err := randomToken(32)
	if err != nil {
		return "", "", fmt.Errorf("generate invitation credential: %w", err)
	}
	token := invitationTokenPrefix + id.String() + "." + secret
	digest, err := s.InvitationTokenDigest(token)
	return token, digest, err
}

func (s *WebService) InvitationTokenDigest(token string) (string, error) {
	if s == nil || len(s.mfaKey) == 0 || !strings.HasPrefix(token, invitationTokenPrefix) || len(token) > 256 {
		return "", fmt.Errorf("invalid invitation credential")
	}
	parts := strings.SplitN(strings.TrimPrefix(token, invitationTokenPrefix), ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("invalid invitation credential")
	}
	mac := hmac.New(sha256.New, s.mfaPurposeKey("invitation:v1"))
	_, _ = mac.Write([]byte("platform:invitation:v1\x00" + token))
	return hex.EncodeToString(mac.Sum(nil)), nil
}
