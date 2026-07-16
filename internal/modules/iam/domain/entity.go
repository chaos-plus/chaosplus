package domain

import (
	"errors"
	"time"
)

var (
	ErrRoleNotFound       = errors.New("iam role not found")
	ErrRoleNameConflict   = errors.New("iam role name already exists")
	ErrInvalidArgument    = errors.New("invalid iam argument")
	ErrPermissionNotFound = errors.New("iam permission not found")
)

type IDGenerator func() (string, error)

type Role struct {
	ID          string
	TenantID    string
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type OutboxStatus string

const (
	OutboxPending    OutboxStatus = "pending"
	OutboxProcessing OutboxStatus = "processing"
	OutboxDelivered  OutboxStatus = "delivered"
	OutboxDead       OutboxStatus = "dead"
)
