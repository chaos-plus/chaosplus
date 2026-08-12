package artifact

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workflow"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

type WorkflowProjector struct {
	nextID func() (guid.ID, error)
}

func NewWorkflowProjector(nextID func() (guid.ID, error)) *WorkflowProjector {
	if nextID == nil {
		panic("artifact workflow projector requires id generator")
	}
	return &WorkflowProjector{nextID: nextID}
}

func (p *WorkflowProjector) Project(ctx context.Context, db bun.IDB, record workflow.EventRecord) error {
	var event workflow.RunEvent
	if err := json.Unmarshal([]byte(record.PayloadJSON), &event); err != nil {
		return fmt.Errorf("decode workflow event: %w", err)
	}
	if len(event.Artifacts) == 0 {
		return nil
	}
	dependencies, err := loadDependencies(ctx, db, record, event.Consumes)
	if err != nil {
		return err
	}
	for _, produced := range event.Artifacts {
		if produced.Path == "" || produced.Checksum == "" {
			return fmt.Errorf("project artifact: path and checksum are required")
		}
		artifactType := produced.Type
		if artifactType == "" {
			artifactType = "file"
		}
		artifact, err := loadArtifactByPath(ctx, db, record, produced.Path)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			artifact.ID, err = p.nextID()
			if err != nil {
				return fmt.Errorf("generate artifact id: %w", err)
			}
			artifact.CreatedAt, artifact.CreatedBy, artifact.Version = record.TS, record.ActorID, 1
		}
		if artifact.CreatedAt == 0 {
			artifact.CreatedAt = record.TS
		}
		artifact.TenantID, artifact.EntityID, artifact.ProjectID, artifact.OwnerID = record.TenantID, record.EntityID, record.ProjectID, record.ActorID
		artifact.LogicalKey, artifact.LogicalPath, artifact.Type = produced.ID, produced.Path, artifactType
		artifact.Checksum, artifact.SizeBytes, artifact.Status, artifact.ForceValid = produced.Checksum, produced.SizeBytes, ArtifactValid, false
		artifact.ProducerRunID, artifact.ProducerNodeKey, artifact.Attempt = record.RunID, event.NodeID, event.Attempt+1
		artifact.RunnerHandle, artifact.SpawnHandle = produced.RunnerID, produced.SpawnID
		artifact.UpdatedAt, artifact.UpdatedBy = record.TS, record.ActorID
		if err := projectArtifact(ctx, db, artifact, dependencies, record.TS); err != nil {
			return err
		}
	}
	return nil
}

func loadArtifactByPath(ctx context.Context, db bun.IDB, record workflow.EventRecord, path string) (Artifact, error) {
	var artifact Artifact
	err := db.NewSelect().Model(&artifact).
		Where("tenant_id = ? AND entity_id = ? AND project_id = ? AND logical_path = ? AND deleted_at = 0", record.TenantID, record.EntityID, record.ProjectID, path).
		Scan(ctx)
	return artifact, err
}

func loadDependencies(ctx context.Context, db bun.IDB, record workflow.EventRecord, keys []string) ([]Artifact, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	items := make([]Artifact, 0)
	err := db.NewSelect().Model(&items).
		Where("tenant_id = ? AND entity_id = ? AND project_id = ? AND logical_key IN (?) AND status = ? AND deleted_at = 0", record.TenantID, record.EntityID, record.ProjectID, bun.List(keys), ArtifactValid).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("load artifact dependencies: %w", err)
	}
	return items, nil
}

func projectArtifact(ctx context.Context, db bun.IDB, artifact Artifact, dependencies []Artifact, eventTime int64) error {
	var previous Artifact
	err := db.NewSelect().Model(&previous).Where("id = ? AND tenant_id = ? AND entity_id = ?", artifact.ID, artifact.TenantID, artifact.EntityID).Scan(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && previous.Checksum != artifact.Checksum {
		if err := markArtifactDependentsStale(ctx, db, artifact.ID); err != nil {
			return err
		}
		artifact.CreatedAt, artifact.CreatedBy, artifact.Version = previous.CreatedAt, previous.CreatedBy, previous.Version+1
	}
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := db.NewInsert().Model(&artifact).Exec(ctx); err != nil {
			return fmt.Errorf("insert artifact projection: %w", err)
		}
	} else {
		if _, err := db.NewUpdate().Model(&artifact).Column("logical_key", "logical_path", "type", "checksum", "size_bytes", "status", "force_valid", "producer_run_id", "producer_node_key", "attempt", "runner_handle", "spawn_handle", "updated_at", "updated_by", "version").WherePK().Exec(ctx); err != nil {
			return fmt.Errorf("update artifact projection: %w", err)
		}
	}
	if _, err := db.NewDelete().Model((*ArtifactDep)(nil)).Where("tenant_id = ? AND entity_id = ? AND artifact_id = ?", artifact.TenantID, artifact.EntityID, artifact.ID).Exec(ctx); err != nil {
		return err
	}
	for _, dependency := range dependencies {
		edge := ArtifactDep{TenantID: artifact.TenantID, EntityID: artifact.EntityID, ArtifactID: artifact.ID, DependsOnID: dependency.ID, InputChecksum: dependency.Checksum, CreatedAt: eventTime, CreatedBy: artifact.UpdatedBy}
		if _, err := db.NewInsert().Model(&edge).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

var _ workflow.EventProjector = (*WorkflowProjector)(nil)
