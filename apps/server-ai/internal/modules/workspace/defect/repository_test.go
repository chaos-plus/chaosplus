package defect

import (
	"context"
	"errors"
	"testing"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workflow"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/objective"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/requirement"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/task"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/testcase"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/testrun"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
)

func TestServiceEnforcesDefectResolutionLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, migrate := range []func(context.Context, *bun.DB) error{workflow.Migrate, objective.Migrate, requirement.Migrate, task.Migrate, testcase.Migrate, testrun.Migrate, Migrate} {
		if err := migrate(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	next := guid.ID(100)
	nextID := func() (guid.ID, error) { next++; return next, nil }
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	tasks := task.NewRepository(db, nextID)
	work := &task.Task{Title: "Fix login", Status: task.StatusOpen}
	if err := tasks.Create(ctx, work); err != nil {
		t.Fatal(err)
	}
	service := NewService(NewRepository(db, nextID), nil, tasks, nil, nil)
	value, err := service.Create(ctx, CreateInput{TaskID: &work.ID, Title: "Login fails", ReproductionSteps: "Submit valid credentials", ExpectedResult: "Dashboard opens", ActualResult: "Error appears", Severity: SeverityCritical, Priority: PriorityHigh})
	if err != nil {
		t.Fatal(err)
	}
	inProgress := StatusInProgress
	value, err = service.Update(ctx, value.ID, UpdateInput{Status: &inProgress, Version: value.Version})
	if err != nil {
		t.Fatal(err)
	}
	resolved := StatusResolved
	if _, err := service.Update(ctx, value.ID, UpdateInput{Status: &resolved, Version: value.Version}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("resolve without details = %v", err)
	}
	resolution := ResolutionFixed
	note := "Corrected session validation"
	value, err = service.Update(ctx, value.ID, UpdateInput{Status: &resolved, Resolution: &resolution, ResolutionNote: &note, Version: value.Version})
	if err != nil {
		t.Fatal(err)
	}
	verified := StatusVerified
	value, err = service.Update(ctx, value.ID, UpdateInput{Status: &verified, Version: value.Version})
	if err != nil {
		t.Fatal(err)
	}
	reopened := StatusReopened
	value, err = service.Update(ctx, value.ID, UpdateInput{Status: &reopened, Version: value.Version})
	if err != nil || value.Resolution != "" {
		t.Fatalf("reopen = (%+v,%v)", value, err)
	}
	value, err = service.Update(ctx, value.ID, UpdateInput{Status: &resolved, Resolution: &resolution, ResolutionNote: &note, Version: value.Version})
	if err != nil {
		t.Fatal(err)
	}
	value, err = service.Update(ctx, value.ID, UpdateInput{Status: &verified, Version: value.Version})
	if err != nil {
		t.Fatal(err)
	}
	closed := StatusClosed
	value, err = service.Update(ctx, value.ID, UpdateInput{Status: &closed, Version: value.Version})
	if err != nil || value.Status != StatusClosed {
		t.Fatalf("close = (%+v,%v)", value, err)
	}
	if _, err := service.Update(ctx, value.ID, UpdateInput{Status: &reopened, Version: value.Version}); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("rewrite closed = %v", err)
	}
}

func TestServiceRejectsMissingOrCrossScopeSource(t *testing.T) {
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, migrate := range []func(context.Context, *bun.DB) error{workflow.Migrate, objective.Migrate, requirement.Migrate, task.Migrate, testcase.Migrate, testrun.Migrate, Migrate} {
		if err := migrate(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	next := guid.ID(300)
	nextID := func() (guid.ID, error) { next++; return next, nil }
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	tasks := task.NewRepository(db, nextID)
	service := NewService(NewRepository(db, nextID), nil, tasks, nil, nil)
	input := CreateInput{Title: "No source", ReproductionSteps: "r", ExpectedResult: "e", ActualResult: "a"}
	if _, err := service.Create(ctx, input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing source = %v", err)
	}
	unknown := guid.ID(999)
	input.TaskID = &unknown
	if _, err := service.Create(ctx, input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown source = %v", err)
	}
}
