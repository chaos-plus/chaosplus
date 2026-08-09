// Package store persists the control-plane's event log (PRD §15.1).
// Reuses apps/server's bunx (uptrace/bun datasource) + goosex (goose migrations).
package store

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/goosex"
	"github.com/uptrace/bun"
)

//go:embed sql/sqlite/*.sql
var migrations embed.FS

// Store is the control-plane StateStore (SQLite-first per PRD §16/C9).
type Store struct {
	db *bun.DB
}

// Open opens a SQLite datasource, runs migrations, returns the Store.
func Open(ctx context.Context, dsn string) (*Store, error) {
	ds := bunx.Datasource{Type: "sqlite", Dsn: dsn, Writable: true}
	db := ds.NewDB()
	if db == nil {
		return nil, fmt.Errorf("bunx: failed to open sqlite %q", dsn)
	}

	if err := goosex.Run(ctx, db.DB, migrations, "sqlite", "control_plane_migrations"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store migrations: %w", err)
	}
	slog.Info("control-plane store ready", "dsn", dsn)
	return &Store{db: db}, nil
}

// Event is one append-only row in the event log (PRD §16 events table).
type Event struct {
	Seq            int64  `bun:"seq,pk,autoincrement"`
	ID             string `bun:"id,notnull,unique"`
	InstanceID     string `bun:"instance_id,notnull,default:''"`
	RunID          string `bun:"run_id,notnull,default:''"`
	TS             int64  `bun:"ts,notnull"`
	Type           string `bun:"type,notnull"`
	IdempotencyKey string `bun:"idempotency_key,notnull,unique"`
	SchemaVersion  int    `bun:"schema_version,notnull,default:1"`
	PayloadJSON    string `bun:"payload_json,notnull,default:'{}'"`
}

// Append writes one event. Duplicate idempotency_key is a no-op (INSERT OR IGNORE).
func (s *Store) Append(ctx context.Context, e Event) error {
	e.TS = time.Now().UnixMilli()
	_, err := s.db.NewInsert().
		Model(&e).
		On("CONFLICT (idempotency_key) DO NOTHING").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("store append %s: %w", e.Type, err)
	}
	return nil
}

// ListEvents returns events for a run in seq order (optionally since a seq).
func (s *Store) ListEvents(ctx context.Context, runID string, sinceSeq int64, limit int) ([]Event, error) {
	out := []Event{}
	q := s.db.NewSelect().Model(&out).Where("run_id = ?", runID)
	if sinceSeq > 0 {
		q = q.Where("seq > ?", sinceSeq)
	}
	if err := q.Order("seq ASC").Limit(limit).Scan(ctx); err != nil {
		return nil, fmt.Errorf("store list events: %w", err)
	}
	return out, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}
