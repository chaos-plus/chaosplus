package testcase

import (
	"context"
	"errors"
	"testing"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/objective"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/requirement"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func TestRepositoryPersistsOrderedStepsAndScope(t *testing.T) {
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := objective.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := requirement.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	next := guid.ID(100)
	nextID := func() (guid.ID, error) { next++; return next, nil }
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	requirements := requirement.NewRepository(db, nextID)
	req := &requirement.Requirement{Title: "Release", Status: requirement.StatusDraft}
	if err := requirements.Create(ctx, req); err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(db, nextID)
	value := &TestCase{RequirementID: &req.ID, Title: "Login", Priority: PriorityHigh, Status: StatusDraft}
	steps := []StepInput{{Action: "Open login", ExpectedResult: "Form is visible"}, {Action: "Submit credentials", ExpectedResult: "Dashboard opens"}}
	if err := repository.Create(ctx, value, steps); err != nil {
		t.Fatal(err)
	}
	got, err := repository.Get(ctx, value.ID)
	if err != nil || len(got.Steps) != 2 || got.Steps[0].Position != 1 || got.Steps[1].Position != 2 || got.OwnerID != 31 {
		t.Fatalf("test case = (%+v, %v)", got, err)
	}
	other := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 12, EntityID: 22, PrincipalID: 32})
	if _, err := repository.Get(other, value.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-scope get = %v", err)
	}
}

func TestServiceEnforcesLifecycleAndReferenceScope(t *testing.T) {
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := objective.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := requirement.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	next := guid.ID(200)
	nextID := func() (guid.ID, error) { next++; return next, nil }
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	requirements := requirement.NewRepository(db, nextID)
	req := &requirement.Requirement{Title: "Release", Status: requirement.StatusDraft}
	if err := requirements.Create(ctx, req); err != nil {
		t.Fatal(err)
	}
	service := NewService(NewRepository(db, nextID), requirements)
	value, err := service.Create(ctx, CreateInput{RequirementID: &req.ID, Title: "Login", Steps: []StepInput{{Action: "Open", ExpectedResult: "Visible"}}})
	if err != nil {
		t.Fatal(err)
	}
	retired := StatusRetired
	value, err = service.Update(ctx, value.ID, UpdateInput{Status: &retired, Version: value.Version})
	if err != nil || value.Status != StatusRetired {
		t.Fatalf("retire = (%+v, %v)", value, err)
	}
	active := StatusActive
	if _, err := service.Update(ctx, value.ID, UpdateInput{Status: &active, Version: value.Version}); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("reactivate = %v", err)
	}
}
