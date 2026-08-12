package machine

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	coreid "github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func TestMachineRepositoryCRUD(t *testing.T) {
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 201, EntityID: 301, PrincipalID: 401})
	datasource := bunx.Datasource{Type: "sqlite", Dsn: filepath.Join(t.TempDir(), "machine.db"), Writable: true}
	db, err := datasource.Open()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repository := NewRepository(db)

	machineID := coreid.ID(101)
	machine := Machine{ID: machineID, TenantID: 201, EntityID: 301, OwnerID: 401, Address: "127.0.0.1:8081", Status: StatusConfirmed, TokenHash: ""}
	if err := repository.Upsert(ctx, machine); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	items, err := repository.List(ctx)
	if err != nil || len(items) != 1 || items[0].ID != machineID {
		t.Fatalf("list = (%+v, %v), want %s", items, err, machineID)
	}
	if err := repository.TouchHeartbeat(ctx, machineID); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	newHash := hashToken("new-token")
	if err := repository.UpdateToken(ctx, machineID, newHash); err != nil {
		t.Fatalf("update token: %v", err)
	}
	items, _ = repository.List(ctx)
	if items[0].LastHeartbeatAt == 0 || items[0].TokenHash != newHash {
		t.Fatalf("updates not persisted: %+v", items[0])
	}
	if err := repository.Delete(ctx, machineID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	items, _ = repository.List(ctx)
	if len(items) != 0 {
		t.Fatalf("delete left %d rows", len(items))
	}
}

func TestMachineRepositoryRejectsMissingAndCrossScopeClaims(t *testing.T) {
	datasource := bunx.Datasource{Type: "sqlite", Dsn: filepath.Join(t.TempDir(), "machine-scope.db"), Writable: true}
	db, err := datasource.Open()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repository := NewRepository(db)
	machine := Machine{ID: 101, TenantID: 201, EntityID: 301, OwnerID: 401, Status: StatusConfirmed}
	if err := repository.Upsert(context.Background(), machine); err == nil {
		t.Fatal("upsert without authenticated claims must fail")
	}
	ownerCtx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 201, EntityID: 301, PrincipalID: 401})
	if err := repository.Upsert(ownerCtx, machine); err != nil {
		t.Fatalf("owner upsert: %v", err)
	}
	otherCtx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 202, EntityID: 302, PrincipalID: 402})
	items, err := repository.List(otherCtx)
	if err != nil || len(items) != 0 {
		t.Fatalf("cross-scope list = (%+v, %v), want empty", items, err)
	}
}
