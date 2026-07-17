package iam

import iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"

var (
	ErrRoleNotFound       = iamdomain.ErrRoleNotFound
	ErrRoleNameConflict   = iamdomain.ErrRoleNameConflict
	ErrInvalidArgument    = iamdomain.ErrInvalidArgument
	ErrPermissionNotFound = iamdomain.ErrPermissionNotFound
	ErrMemberNotFound     = iamdomain.ErrMemberNotFound
	ErrMemberInactive     = iamdomain.ErrMemberInactive
	ErrMenuNotFound       = iamdomain.ErrMenuNotFound
	ErrMenuConflict       = iamdomain.ErrMenuConflict
	ErrMenuHasChildren    = iamdomain.ErrMenuHasChildren
)

type IDGenerator = iamdomain.IDGenerator
type Role = iamdomain.Role
type TenantMember = iamdomain.TenantMember
type MemberStatus = iamdomain.MemberStatus
type MemberFilter = iamdomain.MemberFilter
type Menu = iamdomain.Menu
type MenuStatus = iamdomain.MenuStatus
type OutboxStatus = iamdomain.OutboxStatus

const (
	OutboxPending    = iamdomain.OutboxPending
	OutboxProcessing = iamdomain.OutboxProcessing
	OutboxDelivered  = iamdomain.OutboxDelivered
	OutboxDead       = iamdomain.OutboxDead
)

const (
	MemberActive   = iamdomain.MemberActive
	MemberDisabled = iamdomain.MemberDisabled
	MenuActive     = iamdomain.MenuActive
	MenuDisabled   = iamdomain.MenuDisabled
)
