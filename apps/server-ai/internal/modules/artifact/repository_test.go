package artifact

import (
	"context"
	"errors"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func newTestRepository(t *testing.T) (*BunRepository, context.Context) {
	t.Helper()
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	next := guid.ID(1000)
	repository := NewBunRepository(db, func() (guid.ID, error) {
		next++
		return next, nil
	})
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	return repository, ctx
}

func TestRepositoryScopesArtifactsToVerifiedClaims(t *testing.T) {
	repository, ctx := newTestRepository(t)
	artifact := Artifact{ProjectID: 41, LogicalKey: "release", LogicalPath: "release.json", Type: "json", Checksum: "sha256:abc", ProducerRunID: 51, ProducerNodeKey: "build"}
	if err := repository.UpsertArtifact(ctx, artifact, nil); err != nil {
		t.Fatal(err)
	}
	items, err := repository.ListArtifacts(ctx, 21, 41, ArtifactValid)
	if err != nil || len(items) != 1 {
		t.Fatalf("list artifacts = (%+v, %v)", items, err)
	}
	if items[0].TenantID != 11 || items[0].EntityID != 21 || items[0].OwnerID != 31 || items[0].ID.Zero() {
		t.Fatalf("artifact scope/audit was not derived from claims: %+v", items[0])
	}
	other := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 12, EntityID: 22, PrincipalID: 32})
	items, err = repository.ListArtifacts(other, 22, 41, "")
	if err != nil || len(items) != 0 {
		t.Fatalf("cross-scope list = (%+v, %v), want empty", items, err)
	}
}

func TestRepositoryRejectsMissingClaims(t *testing.T) {
	repository, _ := newTestRepository(t)
	if _, err := repository.ListArtifacts(context.Background(), 0, 0, ""); err == nil {
		t.Fatal("list without verified claims must fail")
	}
}

func TestForceValidateRejectsOrphanedArtifact(t *testing.T) {
	repository, ctx := newTestRepository(t)
	value := Artifact{ProjectID: 41, LogicalKey: "missing", LogicalPath: "missing.json", Type: "json", Checksum: "sha256:missing", Status: ArtifactOrphaned, ProducerRunID: 51, ProducerNodeKey: "build"}
	if err := repository.UpsertArtifact(ctx, value, nil); err != nil {
		t.Fatal(err)
	}
	items, err := repository.ListArtifacts(ctx, 21, 41, ArtifactOrphaned)
	if err != nil || len(items) != 1 {
		t.Fatalf("orphaned artifacts = (%+v, %v)", items, err)
	}
	if err := repository.ForceValidateArtifact(ctx, items[0].ID, 31); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("force validate orphaned = %v, want ErrArtifactNotFound", err)
	}
	got, err := repository.GetArtifact(ctx, items[0].ID, 21)
	if err != nil || got.Status != ArtifactOrphaned || got.ForceValid {
		t.Fatalf("orphaned artifact changed = (%+v, %v)", got, err)
	}
	validations, err := repository.ListValidationResults(ctx, items[0].ID, 10)
	if err != nil || len(validations) != 0 {
		t.Fatalf("orphaned validation audit = (%+v, %v), want none", validations, err)
	}
}
