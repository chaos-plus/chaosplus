package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
	RunID         string `bun:"run_id,pk"`
	OwnerID       string `bun:"owner_id,notnull"`
	FencingToken  int64  `bun:"fencing_token,notnull"`
	ExpiresAt     int64  `bun:"expires_at,notnull"`
	UpdatedAt     int64  `bun:"updated_at,notnull"`
}

func (s *Store) AcquireRunLease(ctx context.Context, runID, ownerID string, ttl time.Duration) (RunLease, error) {
	if runID == "" || ownerID == "" || ttl <= 0 {
		return RunLease{}, fmt.Errorf("acquire run lease: runID, ownerID and positive ttl are required")
	}
	var acquired RunLease
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		now := time.Now().UnixMilli()
		var current RunLease
		err := tx.NewSelect().Model(&current).Where("run_id = ?", runID).Scan(ctx)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			acquired = RunLease{RunID: runID, OwnerID: ownerID, FencingToken: 1,
				ExpiresAt: now + ttl.Milliseconds(), UpdatedAt: now}
			if _, err := tx.NewInsert().Model(&acquired).Exec(ctx); err != nil {
				return fmt.Errorf("insert run lease: %w", err)
			}
			return nil
		case err != nil:
			return fmt.Errorf("load run lease: %w", err)
		case current.ExpiresAt > now && current.OwnerID != ownerID:
			return ErrLeaseHeld
		}
		acquired = RunLease{RunID: runID, OwnerID: ownerID, FencingToken: current.FencingToken + 1,
			ExpiresAt: now + ttl.Milliseconds(), UpdatedAt: now}
		res, err := tx.NewUpdate().Model((*RunLease)(nil)).
			Set("owner_id = ?", ownerID).
			Set("fencing_token = ?", acquired.FencingToken).
			Set("expires_at = ?", acquired.ExpiresAt).
			Set("updated_at = ?", now).
			Where("run_id = ? AND fencing_token = ?", runID, current.FencingToken).Exec(ctx)
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

func (s *Store) RenewRunLease(ctx context.Context, lease RunLease, ttl time.Duration) (RunLease, error) {
	now := time.Now().UnixMilli()
	expiresAt := now + ttl.Milliseconds()
	res, err := s.db.NewUpdate().Model((*RunLease)(nil)).
		Set("expires_at = ?", expiresAt).
		Set("updated_at = ?", now).
		Where("run_id = ? AND owner_id = ? AND fencing_token = ? AND expires_at > ?",
			lease.RunID, lease.OwnerID, lease.FencingToken, now).Exec(ctx)
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

func (s *Store) ReleaseRunLease(ctx context.Context, lease RunLease) error {
	now := time.Now().UnixMilli()
	_, err := s.db.NewUpdate().Model((*RunLease)(nil)).
		Set("expires_at = ?", now).
		Set("updated_at = ?", now).
		Where("run_id = ? AND owner_id = ? AND fencing_token = ?",
			lease.RunID, lease.OwnerID, lease.FencingToken).Exec(ctx)
	if err != nil {
		return fmt.Errorf("release run lease %s: %w", lease.RunID, err)
	}
	return nil
}

// CommitRunEvents atomically verifies the run's fencing token, appends every
// event, and applies its rebuildable projections. A duplicate idempotency key
// is a no-op for both the event and projection.
func (s *Store) CommitRunEvents(ctx context.Context, lease RunLease, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		now := time.Now().UnixMilli()
		count, err := tx.NewSelect().Model((*RunLease)(nil)).
			Where("run_id = ? AND owner_id = ? AND fencing_token = ? AND expires_at > ?",
				lease.RunID, lease.OwnerID, lease.FencingToken, now).Count(ctx)
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
		}
		return nil
	})
}

// CommitArtifactEvent atomically appends one non-run-scheduler artifact event
// and applies its projection. Reconciliation and human force-validation cannot
// use a run lease because they may occur after the producing run is terminal.
func (s *Store) CommitArtifactEvent(ctx context.Context, event Event) error {
	_, err := s.commitArtifactEvent(ctx, event, "", false)
	return err
}

// CommitArtifactEventIfChecksum commits a reconciliation event only when the
// artifact still has the checksum observed before the external read. This
// prevents a slow reconciliation pass from overwriting a newer producer write.
func (s *Store) CommitArtifactEventIfChecksum(ctx context.Context, event Event, expectedChecksum string) (bool, error) {
	if expectedChecksum == "" {
		return false, errors.New("commit artifact event: expected checksum is required")
	}
	return s.commitArtifactEvent(ctx, event, expectedChecksum, true)
}

func (s *Store) commitArtifactEvent(ctx context.Context, event Event, expectedChecksum string, compareChecksum bool) (bool, error) {
	switch event.Type {
	case "ARTIFACT_INVALIDATED", "ARTIFACT_STALE_MARKED", "ARTIFACT_ORPHANED", "ARTIFACT_FORCE_VALIDATED":
	default:
		return false, fmt.Errorf("commit artifact event: unsupported type %q", event.Type)
	}
	if event.ID == "" || event.IdempotencyKey == "" {
		return false, fmt.Errorf("commit artifact event: id and idempotency key are required")
	}
	artifactID, err := artifactIDFromEvent(event)
	if err != nil {
		return false, err
	}
	committed := false
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if compareChecksum {
			var checksum string
			if err := tx.NewSelect().Model((*Artifact)(nil)).Column("checksum").Where("id = ?", artifactID).Scan(ctx, &checksum); err != nil {
				return fmt.Errorf("load artifact checksum: %w", err)
			}
			if checksum != expectedChecksum {
				return nil
			}
		}
		if event.TS == 0 {
			event.TS = time.Now().UnixMilli()
		}
		res, err := tx.NewInsert().Model(&event).
			On("CONFLICT (idempotency_key) DO NOTHING").Exec(ctx)
		if err != nil {
			return fmt.Errorf("append artifact event: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("append artifact event rows affected: %w", err)
		}
		if n == 0 {
			return nil
		}
		if err := replayCoreEvent(ctx, tx, event); err != nil {
			return fmt.Errorf("project artifact event: %w", err)
		}
		committed = true
		return nil
	})
	return committed, err
}

func artifactIDFromEvent(event Event) (string, error) {
	var artifact Artifact
	if json.Unmarshal([]byte(event.PayloadJSON), &artifact) == nil && artifact.ID != "" {
		return artifact.ID, nil
	}
	var envelope replayArtifactEvent
	if json.Unmarshal([]byte(event.PayloadJSON), &envelope) == nil && envelope.Artifact.ID != "" {
		return envelope.Artifact.ID, nil
	}
	return "", errors.New("commit artifact event: payload artifact id is required")
}
