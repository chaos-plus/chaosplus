package testrun

import (
	"context"
	"errors"
	"testing"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workflow"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/objective"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/requirement"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/testcase"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

func newTestService(t *testing.T) (*Service, context.Context, guid.ID) {
	t.Helper()
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, migrate := range []func(context.Context, *bun.DB) error{workflow.Migrate, objective.Migrate, requirement.Migrate, testcase.Migrate, Migrate} {
		if err := migrate(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	next := guid.ID(100)
	nextID := func() (guid.ID, error) { next++; return next, nil }
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	cases := testcase.NewRepository(db, nextID)
	value := &testcase.TestCase{Title: "Login", Priority: testcase.PriorityHigh, Status: testcase.StatusDraft}
	if err := cases.Create(ctx, value, []testcase.StepInput{{Action: "Open", ExpectedResult: "Visible"}}); err != nil {
		t.Fatal(err)
	}
	return NewService(NewRepository(db, nextID), cases), ctx, value.ID
}

func TestServicePreservesTerminalTestRun(t *testing.T) {
	service, ctx, testCaseID := newTestService(t)
	value, err := service.Create(ctx, CreateInput{TestCaseID: testCaseID, Environment: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	value, err = service.Update(ctx, value.ID, UpdateInput{Status: StatusRunning, Version: value.Version})
	if err != nil || value.StartedAt == 0 {
		t.Fatalf("start = (%+v, %v)", value, err)
	}
	observed := "Dashboard opened"
	value, err = service.Update(ctx, value.ID, UpdateInput{Status: StatusPassed, ObservedResult: &observed, Version: value.Version})
	if err != nil || value.CompletedAt < value.StartedAt {
		t.Fatalf("pass = (%+v, %v)", value, err)
	}
	if _, err := service.Update(ctx, value.ID, UpdateInput{Status: StatusFailed, Version: value.Version}); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("rewrite terminal = %v", err)
	}
}

func TestServiceRequiresFailureSummary(t *testing.T) {
	service, ctx, testCaseID := newTestService(t)
	value, err := service.Create(ctx, CreateInput{TestCaseID: testCaseID, Environment: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	value, err = service.Update(ctx, value.ID, UpdateInput{Status: StatusRunning, Version: value.Version})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(ctx, value.ID, UpdateInput{Status: StatusFailed, Version: value.Version}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("failure without summary = %v", err)
	}
}
