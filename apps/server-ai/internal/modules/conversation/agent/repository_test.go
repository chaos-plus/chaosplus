package agent

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/machine"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func TestSQLiteAgentRepositoryLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
	if err := machine.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := machine.NewRepository(db).Upsert(ctx, machine.Machine{ID: 41, TenantID: 11, EntityID: 21, OwnerID: 31, Status: machine.StatusConfirmed}); err != nil {
		t.Fatal(err)
	}
	next := guid.ID(100)
	repository := NewRepository(db, func() (guid.ID, error) { next++; return next, nil })
	value := &Agent{MachineID: 41, Name: "Builder", Kind: KindExecutor, Runtime: "codex", Status: StatusStopped, SpecJSON: "{}"}
	if err := repository.Create(ctx, value); err != nil {
		t.Fatal(err)
	}
	items, err := repository.List(ctx)
	if err != nil || len(items) != 1 || items[0].ID != value.ID {
		t.Fatalf("list = (%+v, %v)", items, err)
	}
	value.Name = "Builder 2"
	if err := repository.Update(ctx, value, value.Version); err != nil {
		t.Fatal(err)
	}
	updated, err := repository.SetLifecycle(ctx, value.ID, StatusRetired, "handover", value.Version)
	if err != nil || updated.Status != StatusRetired {
		t.Fatalf("retire = (%+v, %v)", updated, err)
	}
	counts, err := repository.CountByMachine(ctx)
	if err != nil || counts[41] != 0 {
		t.Fatalf("counts = (%v, %v)", counts, err)
	}
	if err := repository.Delete(ctx, value.ID, updated.Version); err != nil {
		t.Fatal(err)
	}
}
