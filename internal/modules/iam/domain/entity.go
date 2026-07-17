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
	ErrMemberNotFound     = errors.New("iam tenant member not found")
	ErrMemberInactive     = errors.New("iam tenant member inactive")
	ErrMenuNotFound       = errors.New("iam menu not found")
	ErrMenuConflict       = errors.New("iam menu route already exists")
	ErrMenuHasChildren    = errors.New("iam menu has children")
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

type MemberStatus string

const (
	MemberActive   MemberStatus = "active"
	MemberDisabled MemberStatus = "disabled"
)

type TenantMember struct {
	TenantID    string
	Subject     string
	DisplayName string
	Email       string
	Status      MemberStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DisabledAt  time.Time
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
	ID             string
	TenantID       string
	ParentID       string
	Label          string
	Route          string
	Icon           string
	SortOrder      int
	PermissionCode string
	Status         MenuStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type OutboxStatus string

const (
	OutboxPending    OutboxStatus = "pending"
	OutboxProcessing OutboxStatus = "processing"
	OutboxDelivered  OutboxStatus = "delivered"
	OutboxDead       OutboxStatus = "dead"
)
