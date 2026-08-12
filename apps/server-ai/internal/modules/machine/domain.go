package machine

import (
	coreid "github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type Status string

const (
	StatusConfirmed Status = "confirmed"
	StatusOffline   Status = "offline"
)

// Machine is a confirmed runner owned by this bounded context.
type Machine struct {
	bun.BaseModel   `bun:"table:machine_runners"`
	ID              coreid.ID `bun:"id,pk" json:"id"`
	TenantID        coreid.ID `bun:"tenant_id,notnull" json:"tenantId"`
	Address         string    `bun:"address,notnull,default:''" json:"address"`
	Status          Status    `bun:"status,notnull,default:'confirmed'" json:"status"`
	LastHeartbeatAt int64     `bun:"last_heartbeat_at,notnull,default:0" json:"lastHeartbeatAt"`
	TokenHash       string    `bun:"token_hash,notnull,default:''" json:"-"`
	OS              string    `bun:"os,notnull,default:''" json:"os"`
	EntityID        coreid.ID `bun:"entity_id,notnull,default:0" json:"entityId"`
	OwnerID         coreid.ID `bun:"owner_id,notnull" json:"ownerId"`
	RegisteredAt    int64     `bun:"registered_at,notnull,default:0" json:"registeredAt"`
	CreatedAt       int64     `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy       coreid.ID `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt       int64     `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy       coreid.ID `bun:"updated_by,notnull" json:"updatedBy"`
	DeletedAt       int64     `bun:"deleted_at,notnull,default:0" json:"-"`
	DeletedBy       coreid.ID `bun:"deleted_by,notnull,default:0" json:"-"`
	Version         int64     `bun:"version,notnull,default:1" json:"version"`
}
