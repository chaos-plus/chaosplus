package artifact

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
)

type ArtifactReader interface {
	ReadArtifact(context.Context, string, string, string) ([]byte, error)
	RegisteredRunners() []string
}

type ReconcileReport struct {
	Checked  int `json:"checked"`
	Changed  int `json:"changed"`
	Orphaned int `json:"orphaned"`
}

// ScopeDirectory is implemented at composition by the IAM owner. It returns
// only active tenant/entity/service-principal scopes authorized for lifecycle
// reconciliation; artifact never discovers or fabricates identity itself.
type ScopeDirectory interface {
	ActiveArtifactScopes(context.Context) ([]authn.Claims, error)
}

type Service struct {
	repository *BunRepository
	reader     ArtifactReader
	scopes     ScopeDirectory
	interval   time.Duration
}

func NewService(repository *BunRepository, reader ArtifactReader, scopes ScopeDirectory, interval time.Duration) *Service {
	if repository == nil {
		panic("artifact service requires repository")
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Service{repository: repository, reader: reader, scopes: scopes, interval: interval}
}

func (s *Service) Run(ctx context.Context) {
	if s.reader == nil || s.scopes == nil {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scopes, err := s.scopes.ActiveArtifactScopes(ctx)
			if err != nil {
				slog.Warn("artifact reconciliation scopes", "error", err)
				continue
			}
			for i := range scopes {
				claims := scopes[i]
				if claims.TenantID.Zero() || claims.EntityID.Zero() || claims.PrincipalID.Zero() {
					slog.Warn("artifact reconciliation rejected incomplete IAM scope")
					continue
				}
				report, err := s.Reconcile(authn.WithClaims(ctx, &claims))
				if err != nil {
					slog.Warn("artifact reconciliation", "tenant_id", claims.TenantID, "entity_id", claims.EntityID, "error", err)
				} else if report.Changed > 0 || report.Orphaned > 0 {
					slog.Info("artifact reconciliation completed", "tenant_id", claims.TenantID, "entity_id", claims.EntityID, "checked", report.Checked, "changed", report.Changed, "orphaned", report.Orphaned)
				}
			}
		}
	}
}

func (s *Service) Reconcile(ctx context.Context) (ReconcileReport, error) {
	var report ReconcileReport
	if s.reader == nil {
		return report, nil
	}
	artifacts, err := s.repository.ListArtifacts(ctx, authn.EntityIDFromContext(ctx), 0, "")
	if err != nil {
		return report, err
	}
	online := make(map[string]bool)
	for _, id := range s.reader.RegisteredRunners() {
		online[id] = true
	}
	for _, artifact := range artifacts {
		if artifact.RunnerHandle == "" || artifact.SpawnHandle == "" {
			continue
		}
		body, readErr := s.reader.ReadArtifact(ctx, artifact.RunnerHandle, artifact.SpawnHandle, artifact.LogicalPath)
		if readErr != nil {
			if online[artifact.RunnerHandle] && artifact.Status != ArtifactOrphaned {
				if err := s.repository.MarkArtifactOrphanedIfChecksum(ctx, artifact.ID, artifact.Checksum); err != nil {
					return report, err
				}
				report.Orphaned++
			}
			continue
		}
		report.Checked++
		digest := sha256.Sum256(body)
		checksum := fmt.Sprintf("sha256:%x", digest[:])
		if artifact.Checksum == checksum {
			continue
		}
		changed, err := s.repository.ReconcileArtifactIfChecksum(ctx, artifact.ID, artifact.Checksum, checksum, int64(len(body)))
		if err != nil {
			return report, err
		}
		if changed {
			report.Changed++
		}
	}
	return report, nil
}
