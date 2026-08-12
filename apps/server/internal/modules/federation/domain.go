package federation

import (
	"errors"
	"time"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

const (
	ProviderOIDC = "oidc"
	ProviderSAML = "saml"

	ProviderActive   = "active"
	ProviderDisabled = "disabled"

	defaultScopes = "openid profile email"
)

var (
	ErrInvalidProvider        = errors.New("invalid identity provider")
	ErrProviderNotFound       = errors.New("identity provider not found")
	ErrProviderIssuerExists   = errors.New("identity provider issuer exists")
	ErrProviderDisabled       = errors.New("identity provider is disabled")
	ErrProviderRoleMissing    = errors.New("identity provider default role not found")
	ErrOIDCDiscovery          = errors.New("OIDC discovery failed")
	ErrOIDCToken              = errors.New("OIDC token exchange failed")
	ErrOIDCState              = errors.New("invalid OIDC state")
	ErrOIDCTokenInvalid       = errors.New("invalid OIDC ID token")
	ErrSAMLResponse           = errors.New("invalid SAML response")
	ErrInvalidSAMLSP          = errors.New("invalid SAML service provider")
	ErrSAMLSPNotFound         = errors.New("SAML service provider not found")
	ErrSAMLSPEntityIDExists   = errors.New("SAML service provider entity ID exists")
	ErrProvisioningDisabled   = errors.New("identity provisioning is disabled for this provider")
	ErrProvisioningUnverified = errors.New("identity provisioning requires a verified email")
	ErrPrincipalInactive      = errors.New("linked principal is not active")
)

// Provider is a tenant-scoped upstream identity provider used for inbound
// federation. Only the OIDC authorization-code flow is implemented; SAML is a
// separate future delivery and must never reuse this code path.
type Provider struct {
	ID              guid.ID   `json:"id"`
	TenantID        guid.ID   `json:"tenant_id"`
	Name            string    `json:"name"`
	ProviderType    string    `json:"provider_type"`
	Issuer          string    `json:"issuer"`
	ClientID        string    `json:"client_id"`
	ClientSecretSet bool      `json:"client_secret_set"`
	Scopes          string    `json:"scopes"`
	AutoProvision   bool      `json:"auto_provision"`
	DefaultRoleID   guid.ID   `json:"default_role_id,omitempty"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ProviderInput carries the validated fields accepted by the management API.
type ProviderInput struct {
	Name          string
	ProviderType  string
	Issuer        string
	ClientID      string
	ClientSecret  string
	Scopes        string
	AutoProvision bool
	DefaultRoleID guid.ID
	Status        string
}

// IdentityLink binds one external subject to one global principal inside the
// tenant that owns the provider. Reusing the link on later logins is what makes
// federation idempotent: one external identity always maps to one account.
type IdentityLink struct {
	ProviderID      string
	TenantID        string
	PrincipalID     string
	ExternalSubject string
	Email           string
	DisplayName     string
	LastLoginAt     time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// SAMLServiceProvider is a tenant-registered downstream application that
// consumes SAML assertions from the Chaosplus identity provider. Its metadata
// document is the source of truth for entity ID, assertion consumer, and
// single-logout endpoints.
type SAMLServiceProvider struct {
	ID          guid.ID   `json:"id"`
	TenantID    guid.ID   `json:"tenant_id"`
	Name        string    `json:"name"`
	EntityID    string    `json:"entity_id"`
	MetadataXML string    `json:"metadata_xml"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SAMLServiceProviderInput carries the validated fields accepted by the
// management API.
type SAMLServiceProviderInput struct {
	Name        string
	EntityID    string
	MetadataXML string
	Status      string
}
