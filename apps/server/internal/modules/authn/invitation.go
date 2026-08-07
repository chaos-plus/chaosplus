package authn

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const invitationTokenPrefix = "cpi1_"

func (s *WebService) IssueInvitationToken(id string) (string, string, error) {
	id = strings.TrimSpace(id)
	if s == nil || len(s.mfaKey) == 0 || id == "" || len(id) > 128 || strings.ContainsAny(id, ".\r\n") {
		return "", "", fmt.Errorf("invalid invitation credential request")
	}
	secret, err := randomToken(32)
	if err != nil {
		return "", "", fmt.Errorf("generate invitation credential: %w", err)
	}
	token := invitationTokenPrefix + id + "." + secret
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
	_, _ = mac.Write([]byte("chaosplus:invitation:v1\x00" + token))
	return hex.EncodeToString(mac.Sum(nil)), nil
}
