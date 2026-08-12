package artifact

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

const (
	ArtifactValid    = "valid"
	ArtifactStale    = "stale"
	ArtifactInvalid  = "invalid"
	ArtifactOrphaned = "orphaned"
)

var ErrArtifactNotFound = errors.New("artifact not found")

type Artifact struct {
	bun.BaseModel   `bun:"table:artifacts"`
	ID              guid.ID `bun:"id,pk" json:"id"`
	TenantID        guid.ID `bun:"tenant_id,notnull" json:"tenantId"`
	EntityID        guid.ID `bun:"entity_id,notnull" json:"entityId"`
	ProjectID       guid.ID `bun:"project_id,notnull" json:"projectId"`
	OwnerID         guid.ID `bun:"owner_id,notnull" json:"ownerId"`
	LogicalKey      string  `bun:"logical_key,notnull" json:"logicalKey"`
	LogicalPath     string  `bun:"logical_path,notnull" json:"logicalPath"`
	Type            string  `bun:"type,notnull" json:"type"`
	Checksum        string  `bun:"checksum,notnull" json:"checksum"`
	SizeBytes       int64   `bun:"size_bytes,notnull" json:"sizeBytes"`
	Status          string  `bun:"status,notnull" json:"status"`
	ForceValid      bool    `bun:"force_valid,notnull" json:"forceValid"`
	ProducerRunID   guid.ID `bun:"producer_run_id,notnull" json:"producerRunId"`
	ProducerNodeKey string  `bun:"producer_node_key,notnull" json:"producerNodeKey"`
	Attempt         int     `bun:"attempt,notnull" json:"attempt"`
	RunnerHandle    string  `bun:"runner_handle,notnull" json:"runnerHandle"`
	SpawnHandle     string  `bun:"spawn_handle,notnull" json:"spawnHandle"`
	CreatedAt       int64   `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy       guid.ID `bun:"created_by,notnull" json:"createdBy"`
	UpdatedAt       int64   `bun:"updated_at,notnull" json:"updatedAt"`
	UpdatedBy       guid.ID `bun:"updated_by,notnull" json:"updatedBy"`
	DeletedAt       int64   `bun:"deleted_at,notnull" json:"-"`
	DeletedBy       guid.ID `bun:"deleted_by,notnull" json:"-"`
	Version         int64   `bun:"version,notnull" json:"version"`
}

type ArtifactDep struct {
	bun.BaseModel `bun:"table:artifact_deps"`
	TenantID      guid.ID `bun:"tenant_id,pk" json:"tenantId"`
	EntityID      guid.ID `bun:"entity_id,pk" json:"entityId"`
	ArtifactID    guid.ID `bun:"artifact_id,pk" json:"artifactId"`
	DependsOnID   guid.ID `bun:"depends_on_id,pk" json:"dependsOnId"`
	InputChecksum string  `bun:"input_checksum,notnull" json:"inputChecksum"`
	CreatedAt     int64   `bun:"created_at,notnull" json:"createdAt"`
	CreatedBy     guid.ID `bun:"created_by,notnull" json:"createdBy"`
}

type BunRepository struct {
	db     *bun.DB
	nextID func() (guid.ID, error)
}

func NewBunRepository(db *bun.DB, nextID func() (guid.ID, error)) *BunRepository {
	if db == nil || nextID == nil {
		panic("artifact repository requires database and id generator")
	}
	return &BunRepository{db: db, nextID: nextID}
}

func (s *BunRepository) UpsertArtifact(ctx context.Context, artifact Artifact, deps []Artifact) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	if artifact.ID.Zero() {
		artifact.ID, err = s.nextID()
		if err != nil {
			return fmt.Errorf("generate artifact id: %w", err)
		}
	}
	if artifact.TenantID.Zero() {
		artifact.TenantID = claims.TenantID
	}
	if artifact.EntityID.Zero() {
		artifact.EntityID = claims.EntityID
	}
	if artifact.OwnerID.Zero() {
		artifact.OwnerID = claims.PrincipalID
	}
	if artifact.TenantID != claims.TenantID || artifact.EntityID != claims.EntityID || artifact.OwnerID != claims.PrincipalID {
		return fmt.Errorf("upsert artifact: ownership scope does not match authenticated claims")
	}
	now := time.Now().UTC().UnixMilli()
	artifact.UpdatedAt = now
	if artifact.CreatedAt == 0 {
		artifact.CreatedAt = now
	}
	if artifact.CreatedBy.Zero() {
		artifact.CreatedBy = claims.PrincipalID
	}
	artifact.UpdatedBy = claims.PrincipalID
	if artifact.Version < 1 {
		artifact.Version = 1
	}
	if artifact.Status == "" {
		artifact.Status = ArtifactValid
	}
	if artifact.Attempt < 1 {
		artifact.Attempt = 1
	}

	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var previous Artifact
		err := tx.NewSelect().Model(&previous).
			Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", artifact.ID, claims.TenantID, claims.EntityID).
			Scan(ctx)
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

		result, err := tx.NewUpdate().Model((*Artifact)(nil)).
			Set("logical_key = ?", artifact.LogicalKey).
			Set("logical_path = ?", artifact.LogicalPath).
			Set("type = ?", artifact.Type).
			Set("checksum = ?", artifact.Checksum).
			Set("size_bytes = ?", artifact.SizeBytes).
			Set("status = ?", artifact.Status).
			Set("force_valid = ?", false).
			Set("producer_run_id = ?", artifact.ProducerRunID).
			Set("producer_node_key = ?", artifact.ProducerNodeKey).
			Set("attempt = ?", artifact.Attempt).
			Set("runner_handle = ?", artifact.RunnerHandle).
			Set("spawn_handle = ?", artifact.SpawnHandle).
			Set("updated_at = ?", artifact.UpdatedAt).
			Set("updated_by = ?", artifact.UpdatedBy).
			Set("version = version + 1").
			Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", artifact.ID, claims.TenantID, claims.EntityID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("upsert artifact: %w", err)
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("inspect artifact update: %w", err)
		}
		if updated == 0 {
			if _, err := tx.NewInsert().Model(&artifact).Exec(ctx); err != nil {
				return fmt.Errorf("insert artifact: %w", err)
			}
		}
		if _, err := tx.NewDelete().Model((*ArtifactDep)(nil)).
			Where("tenant_id = ? AND entity_id = ? AND artifact_id = ?", claims.TenantID, claims.EntityID, artifact.ID).Exec(ctx); err != nil {
			return fmt.Errorf("replace artifact dependencies: %w", err)
		}
		for _, dep := range deps {
			edge := ArtifactDep{TenantID: artifact.TenantID, EntityID: artifact.EntityID, ArtifactID: artifact.ID, DependsOnID: dep.ID, InputChecksum: dep.Checksum, CreatedAt: now, CreatedBy: artifact.UpdatedBy}
			if _, err := tx.NewInsert().Model(&edge).Exec(ctx); err != nil {
				return fmt.Errorf("insert artifact dependency: %w", err)
			}
		}
		return nil
	})
}

func (s *BunRepository) ListArtifacts(ctx context.Context, entityID, projectID guid.ID, status string) ([]Artifact, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	if !entityID.Zero() && entityID != claims.EntityID {
		return nil, fmt.Errorf("list artifacts: requested entity does not match authenticated claims")
	}
	out := []Artifact{}
	q := s.db.NewSelect().Model(&out).
		Where("tenant_id = ? AND entity_id = ? AND deleted_at = 0", claims.TenantID, claims.EntityID)
	if !projectID.Zero() {
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

func (s *BunRepository) GetArtifact(ctx context.Context, id, entityID guid.ID) (Artifact, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return Artifact{}, err
	}
	if !entityID.Zero() && entityID != claims.EntityID {
		return Artifact{}, fmt.Errorf("get artifact: requested entity does not match authenticated claims")
	}
	var artifact Artifact
	q := s.db.NewSelect().Model(&artifact).
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", id, claims.TenantID, claims.EntityID)
	if err := q.Scan(ctx); err != nil {
		return artifact, fmt.Errorf("get artifact: %w", err)
	}
	return artifact, nil
}

func (s *BunRepository) ListArtifactDependencies(ctx context.Context, artifactID guid.ID) ([]ArtifactDep, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	out := []ArtifactDep{}
	if err := s.db.NewSelect().Model(&out).
		Where("tenant_id = ? AND entity_id = ? AND artifact_id = ?", claims.TenantID, claims.EntityID, artifactID).
		Order("depends_on_id ASC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list artifact dependencies: %w", err)
	}
	return out, nil
}

func (s *BunRepository) ResolveArtifacts(ctx context.Context, entityID, projectID guid.ID, logicalKeys []string) ([]Artifact, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return nil, err
	}
	if entityID != claims.EntityID {
		return nil, fmt.Errorf("resolve artifacts: requested entity does not match authenticated claims")
	}
	if len(logicalKeys) == 0 {
		return nil, nil
	}
	out := []Artifact{}
	err = s.db.NewSelect().Model(&out).
		Where("tenant_id = ? AND entity_id = ? AND project_id = ? AND deleted_at = 0", claims.TenantID, entityID, projectID).
		Where("logical_key IN (?)", bun.List(logicalKeys)).
		Where("status = ?", ArtifactValid).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve artifacts: %w", err)
	}
	return out, nil
}

func (s *BunRepository) ReconcileArtifactIfChecksum(ctx context.Context, id guid.ID, expectedChecksum, observedChecksum string, observedSize int64) (bool, error) {
	claims, err := requireClaims(ctx)
	if err != nil {
		return false, err
	}
	changed := false
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*Artifact)(nil)).
			Set("checksum = ?", observedChecksum).
			Set("size_bytes = ?", observedSize).
			Set("status = ?", ArtifactInvalid).
			Set("force_valid = ?", false).
			Set("updated_at = ?", time.Now().UTC().UnixMilli()).
			Set("updated_by = ?", claims.PrincipalID).
			Set("version = version + 1").
			Where("id = ? AND tenant_id = ? AND entity_id = ? AND checksum = ? AND deleted_at = 0", id, claims.TenantID, claims.EntityID, expectedChecksum).Exec(ctx)
		if err != nil {
			return fmt.Errorf("invalidate changed artifact: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil || rows == 0 {
			return err
		}
		changed = true
		return markArtifactDependentsStale(ctx, tx, id)
	})
	return changed, err
}

func (s *BunRepository) MarkArtifactOrphanedIfChecksum(ctx context.Context, id guid.ID, expectedChecksum string) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.NewUpdate().Model((*Artifact)(nil)).
		Set("status = ?", ArtifactOrphaned).
		Set("force_valid = ?", false).
		Set("updated_at = ?", time.Now().UTC().UnixMilli()).
		Set("updated_by = ?", claims.PrincipalID).
		Set("version = version + 1").
		Where("id = ? AND tenant_id = ? AND entity_id = ? AND checksum = ? AND deleted_at = 0", id, claims.TenantID, claims.EntityID, expectedChecksum).
		Where("status != ?", ArtifactOrphaned).Exec(ctx)
	return err
}

func (s *BunRepository) ForceValidateArtifact(ctx context.Context, id, reviewer guid.ID) error {
	claims, err := requireClaims(ctx)
	if err != nil {
		return err
	}
	if reviewer.Zero() {
		reviewer = claims.PrincipalID
	}
	if reviewer != claims.PrincipalID {
		return fmt.Errorf("force validate artifact: reviewer does not match authenticated principal")
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().Model((*Artifact)(nil)).
			Set("status = ?", ArtifactValid).
			Set("force_valid = ?", true).
			Set("updated_at = ?", time.Now().UTC().UnixMilli()).
			Set("updated_by = ?", claims.PrincipalID).
			Set("version = version + 1").
			Where("id = ? AND tenant_id = ? AND entity_id = ? AND deleted_at = 0", id, claims.TenantID, claims.EntityID).
			Where("status != ?", ArtifactOrphaned).
			Exec(ctx)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrArtifactNotFound
		}
		validationID, err := s.nextID()
		if err != nil {
			return fmt.Errorf("generate validation id: %w", err)
		}
		validation := ValidationResult{
			ID: validationID, TenantID: claims.TenantID, EntityID: claims.EntityID, ArtifactID: id,
			ValidatorKey: "force_valid", ValidatorType: "human", Passed: true,
			EvidenceJSON: `{"forceValid":true}`, ReviewedBy: reviewer, TS: time.Now().UnixMilli(),
		}
		_, err = tx.NewInsert().Model(&validation).Exec(ctx)
		return err
	})
}

func markArtifactDependentsStale(ctx context.Context, tx bun.IDB, id guid.ID) error {
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

func requireClaims(ctx context.Context) (*authn.Claims, error) {
	claims, ok := authn.FromContext(ctx)
	if !ok || claims.TenantID.Zero() || claims.EntityID.Zero() || claims.PrincipalID.Zero() {
		return nil, fmt.Errorf("artifact repository requires authenticated tenant, entity, and principal claims")
	}
	return claims, nil
}
