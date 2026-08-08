package store

import (
	"context"
	"testing"
)

func TestAppendAndListEvents(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Append(ctx, Event{ID: "e1", RunID: "r1", Type: "RUN_STARTED", IdempotencyKey: "r1:1", PayloadJSON: `{"runId":"r1"}`}); err != nil {
		t.Fatalf("append e1: %v", err)
	}
	if err := s.Append(ctx, Event{ID: "e2", RunID: "r1", Type: "NODE_STARTED", IdempotencyKey: "r1:2", PayloadJSON: `{"nodeId":"n1"}`}); err != nil {
		t.Fatalf("append e2: %v", err)
	}

	evs, err := s.ListEvents(ctx, "r1", 0, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2", len(evs))
	}
	if evs[0].Type != "RUN_STARTED" || evs[1].Type != "NODE_STARTED" {
		t.Fatalf("order wrong: %+v", evs)
	}
	if evs[0].Seq >= evs[1].Seq {
		t.Fatalf("seq not increasing: %d >= %d", evs[0].Seq, evs[1].Seq)
	}
	if evs[0].TS == 0 {
		t.Fatal("ts not set")
	}
}

func TestAppendIdempotent(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	e := Event{ID: "e1", RunID: "r1", Type: "RUN_STARTED", IdempotencyKey: "same-key"}
	if err := s.Append(ctx, e); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := s.Append(ctx, e); err != nil { // duplicate key → no-op, no error
		t.Fatalf("duplicate append: %v", err)
	}
	evs, err := s.ListEvents(ctx, "r1", 0, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("duplicate inserted: got %d events, want 1", len(evs))
	}
}
