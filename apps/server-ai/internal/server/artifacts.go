package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
)

type ReconcileReport struct {
	Checked  int `json:"checked"`
	Changed  int `json:"changed"`
	Orphaned int `json:"orphaned"`
}

func (m *RunManager) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report, err := m.ReconcileArtifacts(ctx, "")
			if err != nil {
				slog.Warn("artifact reconciliation", "err", err)
			} else if report.Changed > 0 || report.Orphaned > 0 {
				slog.Info("artifact reconciliation completed", "checked", report.Checked, "changed", report.Changed, "orphaned", report.Orphaned)
			}
		}
	}
}

func (m *RunManager) ReconcileArtifacts(ctx context.Context, instanceID string) (ReconcileReport, error) {
	var report ReconcileReport
	if m.st == nil || m.link == nil {
		return report, nil
	}
	artifacts, err := m.st.ListArtifacts(ctx, instanceID, "", "")
	if err != nil {
		return report, err
	}
	online := make(map[string]bool)
	for _, id := range m.link.RegisteredRunners() {
		online[id] = true
	}
	for _, artifact := range artifacts {
		if artifact.RunnerID == "" || artifact.SpawnID == "" {
			continue
		}
		body, err := m.link.ReadArtifact(ctx, artifact.RunnerID, artifact.SpawnID, artifact.LogicalPath)
		if err != nil {
			if online[artifact.RunnerID] && artifact.Status != store.ArtifactOrphaned {
				artifact.Status = store.ArtifactOrphaned
				payload, marshalErr := json.Marshal(artifact)
				if marshalErr != nil {
					return report, marshalErr
				}
				committed, appendErr := m.st.CommitArtifactEventIfChecksum(ctx, store.Event{
					ID: fmt.Sprintf("orphan-%s-%d", artifact.ID, time.Now().UnixNano()), InstanceID: artifact.InstanceID,
					RunID: artifact.ProducerRunID, Type: "ARTIFACT_ORPHANED",
					IdempotencyKey: fmt.Sprintf("orphan:%s:%s", artifact.ID, artifact.Checksum), PayloadJSON: string(payload),
				}, artifact.Checksum)
				if appendErr != nil {
					return report, appendErr
				}
				if committed {
					report.Orphaned++
				}
			}
			continue
		}
		report.Checked++
		digest := sha256.Sum256(body)
		checksum := fmt.Sprintf("sha256:%x", digest[:])
		if artifact.Checksum == checksum {
			continue
		}
		expectedChecksum := artifact.Checksum
		artifact.Checksum = checksum
		artifact.SizeBytes = int64(len(body))
		artifact.Status = store.ArtifactInvalid
		payload, marshalErr := json.Marshal(artifact)
		if marshalErr != nil {
			return report, marshalErr
		}
		committed, err := m.st.CommitArtifactEventIfChecksum(ctx, store.Event{
			ID:         fmt.Sprintf("reconcile-%s-%d", artifact.ID, time.Now().UnixNano()),
			InstanceID: artifact.InstanceID, RunID: artifact.ProducerRunID,
			Type:           "ARTIFACT_INVALIDATED",
			IdempotencyKey: fmt.Sprintf("reconcile:%s:%s", artifact.ID, checksum),
			PayloadJSON:    string(payload),
		}, expectedChecksum)
		if err != nil {
			return report, err
		}
		if committed {
			report.Changed++
		}
	}
	return report, nil
}
