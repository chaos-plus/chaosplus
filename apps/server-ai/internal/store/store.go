// Package store persists the server-ai's event log (PRD §15.1).
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

// Store is the server-ai StateStore (SQLite-first per PRD §16/C9).
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
	slog.Info("server-ai store ready", "dsn", dsn)
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

// RunDef is a persisted workflow run (PRD §16 workflow_runs table).
type RunDef struct {
	bun.BaseModel `bun:"table:workflow_runs"`
	ID            string `bun:"id,pk"`
	DefJSON       string `bun:"def_json,notnull"`
	Status        string `bun:"status,notnull,default:'running'"`
	CreatedAt     int64  `bun:"created_at,notnull"`
	UpdatedAt     int64  `bun:"updated_at,notnull"`
}

// SaveRunDefinition upserts a workflow run (INSERT OR REPLACE).
func (s *Store) SaveRunDefinition(ctx context.Context, rd RunDef) error {
	rd.UpdatedAt = time.Now().UnixMilli()
	if rd.CreatedAt == 0 {
		rd.CreatedAt = rd.UpdatedAt
	}
	if rd.Status == "" {
		rd.Status = "running"
	}
	_, err := s.db.NewInsert().Model(&rd).On("CONFLICT (id) DO UPDATE").
		Set("status = EXCLUDED.status").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

// LoadActiveRunDefinitions returns runs that are not terminal.
func (s *Store) LoadActiveRunDefinitions(ctx context.Context) ([]RunDef, error) {
	var out []RunDef
	if err := s.db.NewSelect().Model(&out).
		Where("status NOT IN ('completed','failed')").
		Order("created_at DESC").
		Scan(ctx); err != nil {
		return nil, fmt.Errorf("store load active run definitions: %w", err)
	}
	return out, nil
}

// UpdateRunStatus sets a run's status.
func (s *Store) UpdateRunStatus(ctx context.Context, runID, status string) error {
	_, err := s.db.NewUpdate().Model((*RunDef)(nil)).
		Set("status = ?", status).
		Set("updated_at = ?", time.Now().UnixMilli()).
		Where("id = ?", runID).
		Exec(ctx)
	return err
}

// WorkflowDefModel is a persisted workflow definition (PRD §16 workflows table).
type WorkflowDefModel struct {
	bun.BaseModel `bun:"table:workflows"`
	ID            string `bun:"id,pk" json:"id"`
	Version       string `bun:"version,pk" json:"version"`
	Name          string `bun:"name,notnull" json:"name"`
	DefJSON       string `bun:"def_json,notnull" json:"-"`
	DefRaw        any    `bun:"-" json:"def,omitempty"`
	InstanceID    string `bun:"instance_id,notnull" json:"instanceId"`
	OwnerID       string `bun:"owner_id,notnull" json:"ownerId"`
	CreatedAt     int64  `bun:"created_at,notnull" json:"createdAt"`
	UpdatedAt     int64  `bun:"updated_at,notnull" json:"updatedAt"`
}

// SaveWorkflow upserts a workflow definition.
func (s *Store) SaveWorkflow(ctx context.Context, m WorkflowDefModel) error {
	now := time.Now().UnixMilli()
	if m.CreatedAt == 0 {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	if m.Version == "" {
		m.Version = "1"
	}
	_, err := s.db.NewInsert().Model(&m).On("CONFLICT (id, version) DO UPDATE").
		Set("name = EXCLUDED.name").
		Set("def_json = EXCLUDED.def_json").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

// ListWorkflows returns all workflows (newest version first).
func (s *Store) ListWorkflows(ctx context.Context, instanceID string) ([]WorkflowDefModel, error) {
	var out []WorkflowDefModel
	q := s.db.NewSelect().Model(&out).Order("updated_at DESC")
	if instanceID != "" {
		q = q.Where("instance_id = ?", instanceID)
	}
	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("store list workflows: %w", err)
	}
	return out, nil
}

// GetWorkflow fetches one workflow by id+version.
func (s *Store) GetWorkflow(ctx context.Context, id, version string) (*WorkflowDefModel, error) {
	m := &WorkflowDefModel{}
	if err := s.db.NewSelect().Model(m).Where("id = ? AND version = ?", id, version).Scan(ctx); err != nil {
		return nil, fmt.Errorf("store get workflow %s@%s: %w", id, version, err)
	}
	return m, nil
}

// DeleteWorkflow removes all versions of a workflow.
func (s *Store) DeleteWorkflow(ctx context.Context, id string) error {
	_, err := s.db.NewDelete().Model((*WorkflowDefModel)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}
