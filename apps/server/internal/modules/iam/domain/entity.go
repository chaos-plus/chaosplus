package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

var (
	ErrRoleNotFound                   = errors.New("iam role not found")
	ErrRoleNameConflict               = errors.New("iam role name already exists")
	ErrInvalidArgument                = errors.New("invalid iam argument")
	ErrPermissionNotFound             = errors.New("iam permission not found")
	ErrRolePermissionNotGranted       = errors.New("iam role permission is not granted")
	ErrInvalidRolePermissionCondition = errors.New("invalid iam role permission condition")
	ErrMemberNotFound                 = errors.New("iam tenant member not found")
	ErrMemberInactive                 = errors.New("iam tenant member inactive")
	ErrMenuNotFound                   = errors.New("iam menu not found")
	ErrMenuConflict                   = errors.New("iam menu route already exists")
	ErrMenuHasChildren                = errors.New("iam menu has children")
	ErrDirectoryAssigneeNotFound      = errors.New("iam directory assignee not found")
	ErrDirectoryAssigneeInactive      = errors.New("iam directory assignee inactive")
	ErrLastTenantAdministrator        = errors.New("cannot remove the last tenant administrator")
	ErrEntityNotFound                 = errors.New("iam entity not found")
	ErrEntityConflict                 = errors.New("iam entity already exists")
	ErrEntityHasChildren              = errors.New("iam entity has children")
	ErrEntityHasBindings              = errors.New("iam entity has role bindings")
	ErrEntityHasRelationships         = errors.New("iam entity has relationships")
	ErrEntityHierarchy                = errors.New("invalid iam entity hierarchy")
	ErrInvalidRelationship            = errors.New("invalid iam relationship")
	ErrRelationshipSubjectMissing     = errors.New("iam relationship subject not found")
	ErrRelationshipSubjectInactive    = errors.New("iam relationship subject inactive")
	ErrRelationshipResourceInactive   = errors.New("iam relationship resource inactive")
	ErrRelationshipHierarchy          = errors.New("invalid iam relationship graph")
	ErrInvalidRelationshipWindow      = errors.New("invalid iam relationship validity window")
	ErrInvalidRelationshipCondition   = errors.New("invalid iam relationship condition")
	ErrInvalidResourceAuthorization   = errors.New("invalid business resource authorization")
	ErrAuthorizationChanged           = errors.New("iam authorization policy changed during evaluation")
	ErrInvalidRoleDataScope           = errors.New("invalid role data scope")
	ErrRoleScopeDepartmentNeeded      = errors.New("role data scope requires departments")
	ErrRoleScopeDepartmentsExtra      = errors.New("role data scope does not accept departments")
	ErrRoleScopeDepartmentMissing     = errors.New("role scope department not found")
	ErrRoleScopeDepartmentInactive    = errors.New("role scope department inactive")
	ErrMemberDepartmentMissing        = errors.New("member department not found")
	ErrMemberDepartmentInactive       = errors.New("member department inactive")
	ErrPlatformPermissionScope        = errors.New("permission is not a platform permission")
	ErrPlatformAdministratorNotFound  = errors.New("platform administrator not found")
	ErrLastPlatformAdministrator      = errors.New("cannot remove the last platform administrator")
	ErrPrivilegeEscalation            = errors.New("cannot grant a permission the actor does not hold")
)

// PlatformAdministrator is one principal's platform authorization. A full
// administrator holds every declared platform permission; a restricted one
// holds only the explicitly granted permission codes.
type PlatformAdministrator struct {
	PrincipalID       guid.ID
	FullAdministrator bool
	Permissions       []string
	CreatedAt         time.Time
}

type IDGenerator func() (guid.ID, error)

type Role struct {
	ID          guid.ID
	TenantID    guid.ID
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type RolePermissionGrant struct {
	PermissionCode string
	Condition      json.RawMessage
	CreatedAt      time.Time
}

type DataScope string

const (
	DataScopeAll                      DataScope = "all"
	DataScopeSelf                     DataScope = "self"
	DataScopeDepartment               DataScope = "department"
	DataScopeDepartmentAndDescendants DataScope = "department_and_descendants"
	DataScopeSelectedDepartments      DataScope = "selected_departments"
)

type RoleDataScope struct {
	RoleID        guid.ID
	Scope         DataScope
	DepartmentIDs []guid.ID
	UpdatedAt     time.Time
}

type DirectoryAssigneeType string

const (
	DirectoryAssigneeGroup    DirectoryAssigneeType = "group"
	DirectoryAssigneePosition DirectoryAssigneeType = "position"
)

type RoleDirectoryBinding struct {
	RoleID       guid.ID
	AssigneeType DirectoryAssigneeType
	AssigneeID   guid.ID
	CreatedAt    time.Time
}

type MemberStatus string

const (
	MemberActive   MemberStatus = "active"
	MemberDisabled MemberStatus = "disabled"
)

type TenantMember struct {
	TenantID     guid.ID
	PrincipalID  guid.ID
	DisplayName  string
	Email        string
	DepartmentID guid.ID
	Status       MemberStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DisabledAt   time.Time
}

type EntityStatus string

const (
	EntityActive   EntityStatus = "active"
	EntityDisabled EntityStatus = "disabled"
)

type Entity struct {
	ID        guid.ID
	TenantID  guid.ID
	ParentID  guid.ID
	Type      string
	Name      string
	Status    EntityStatus
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

type EntityPatch struct {
	ParentID *guid.ID
	Type     *string
	Name     *string
	Status   *EntityStatus
	Metadata *map[string]any
}

type BindingEffect string

const (
	BindingAllow BindingEffect = "allow"
	BindingDeny  BindingEffect = "deny"
)

type EntityRoleBinding struct {
	EntityID    guid.ID
	RoleID      guid.ID
	PrincipalID guid.ID
	Effect      BindingEffect
	ExpiresAt   time.Time
	CreatedAt   time.Time
}

type Relationship struct {
	TenantID        guid.ID
	EntityID        guid.ID
	SubjectType     string
	SubjectID       guid.ID
	SubjectRelation string
	Relation        string
	ResourceType    string
	ResourceID      guid.ID
	StartsAt        *time.Time
	EndsAt          *time.Time
	Condition       json.RawMessage
	CreatedAt       time.Time
}

type RelationshipFilter struct {
	EntityID     guid.ID
	ResourceType string
	ResourceID   guid.ID
}

type MemberFilter struct {
	Search string
	Status MemberStatus
	Offset int
	Limit  int
}

type MenuStatus string

const (
	MenuActive   MenuStatus = "active"
	MenuDisabled MenuStatus = "disabled"
)

type Menu struct {
	ID             guid.ID
	TenantID       guid.ID
	ParentID       guid.ID
	Label          string
	Route          string
	Icon           string
	SortOrder      int
	PermissionCode string
	Status         MenuStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
