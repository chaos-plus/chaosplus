package iam

import iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"

var (
	ErrRoleNotFound                   = iamdomain.ErrRoleNotFound
	ErrRoleNameConflict               = iamdomain.ErrRoleNameConflict
	ErrInvalidArgument                = iamdomain.ErrInvalidArgument
	ErrPermissionNotFound             = iamdomain.ErrPermissionNotFound
	ErrRolePermissionNotGranted       = iamdomain.ErrRolePermissionNotGranted
	ErrInvalidRolePermissionCondition = iamdomain.ErrInvalidRolePermissionCondition
	ErrMemberNotFound                 = iamdomain.ErrMemberNotFound
	ErrMemberInactive                 = iamdomain.ErrMemberInactive
	ErrMenuNotFound                   = iamdomain.ErrMenuNotFound
	ErrMenuConflict                   = iamdomain.ErrMenuConflict
	ErrMenuHasChildren                = iamdomain.ErrMenuHasChildren
	ErrDirectoryAssigneeNotFound      = iamdomain.ErrDirectoryAssigneeNotFound
	ErrDirectoryAssigneeInactive      = iamdomain.ErrDirectoryAssigneeInactive
	ErrLastTenantAdministrator        = iamdomain.ErrLastTenantAdministrator
	ErrAuthorizationChanged           = iamdomain.ErrAuthorizationChanged
	ErrInvalidRoleDataScope           = iamdomain.ErrInvalidRoleDataScope
	ErrRoleScopeDepartmentNeeded      = iamdomain.ErrRoleScopeDepartmentNeeded
	ErrRoleScopeDepartmentsExtra      = iamdomain.ErrRoleScopeDepartmentsExtra
	ErrRoleScopeDepartmentMissing     = iamdomain.ErrRoleScopeDepartmentMissing
	ErrRoleScopeDepartmentInactive    = iamdomain.ErrRoleScopeDepartmentInactive
	ErrMemberDepartmentMissing        = iamdomain.ErrMemberDepartmentMissing
	ErrMemberDepartmentInactive       = iamdomain.ErrMemberDepartmentInactive
)

type IDGenerator = iamdomain.IDGenerator
type Role = iamdomain.Role
type RolePermissionGrant = iamdomain.RolePermissionGrant
type DataScope = iamdomain.DataScope
type RoleDataScope = iamdomain.RoleDataScope
type DirectoryAssigneeType = iamdomain.DirectoryAssigneeType
type RoleDirectoryBinding = iamdomain.RoleDirectoryBinding
type TenantMember = iamdomain.TenantMember
type MemberStatus = iamdomain.MemberStatus
type MemberFilter = iamdomain.MemberFilter
type Menu = iamdomain.Menu
type MenuStatus = iamdomain.MenuStatus

const (
	MemberActive                      = iamdomain.MemberActive
	MemberDisabled                    = iamdomain.MemberDisabled
	MenuActive                        = iamdomain.MenuActive
	MenuDisabled                      = iamdomain.MenuDisabled
	DirectoryAssigneeGroup            = iamdomain.DirectoryAssigneeGroup
	DirectoryAssigneePosition         = iamdomain.DirectoryAssigneePosition
	DataScopeAll                      = iamdomain.DataScopeAll
	DataScopeSelf                     = iamdomain.DataScopeSelf
	DataScopeDepartment               = iamdomain.DataScopeDepartment
	DataScopeDepartmentAndDescendants = iamdomain.DataScopeDepartmentAndDescendants
	DataScopeSelectedDepartments      = iamdomain.DataScopeSelectedDepartments
)
