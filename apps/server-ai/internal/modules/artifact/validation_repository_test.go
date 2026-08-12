package artifact

import "testing"

func TestValidationResultRoundTrip(t *testing.T) {
	repository, ctx := newTestRepository(t)
	artifact := Artifact{ProjectID: 41, LogicalKey: "source", LogicalPath: "source.json", Type: "json", Checksum: "sha256:source", ProducerRunID: 51, ProducerNodeKey: "source"}
	if err := repository.UpsertArtifact(ctx, artifact, nil); err != nil {
		t.Fatal(err)
	}
	items, err := repository.ListArtifacts(ctx, 21, 41, "")
	if err != nil || len(items) != 1 {
		t.Fatalf("artifacts = (%+v, %v)", items, err)
	}
	result := ValidationResult{ArtifactID: items[0].ID, ExecutionKey: "review", ValidatorKey: "human", ValidatorType: "human", Passed: true, EvidenceJSON: `{"reason":"ok"}`}
	if err := repository.RecordValidationResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	got, err := repository.ListValidationResults(ctx, items[0].ID, 10)
	if err != nil || len(got) != 1 || !got[0].Passed || got[0].ReviewedBy != 31 {
		t.Fatalf("validation results = (%+v, %v)", got, err)
	}
}
