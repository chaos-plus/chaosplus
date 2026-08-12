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
			report, err := m.ReconcileArtifacts(ctx)
			if err != nil {
				slog.Warn("artifact reconciliation", "err", err)
			} else if report.Changed > 0 || report.Orphaned > 0 {
				slog.Info("artifact reconciliation completed", "checked", report.Checked, "changed", report.Changed, "orphaned", report.Orphaned)
			}
		}
	}
}

func (m *RunManager) ReconcileArtifacts(ctx context.Context) (ReconcileReport, error) {
	var report ReconcileReport
	if m.st == nil || m.link == nil {
		return report, nil
	}
	artifacts, err := m.st.ListArtifacts(ctx, "", "", "")
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
				if markErr := m.st.MarkArtifactOrphaned(ctx, artifact.ID); markErr != nil {
					return report, markErr
				}
				artifact.Status = store.ArtifactOrphaned
				payload, _ := json.Marshal(artifact)
				if appendErr := m.st.Append(ctx, store.Event{
					ID: fmt.Sprintf("orphan-%s-%d", artifact.ID, time.Now().UnixNano()), InstanceID: artifact.InstanceID,
					RunID: artifact.ProducerRunID, Type: "ARTIFACT_ORPHANED",
					IdempotencyKey: fmt.Sprintf("orphan:%s:%s", artifact.ID, artifact.Checksum), PayloadJSON: string(payload),
				}); appendErr != nil {
					return report, appendErr
				}
				report.Orphaned++
			}
			continue
		}
		report.Checked++
		digest := sha256.Sum256(body)
		checksum := fmt.Sprintf("sha256:%x", digest[:])
		changed, err := m.st.ReconcileArtifact(ctx, artifact.ID, checksum, int64(len(body)))
		if err != nil {
			return report, err
		}
		if !changed {
			continue
		}
		report.Changed++
		artifact.Checksum = checksum
		artifact.SizeBytes = int64(len(body))
		artifact.Status = store.ArtifactInvalid
		payload, _ := json.Marshal(artifact)
		if err := m.st.Append(ctx, store.Event{
			ID:         fmt.Sprintf("reconcile-%s-%d", artifact.ID, time.Now().UnixNano()),
			InstanceID: artifact.InstanceID, RunID: artifact.ProducerRunID,
			Type:           "ARTIFACT_INVALIDATED",
			IdempotencyKey: fmt.Sprintf("reconcile:%s:%s", artifact.ID, checksum),
			PayloadJSON:    string(payload),
		}); err != nil {
			return report, err
		}
	}
	return report, nil
}
