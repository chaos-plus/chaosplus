package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

const (
	ArtifactValid    = "valid"
	ArtifactStale    = "stale"
	ArtifactInvalid  = "invalid"
	ArtifactOrphaned = "orphaned"
)

type Artifact struct {
	bun.BaseModel `bun:"table:artifacts"`
	ID            string `bun:"id,pk" json:"id"`
	InstanceID    string `bun:"instance_id,notnull" json:"instanceId"`
	ProjectID     string `bun:"project_id,notnull" json:"projectId"`
	LogicalID     string `bun:"logical_id,notnull" json:"logicalId"`
	LogicalPath   string `bun:"logical_path,notnull" json:"logicalPath"`
	Type          string `bun:"type,notnull" json:"type"`
	Checksum      string `bun:"checksum,notnull" json:"checksum"`
	SizeBytes     int64  `bun:"size_bytes,notnull" json:"sizeBytes"`
	Status        string `bun:"status,notnull" json:"status"`
	ForceValid    bool   `bun:"force_valid,notnull" json:"forceValid"`
	ProducerRunID string `bun:"producer_run_id,notnull" json:"producerRunId"`
	ProducerNode  string `bun:"producer_node_id,notnull" json:"producerNodeId"`
	Attempt       int    `bun:"attempt,notnull" json:"attempt"`
	RunnerID      string `bun:"runner_id,notnull" json:"runnerId"`
	SpawnID       string `bun:"spawn_id,notnull" json:"spawnId"`
	CreatedAt     int64  `bun:"created_at,notnull" json:"createdAt"`
	UpdatedAt     int64  `bun:"updated_at,notnull" json:"updatedAt"`
}

type ArtifactDep struct {
	bun.BaseModel `bun:"table:artifact_deps"`
	ArtifactID    string `bun:"artifact_id,pk" json:"artifactId"`
	DependsOnID   string `bun:"depends_on_id,pk" json:"dependsOnId"`
	InputChecksum string `bun:"input_checksum,notnull" json:"inputChecksum"`
	CreatedAt     int64  `bun:"created_at,notnull" json:"createdAt"`
}

func (s *Store) UpsertArtifact(ctx context.Context, artifact Artifact, deps []Artifact) error {
	now := time.Now().UnixMilli()
	artifact.UpdatedAt = now
	if artifact.CreatedAt == 0 {
		artifact.CreatedAt = now
	}
	if artifact.Status == "" {
		artifact.Status = ArtifactValid
	}
	if artifact.Attempt < 1 {
		artifact.Attempt = 1
	}

	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var previous Artifact
		err := tx.NewSelect().Model(&previous).Where("id = ?", artifact.ID).Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("load previous artifact: %w", err)
		}
		if err == nil {
			artifact.CreatedAt = previous.CreatedAt
			if previous.Checksum != artifact.Checksum {
				if err := markArtifactDependentsStale(ctx, tx, artifact.ID); err != nil {
					return err
				}
			}
		}

		_, err = tx.NewInsert().Model(&artifact).On("CONFLICT (id) DO UPDATE").
			Set("logical_id = EXCLUDED.logical_id").
			Set("logical_path = EXCLUDED.logical_path").
			Set("type = EXCLUDED.type").
			Set("checksum = EXCLUDED.checksum").
			Set("size_bytes = EXCLUDED.size_bytes").
			Set("status = EXCLUDED.status").
			Set("force_valid = 0").
			Set("producer_run_id = EXCLUDED.producer_run_id").
			Set("producer_node_id = EXCLUDED.producer_node_id").
			Set("attempt = EXCLUDED.attempt").
			Set("runner_id = EXCLUDED.runner_id").
			Set("spawn_id = EXCLUDED.spawn_id").
			Set("updated_at = EXCLUDED.updated_at").Exec(ctx)
		if err != nil {
			return fmt.Errorf("upsert artifact: %w", err)
		}
		if _, err := tx.NewDelete().Model((*ArtifactDep)(nil)).Where("artifact_id = ?", artifact.ID).Exec(ctx); err != nil {
			return fmt.Errorf("replace artifact dependencies: %w", err)
		}
		for _, dep := range deps {
			edge := ArtifactDep{ArtifactID: artifact.ID, DependsOnID: dep.ID, InputChecksum: dep.Checksum, CreatedAt: now}
			if _, err := tx.NewInsert().Model(&edge).Exec(ctx); err != nil {
				return fmt.Errorf("insert artifact dependency: %w", err)
			}
		}
		return nil
	})
}

