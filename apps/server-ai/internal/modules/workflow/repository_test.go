package workflow

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
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
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	return NewBunRepository(db), ctx
}

func TestAppendAndListEvents(t *testing.T) {
	repository, ctx := newTestRepository(t)
	run := RunDef{ID: 41, ProjectID: 51, DefJSON: `{"id":"wf","version":"1","nodes":[],"edges":[]}`, ContextJSON: `{}`, Workspace: "/tmp/work"}
	if err := repository.SaveRunDefinition(ctx, run); err != nil {
		t.Fatal(err)
	}
	for _, event := range []EventRecord{
		{ID: 61, ProjectID: 51, RunID: 41, Type: "RUN_STARTED", IdempotencyKey: "41:1", PayloadJSON: `{"runId":"41"}`},
		{ID: 62, ProjectID: 51, RunID: 41, Type: "NODE_STARTED", IdempotencyKey: "41:2", PayloadJSON: `{"nodeId":"n1"}`},
	} {
		if err := repository.Append(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	items, err := repository.ListEvents(ctx, 41, 0, 10)
	if err != nil || len(items) != 2 || items[0].Seq >= items[1].Seq {
		t.Fatalf("events = (%+v, %v)", items, err)
	}
}

func TestRepositoryRejectsMissingClaims(t *testing.T) {
	repository, _ := newTestRepository(t)
	if _, err := repository.ListEvents(context.Background(), 41, 0, 10); err == nil {
		t.Fatal("missing claims must fail")
	}
}
