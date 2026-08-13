package requirement

import (
	"context"
	"reflect"
	"testing"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/objective"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func TestRepositoryMaintainsKeyResultLinksWithOptimisticVersion(t *testing.T) {
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := objective.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	next := guid.ID(100)
	nextID := func() (guid.ID, error) { next++; return next, nil }
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	objectives := objective.NewRepository(db, nextID)
	goal := &objective.Objective{Title: "Delivery", PeriodStart: 1, PeriodEnd: 2, Status: objective.StatusDraft}
	if err := objectives.Create(ctx, goal, []objective.KeyResultInput{{Title: "Ship", TargetValue: "10", CurrentValue: "1", Unit: "items"}, {Title: "Quality", TargetValue: "99", CurrentValue: "90", Unit: "%"}}); err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(db, nextID)
	value := &Requirement{Title: "Release", Status: StatusDraft, KeyResultIDs: []guid.ID{goal.KeyResults[0].ID}}
	if err := repository.Create(ctx, value); err != nil {
		t.Fatal(err)
	}
	got, err := repository.Get(ctx, value.ID)
	if err != nil || !reflect.DeepEqual(got.KeyResultIDs, value.KeyResultIDs) {
		t.Fatalf("created requirement = (%+v, %v)", got, err)
	}
	listed, err := repository.List(ctx, "", nil)
	if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0].KeyResultIDs, value.KeyResultIDs) {
		t.Fatalf("listed requirements = (%+v, %v)", listed, err)
	}
	got.KeyResultIDs = []guid.ID{goal.KeyResults[1].ID}
	if err := repository.Update(ctx, got, 1); err != nil {
		t.Fatal(err)
	}
	updated, err := repository.Get(ctx, value.ID)
	if err != nil || updated.Version != 2 || !reflect.DeepEqual(updated.KeyResultIDs, got.KeyResultIDs) {
		t.Fatalf("updated requirement = (%+v, %v)", updated, err)
	}
	inUse, err := repository.KeyResultsInUse(ctx, updated.KeyResultIDs)
	if err != nil || !inUse {
		t.Fatalf("key result usage before delete = (%v, %v)", inUse, err)
	}
	if err := repository.Delete(ctx, updated.ID, updated.Version); err != nil {
		t.Fatal(err)
	}
	inUse, err = repository.KeyResultsInUse(ctx, updated.KeyResultIDs)
	if err != nil || inUse {
		t.Fatalf("key result usage after delete = (%v, %v)", inUse, err)
	}
	if _, err := db.NewInsert().Model(&keyResultLink{TenantID: value.TenantID, EntityID: value.EntityID, RequirementID: value.ID, KeyResultID: updated.KeyResultIDs[0], CreatedAt: value.CreatedAt, CreatedBy: value.CreatedBy}).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	inUse, err = repository.KeyResultsInUse(ctx, updated.KeyResultIDs)
	if err != nil || inUse {
		t.Fatalf("legacy deleted requirement usage = (%v, %v)", inUse, err)
	}
}
