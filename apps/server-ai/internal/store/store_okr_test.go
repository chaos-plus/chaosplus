package store

import (
	"context"
	"testing"
)

func TestOkrCRUD(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)

	o := &Okr{ID: "okr-1", Title: "Q3", Objective: "增长", Period: "2026-Q3", KeyResults: `[{"title":"MAU","target":100,"progress":40,"unit":"万"}]`}
	if err := s.CreateOkr(ctx, o); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetOkr(ctx, "okr-1")
	if err != nil || got.Objective != "增长" || got.Period != "2026-Q3" {
		t.Fatalf("get wrong: %+v %v", got, err)
	}
	list, err := s.ListOkrs(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list wrong: %+v %v", list, err)
	}

	o.Objective = "营收"
	if err := s.UpdateOkr(ctx, o); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.GetOkr(ctx, "okr-1")
	if got.Objective != "营收" {
		t.Fatalf("update not applied: %+v", got)
	}

	if err := s.DeleteOkr(ctx, "okr-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = s.ListOkrs(ctx)
	if len(list) != 0 {
		t.Fatalf("delete failed: %+v", list)
	}
}
