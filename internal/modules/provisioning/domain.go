package provisioning

import (
	"errors"
	"time"
)

const (
	DirectoryActive   = "active"
	DirectoryDisabled = "disabled"

	ResourceUser  = "User"
	ResourceGroup = "Group"

	UserSchema  = "urn:ietf:params:scim:schemas:core:2.0:User"
	GroupSchema = "urn:ietf:params:scim:schemas:core:2.0:Group"
	ErrorSchema = "urn:ietf:params:scim:api:messages:2.0:Error"
	ListSchema  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	PatchSchema = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	BulkSchema  = "urn:ietf:params:scim:api:messages:2.0:BulkRequest"
	BulkReply   = "urn:ietf:params:scim:api:messages:2.0:BulkResponse"
)

var (
	ErrInvalidDirectory  = errors.New("invalid SCIM directory")
	ErrDirectoryMissing  = errors.New("SCIM directory not found")
	ErrDirectoryName     = errors.New("SCIM directory name conflict")
	ErrDirectoryVersion  = errors.New("SCIM directory version conflict")
	ErrCredentialMissing = errors.New("SCIM credential not found")
	ErrCredentialLimit   = errors.New("SCIM credential limit reached")
	ErrUnauthorized      = errors.New("invalid SCIM bearer credential")
	ErrResourceMissing   = errors.New("SCIM resource not found")
	ErrResourceConflict  = errors.New("SCIM resource uniqueness conflict")
	ErrResourceVersion   = errors.New("SCIM resource version conflict")
	ErrInvalidSCIM       = errors.New("invalid SCIM request")
	ErrInvalidFilter     = errors.New("invalid SCIM filter")
	ErrInvalidPath       = errors.New("invalid SCIM patch path")
	ErrTooMany           = errors.New("SCIM limit exceeded")
)

type Directory struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Credential struct {
	ID          string     `json:"id"`
	DirectoryID string     `json:"directory_id"`
	Name        string     `json:"name"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type CredentialSecret struct {
	Credential Credential `json:"credential"`
	Token      string     `json:"bearer_token" doc:"Shown once. Store it in a secret manager."`
}

type AuthContext struct {
	TenantID     string
	DirectoryID  string
	CredentialID string
}

type ResourceMapping struct {
	DirectoryID  string
	ResourceType string
	ResourceID   string
	ExternalID   string
	ExternalKey  string
	Version      int64
	CreatedAt    int64
	UpdatedAt    int64
	DeletedAt    int64
}