func (s *Store) ListArtifacts(ctx context.Context, instanceID, projectID, status string) ([]Artifact, error) {
	out := []Artifact{}
	q := s.db.NewSelect().Model(&out)
	if instanceID != "" {
		q = q.Where("instance_id = ?", instanceID)
	}
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Order("updated_at DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	return out, nil
}

func (s *Store) GetArtifact(ctx context.Context, id, instanceID string) (Artifact, error) {
	var artifact Artifact
	q := s.db.NewSelect().Model(&artifact).Where("id = ?", id)
	if instanceID != "" {
		q = q.Where("instance_id = ?", instanceID)
	}
	if err := q.Scan(ctx); err != nil {
		return artifact, fmt.Errorf("get artifact: %w", err)
	}
	return artifact, nil
}

func (s *Store) ListArtifactDependencies(ctx context.Context, artifactID string) ([]ArtifactDep, error) {
	out := []ArtifactDep{}
	if err := s.db.NewSelect().Model(&out).Where("artifact_id = ?", artifactID).Order("depends_on_id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list artifact dependencies: %w", err)
	}
	return out, nil
}

func (s *Store) ResolveArtifacts(ctx context.Context, instanceID, projectID string, logicalIDs []string) ([]Artifact, error) {
	if len(logicalIDs) == 0 {
		return nil, nil
	}
	out := []Artifact{}
	err := s.db.NewSelect().Model(&out).
		Where("instance_id = ? AND project_id = ?", instanceID, projectID).
		Where("logical_id IN (?)", bun.List(logicalIDs)).
		Where("status = ?", ArtifactValid).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve artifacts: %w", err)
	}
	return out, nil
}

func (s *Store) ReconcileArtifact(ctx context.Context, id, observedChecksum string, observedSize int64) (bool, error) {
	changed := false
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var artifact Artifact
		if err := tx.NewSelect().Model(&artifact).Where("id = ?", id).Scan(ctx); err != nil {
			return fmt.Errorf("load artifact for reconciliation: %w", err)
		}
		if artifact.Checksum == observedChecksum {
			return nil
		}
		changed = true
		if _, err := tx.NewUpdate().Model((*Artifact)(nil)).
			Set("checksum = ?", observedChecksum).
			Set("size_bytes = ?", observedSize).
			Set("status = ?", ArtifactInvalid).
			Set("force_valid = 0").
			Set("updated_at = ?", time.Now().UnixMilli()).
			Where("id = ?", id).Exec(ctx); err != nil {
			return fmt.Errorf("invalidate changed artifact: %w", err)
		}
		return markArtifactDependentsStale(ctx, tx, id)
	})
	return changed, err
}

func (s *Store) MarkArtifactOrphaned(ctx context.Context, id string) error {
	_, err := s.db.NewUpdate().Model((*Artifact)(nil)).
		Set("status = ?", ArtifactOrphaned).
		Set("force_valid = 0").
		Set("updated_at = ?", time.Now().UnixMilli()).
		Where("id = ?", id).
		Where("status != ?", ArtifactOrphaned).Exec(ctx)
	return err
}

func (s *Store) ForceValidateArtifact(ctx context.Context, id, reviewer string) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().Model((*Artifact)(nil)).
			Set("status = ?", ArtifactValid).
			Set("force_valid = 1").
			Set("updated_at = ?", time.Now().UnixMilli()).
			Where("id = ?", id).Exec(ctx)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("artifact not found")
		}
		validation := ValidationResult{
			ID: fmt.Sprintf("force-%d-%s", time.Now().UnixNano(), id), ArtifactID: id,
			ValidatorID: "force_valid", ValidatorType: "human", Passed: 1,
			EvidenceJSON: `{"forceValid":true}`, ReviewedBy: reviewer, TS: time.Now().UnixMilli(),
		}
		_, err = tx.NewInsert().Model(&validation).Exec(ctx)
		return err
	})
}

func markArtifactDependentsStale(ctx context.Context, tx bun.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `
WITH RECURSIVE downstream(id) AS (
  SELECT artifact_id FROM artifact_deps WHERE depends_on_id = ?
  UNION
  SELECT d.artifact_id FROM artifact_deps d JOIN downstream x ON d.depends_on_id = x.id
)
UPDATE artifacts
SET status = ?, force_valid = 0, updated_at = ?
WHERE id IN (SELECT id FROM downstream)`, id, ArtifactStale, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("propagate stale artifacts: %w", err)
	}
	return nil
}
