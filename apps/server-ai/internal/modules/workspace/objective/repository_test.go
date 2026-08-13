package objective

import (
	"context"
	"errors"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func TestRepositoryPersistsScopedObjectiveAndKeyResults(t *testing.T) {
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	next := guid.ID(100)
	repository := NewRepository(db, func() (guid.ID, error) { next++; return next, nil })
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	value := &Objective{Title: "Delivery", PeriodStart: 1, PeriodEnd: 2, Status: StatusDraft}
	if err := repository.Create(ctx, value, []KeyResultInput{{Title: "Ship", TargetValue: "10", CurrentValue: "1", Unit: "items"}}); err != nil {
		t.Fatal(err)
	}
	if value.ID.Zero() || len(value.KeyResults) != 1 || value.OwnerID != 31 || value.Version != 1 {
		t.Fatalf("created objective = %+v", value)
	}
	if err := repository.KeyResultsExist(ctx, []guid.ID{value.KeyResults[0].ID}); err != nil {
		t.Fatalf("same-scope key result: %v", err)
	}
	other := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 12, EntityID: 22, PrincipalID: 32})
	if err := repository.KeyResultsExist(other, []guid.ID{value.KeyResults[0].ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-scope key result = %v, want ErrNotFound", err)
	}
	value.Description = "updated"
	if err := repository.Update(ctx, value, []KeyResultInput{{Title: "Verify", TargetValue: "1", CurrentValue: "0", Unit: "flow"}}, 1); err != nil {
		t.Fatalf("update objective: %v", err)
	}
	if value.Version != 2 || value.Description != "updated" || len(value.KeyResults) != 1 || value.KeyResults[0].Title != "Verify" {
		t.Fatalf("updated objective = %+v", value)
	}
	if err := repository.Delete(ctx, value.ID, value.Version); err != nil {
		t.Fatalf("delete objective: %v", err)
	}
	if _, err := repository.Get(ctx, value.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted objective = %v, want ErrNotFound", err)
	}
}
