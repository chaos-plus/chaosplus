package federation

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFederationDomainConstantsAndErrors(t *testing.T) {
	assert.Equal(t, "oidc", ProviderOIDC)
	assert.Equal(t, "active", ProviderActive)
	assert.Equal(t, "disabled", ProviderDisabled)
	assert.Equal(t, "openid profile email", defaultScopes)

	expected := map[error]string{
		ErrInvalidProvider:        "invalid identity provider",
		ErrProviderNotFound:       "identity provider not found",
		ErrProviderIssuerExists:   "identity provider issuer exists",
		ErrProviderDisabled:       "identity provider is disabled",
		ErrProviderRoleMissing:    "identity provider default role not found",
		ErrOIDCDiscovery:          "OIDC discovery failed",
		ErrOIDCToken:              "OIDC token exchange failed",
		ErrOIDCState:              "invalid OIDC state",
		ErrOIDCTokenInvalid:       "invalid OIDC ID token",
		ErrProvisioningDisabled:   "identity provisioning is disabled for this provider",
		ErrProvisioningUnverified: "identity provisioning requires a verified email",
		ErrPrincipalInactive:      "linked principal is not active",
	}
	for err, message := range expected {
		assert.EqualError(t, err, message)
	}
}

func TestProviderJSONRoundTrip(t *testing.T) {
	provider := Provider{
		ID: "p1", TenantID: "tenant-a", Name: "GitLab", ProviderType: ProviderOIDC,
		Issuer: "https://gitlab.example", ClientID: "app", ClientSecretSet: true,
		Scopes: defaultScopes, AutoProvision: true, DefaultRoleID: "role-a", Status: ProviderActive,
		CreatedAt: time.UnixMilli(1700000000000).UTC(), UpdatedAt: time.UnixMilli(1700000001000).UTC(),
	}
	encoded, err := json.Marshal(provider)
	require.NoError(t, err)
	var decoded Provider
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, provider, decoded)
}
