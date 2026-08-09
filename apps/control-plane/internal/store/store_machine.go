package store

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// Machine is one confirmed machine runner (PRD §5.3.1 / §16 machine_runners).
type Machine struct {
	bun.BaseModel   `bun:"table:machine_runners"`
	ID              string `bun:"id,pk"`
	InstanceID      string `bun:"instance_id,notnull,default:''"`
	Address         string `bun:"address,notnull,default:''"`
	Status          string `bun:"status,notnull,default:'confirmed'"`
	LastHeartbeatAt int64  `bun:"last_heartbeat_at,notnull,default:0"` // unix ms
	TokenHash       string `bun:"token_hash,notnull,default:''"`
	OS              string `bun:"os,notnull,default:''"`
	RegisteredAt    int64  `bun:"registered_at,notnull,default:0"` // unix ms,首次确认时间
}

// UpsertMachine confirms a machine (insert or update). Idempotent.
func (s *Store) UpsertMachine(ctx context.Context, m Machine) error {
	if m.RegisteredAt == 0 {
		m.RegisteredAt = time.Now().UnixMilli()
	}
	if _, err := s.db.NewInsert().Model(&m).
		On("CONFLICT (id) DO UPDATE").
		Set("address = EXCLUDED.address").
		Set("status = EXCLUDED.status").
		Set("last_heartbeat_at = EXCLUDED.last_heartbeat_at").
		Set("token_hash = EXCLUDED.token_hash").
		Set("os = EXCLUDED.os").
		// registered_at 只在首次写入,重连不刷新。SQLite 的 UPSERT 里裸列名即旧值,
		// 带表名前缀会被当成未知列。
		Set("registered_at = CASE WHEN registered_at = 0 THEN EXCLUDED.registered_at ELSE registered_at END").
		Exec(ctx); err != nil {
		return fmt.Errorf("upsert machine %s: %w", m.ID, err)
	}
	return nil
}

// ListMachines returns all confirmed machines (id ascending).
func (s *Store) ListMachines(ctx context.Context) ([]Machine, error) {
	out := []Machine{}
	if err := s.db.NewSelect().Model(&out).Order("id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list machines: %w", err)
	}
	return out, nil
}

// TouchMachineHeartbeat updates last_heartbeat_at (unix ms).
func (s *Store) TouchMachineHeartbeat(ctx context.Context, id string) error {
	if _, err := s.db.NewUpdate().Model(&Machine{}).
		Set("last_heartbeat_at = ?", time.Now().UnixMilli()).
		Where("id = ?", id).Exec(ctx); err != nil {
		return fmt.Errorf("touch machine %s: %w", id, err)
	}
	return nil
}

// UpdateMachineToken rotates the stored long-term token hash (manual rotation).
func (s *Store) UpdateMachineToken(ctx context.Context, id, tokenHash string) error {
	if _, err := s.db.NewUpdate().Model(&Machine{}).
		Set("token_hash = ?", tokenHash).
		Where("id = ?", id).Exec(ctx); err != nil {
		return fmt.Errorf("update machine token %s: %w", id, err)
	}
	return nil
}

// DeleteMachine removes a machine (cancel / force-offline).
func (s *Store) DeleteMachine(ctx context.Context, id string) error {
	if _, err := s.db.NewDelete().Model(&Machine{}).Where("id = ?", id).Exec(ctx); err != nil {
		return fmt.Errorf("delete machine %s: %w", id, err)
	}
	return nil
}
