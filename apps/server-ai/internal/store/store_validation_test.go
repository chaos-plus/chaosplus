package store

import (
	"context"
	"testing"
)

func TestValidationResultRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	v := ValidationResult{ID: "vr-1", ArtifactID: "run-1", ExecutionID: "ap",
		ValidatorID: "human", ValidatorType: "human", Passed: 1,
		EvidenceJSON: `{"reason":"ok"}`, ReviewedBy: "human"}
	if err := s.RecordValidationResult(ctx, v); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, err := s.ListValidationResults(ctx, "run-1", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Passed != 1 || got[0].ExecutionID != "ap" {
		t.Fatalf("validation result round-trip failed: %+v", got)
	}
}

// PRD §13: a rejection writes a feedback_log entry carrying the structured
// fields (category/location/expected/detail) for audit and next-attempt use.
func TestFeedbackLogRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	f := FeedbackLogEntry{ID: "fb-1", ArtifactID: "run-1", ExecutionID: "ap",
		Reviewer: "human", Category: "功能缺陷", Location: "main.go:12",
		Expected: "处理空输入", Detail: "缺少空值校验"}
	if err := s.RecordFeedbackLog(ctx, f); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, err := s.ListFeedbackLog(ctx, "run-1", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d feedback entries, want 1", len(got))
	}
	g := got[0]
	if g.Category != "功能缺陷" || g.Location != "main.go:12" || g.Expected != "处理空输入" || g.Detail != "缺少空值校验" {
		t.Fatalf("feedback fields lost: %+v", g)
	}
}
