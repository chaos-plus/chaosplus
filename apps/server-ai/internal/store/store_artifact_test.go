package store

import (
	"context"
	"testing"
)

func TestArtifactChecksumChangePropagatesStaleTransitively(t *testing.T) {
	st, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	a := Artifact{ID: "a", InstanceID: "i", ProjectID: "p", LogicalID: "source", LogicalPath: "source.json", Checksum: "sha-a1"}
	b := Artifact{ID: "b", InstanceID: "i", ProjectID: "p", LogicalID: "build", LogicalPath: "build.json", Checksum: "sha-b1"}
	c := Artifact{ID: "c", InstanceID: "i", ProjectID: "p", LogicalID: "release", LogicalPath: "release.json", Checksum: "sha-c1"}
	if err := st.UpsertArtifact(ctx, a, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertArtifact(ctx, b, []Artifact{a}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertArtifact(ctx, c, []Artifact{b}); err != nil {
		t.Fatal(err)
	}
	a.Checksum = "sha-a2"
	if err := st.UpsertArtifact(ctx, a, nil); err != nil {
		t.Fatal(err)
	}

	got, err := st.ListArtifacts(ctx, "i", "p", "")
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]Artifact, len(got))
	for _, artifact := range got {
		byID[artifact.ID] = artifact
	}
	if byID["a"].Status != ArtifactValid || byID["b"].Status != ArtifactStale || byID["c"].Status != ArtifactStale {
		t.Fatalf("unexpected artifact states: %+v", byID)
	}
}

func TestArtifactReconciliationAndForceValid(t *testing.T) {
	st, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	a := Artifact{ID: "a", InstanceID: "i", ProjectID: "p", LogicalID: "source", LogicalPath: "source.json", Checksum: "sha-a1"}
	if err := st.UpsertArtifact(ctx, a, nil); err != nil {
		t.Fatal(err)
	}
	changed, err := st.ReconcileArtifact(ctx, "a", "sha-external", 42)
	if err != nil || !changed {
		t.Fatalf("ReconcileArtifact = (%v, %v), want changed", changed, err)
	}
	invalid, _ := st.ListArtifacts(ctx, "i", "p", ArtifactInvalid)
	if len(invalid) != 1 || invalid[0].SizeBytes != 42 {
		t.Fatalf("invalid artifacts = %+v", invalid)
	}
	if err := st.ForceValidateArtifact(ctx, "a", "reviewer-1"); err != nil {
		t.Fatal(err)
	}
	valid, _ := st.ListArtifacts(ctx, "i", "p", ArtifactValid)
	if len(valid) != 1 || !valid[0].ForceValid {
		t.Fatalf("force-valid artifact = %+v", valid)
	}
	results, err := st.ListValidationResults(ctx, "a", 10)
	if err != nil || len(results) != 1 || results[0].ReviewedBy != "reviewer-1" {
		t.Fatalf("force-valid validation result = (%+v, %v)", results, err)
	}
}
