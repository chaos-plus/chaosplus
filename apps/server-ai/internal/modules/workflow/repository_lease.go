package workflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

var (
	ErrLeaseHeld  = errors.New("run lease is held by another owner")
	ErrLeaseStale = errors.New("run lease is expired or fenced")
)

// RunLease is the single-writer credential for one run. FencingToken is
// monotonically incremented on every acquisition, including reacquisition by
// the same process, so writes from an older engine are rejected by StateStore.
type RunLease struct {
	bun.BaseModel `bun:"table:run_leases"`
	TenantID      guid.ID `bun:"tenant_id,pk"`
	EntityID      guid.ID `bun:"entity_id,pk"`
	RunID         guid.ID `bun:"run_id,pk"`
	HolderID      guid.ID `bun:"holder_id,notnull"`
	FencingToken  int64   `bun:"fencing_token,notnull"`
	ExpiresAt     int64   `bun:"expires_at,notnull"`
	UpdatedAt     int64   `bun:"updated_at,notnull"`
}

func (s *BunRepository) AcquireRunLease(ctx context.Context, runID, holderID guid.ID, ttl time.Duration) (RunLease, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return RunLease{}, err
	}
	if runID.Zero() || holderID.Zero() || ttl <= 0 {
		return RunLease{}, fmt.Errorf("acquire run lease: run id, holder id, and positive ttl are required")
	}
	var acquired RunLease
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		now := time.Now().UnixMilli()
		var current RunLease
		err := tx.NewSelect().Model(&current).Where("tenant_id = ? AND entity_id = ? AND run_id = ?", claims.TenantID, claims.EntityID, runID).Scan(ctx)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			acquired = RunLease{TenantID: claims.TenantID, EntityID: claims.EntityID, RunID: runID, HolderID: holderID, FencingToken: 1,
				ExpiresAt: now + ttl.Milliseconds(), UpdatedAt: now}
			if _, err := tx.NewInsert().Model(&acquired).Exec(ctx); err != nil {
				return fmt.Errorf("insert run lease: %w", err)
			}
			return nil
		case err != nil:
			return fmt.Errorf("load run lease: %w", err)
		case current.ExpiresAt > now && current.HolderID != holderID:
			return ErrLeaseHeld
		}
		acquired = RunLease{TenantID: claims.TenantID, EntityID: claims.EntityID, RunID: runID, HolderID: holderID, FencingToken: current.FencingToken + 1,
			ExpiresAt: now + ttl.Milliseconds(), UpdatedAt: now}
		res, err := tx.NewUpdate().Model((*RunLease)(nil)).
			Set("holder_id = ?", holderID).
			Set("fencing_token = ?", acquired.FencingToken).
			Set("expires_at = ?", acquired.ExpiresAt).
			Set("updated_at = ?", now).
			Where("tenant_id = ? AND entity_id = ? AND run_id = ? AND fencing_token = ?", claims.TenantID, claims.EntityID, runID, current.FencingToken).Exec(ctx)
		if err != nil {
			return fmt.Errorf("replace run lease: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil || n != 1 {
			return ErrLeaseHeld
		}
		return nil
	})
	if err != nil {
		return RunLease{}, fmt.Errorf("acquire run lease %s: %w", runID, err)
	}
	return acquired, nil
}

func (s *BunRepository) RenewRunLease(ctx context.Context, lease RunLease, ttl time.Duration) (RunLease, error) {
	now := time.Now().UnixMilli()
	expiresAt := now + ttl.Milliseconds()
	res, err := s.db.NewUpdate().Model((*RunLease)(nil)).
		Set("expires_at = ?", expiresAt).
		Set("updated_at = ?", now).
		Where("tenant_id = ? AND entity_id = ? AND run_id = ? AND holder_id = ? AND fencing_token = ? AND expires_at > ?",
			lease.TenantID, lease.EntityID, lease.RunID, lease.HolderID, lease.FencingToken, now).Exec(ctx)
	if err != nil {
		return RunLease{}, fmt.Errorf("renew run lease %s: %w", lease.RunID, err)
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return RunLease{}, fmt.Errorf("renew run lease %s: %w", lease.RunID, ErrLeaseStale)
	}
	lease.ExpiresAt, lease.UpdatedAt = expiresAt, now
	return lease, nil
}

func (s *BunRepository) ReleaseRunLease(ctx context.Context, lease RunLease) error {
	now := time.Now().UnixMilli()
	_, err := s.db.NewUpdate().Model((*RunLease)(nil)).
		Set("expires_at = ?", now).
		Set("updated_at = ?", now).
		Where("tenant_id = ? AND entity_id = ? AND run_id = ? AND holder_id = ? AND fencing_token = ?",
			lease.TenantID, lease.EntityID, lease.RunID, lease.HolderID, lease.FencingToken).Exec(ctx)
	if err != nil {
		return fmt.Errorf("release run lease %s: %w", lease.RunID, err)
	}
	return nil
}

// CommitRunEvents atomically verifies the run's fencing token, appends every
// event, and applies its rebuildable projections. A duplicate idempotency key
// is a no-op for both the event and projection.
func (s *BunRepository) CommitRunEvents(ctx context.Context, lease RunLease, events []EventRecord) error {
	if len(events) == 0 {
		return nil
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		now := time.Now().UnixMilli()
		count, err := tx.NewSelect().Model((*RunLease)(nil)).
			Where("tenant_id = ? AND entity_id = ? AND run_id = ? AND holder_id = ? AND fencing_token = ? AND expires_at > ?",
				lease.TenantID, lease.EntityID, lease.RunID, lease.HolderID, lease.FencingToken, now).Count(ctx)
		if err != nil {
			return fmt.Errorf("validate run lease: %w", err)
		}
		if count != 1 {
			return ErrLeaseStale
		}
		for i := range events {
			e := events[i]
			if e.RunID != lease.RunID {
				return fmt.Errorf("event %s belongs to run %s, lease is for %s", e.ID, e.RunID, lease.RunID)
			}
			if e.TS == 0 {
				e.TS = now
			}
			res, err := tx.NewInsert().Model(&e).
				On("CONFLICT (idempotency_key) DO NOTHING").Exec(ctx)
			if err != nil {
				return fmt.Errorf("append event %s: %w", e.ID, err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return fmt.Errorf("append event %s rows affected: %w", e.ID, err)
			}
			if n == 0 {
				continue
			}
			if err := replayCoreEvent(ctx, tx, e); err != nil {
				return fmt.Errorf("project event %s (%s): %w", e.ID, e.Type, err)
			}
			for _, projector := range s.projectors {
				if err := projector.Project(ctx, tx, e); err != nil {
					return fmt.Errorf("project event %s (%s): %w", e.ID, e.Type, err)
				}
			}
		}
		return nil
	})
}
