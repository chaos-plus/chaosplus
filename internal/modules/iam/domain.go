package iam

import iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"

var (
	ErrRoleNotFound       = iamdomain.ErrRoleNotFound
	ErrRoleNameConflict   = iamdomain.ErrRoleNameConflict
	ErrInvalidArgument    = iamdomain.ErrInvalidArgument
	ErrPermissionNotFound = iamdomain.ErrPermissionNotFound
)

type IDGenerator = iamdomain.IDGenerator
type Role = iamdomain.Role
type OutboxStatus = iamdomain.OutboxStatus

const (
	OutboxPending    = iamdomain.OutboxPending
	OutboxProcessing = iamdomain.OutboxProcessing
	OutboxDelivered  = iamdomain.OutboxDelivered
	OutboxDead       = iamdomain.OutboxDead
)
