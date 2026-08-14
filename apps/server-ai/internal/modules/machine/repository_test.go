package machine

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

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

func TestMachineRepositorySharesPendingTokensAndFencedRoutes(t *testing.T) {
	ctx := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 201, EntityID: 301, PrincipalID: 401})
	datasource := bunx.Datasource{Type: "sqlite", Dsn: filepath.Join(t.TempDir(), "machine-cluster.db"), Writable: true}
	db, err := datasource.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repositoryA := NewRepository(db)
	repositoryB := NewRepository(db)
	now := time.Now().UTC()
	token := PendingToken{MachineID: 101, TenantID: 201, EntityID: 301, OwnerID: 401, TokenHash: hashToken("pending-token"), ExpiresAt: now.Add(time.Minute).UnixMilli(), CreatedAt: now.UnixMilli()}
	if err := repositoryA.StorePendingToken(ctx, token); err != nil {
		t.Fatalf("store pending token: %v", err)
	}
	resolved, err := repositoryB.FindToken(ctx, token.TokenHash)
	if err != nil || resolved.MachineID != token.MachineID || resolved.LongTerm {
		t.Fatalf("shared pending token = (%+v, %v)", resolved, err)
	}
	other := authn.WithClaims(context.Background(), &authn.Claims{TenantID: 202, EntityID: 302, PrincipalID: 402})
	if _, err := repositoryB.PendingTokenForMachine(other, token.MachineID); !errors.Is(err, ErrMachineNotFound) {
		t.Fatalf("cross-scope pending token = %v", err)
	}

	routeA, err := repositoryA.AcquireRoute(ctx, token.MachineID, 901, time.Minute)
	if err != nil {
		t.Fatalf("acquire route A: %v", err)
	}
	if _, err := repositoryB.AcquireRoute(ctx, token.MachineID, 902, time.Minute); !errors.Is(err, ErrRouteHeld) {
		t.Fatalf("duplicate active route = %v", err)
	}
	if _, err := repositoryB.ResolveActiveRoute(other, token.MachineID); !errors.Is(err, ErrMachineNotConnected) {
		t.Fatalf("cross-scope active route = %v", err)
	}
	if routes, err := repositoryB.ListActiveRoutes(other); err != nil || len(routes) != 0 {
		t.Fatalf("cross-scope active routes = (%+v, %v)", routes, err)
	}
	if err := repositoryA.UpdateRouteInventory(ctx, routeA, "build-host", []string{"script", "codex"}, "linux/amd64", "10.0.0.1:1234"); err != nil {
		t.Fatalf("update inventory: %v", err)
	}
	name, runtimes, osName, address, err := repositoryB.RouteInventory(ctx, token.MachineID)
	if err != nil || name != "build-host" || len(runtimes) != 2 || osName != "linux/amd64" || address != "10.0.0.1:1234" {
		t.Fatalf("shared inventory = (%q, %v, %q, %q, %v)", name, runtimes, osName, address, err)
	}
	if current, err := repositoryB.RouteIsCurrent(ctx, routeA); err != nil || !current {
		t.Fatalf("route A current = (%v, %v)", current, err)
	}
	if err := repositoryA.ReleaseRoute(ctx, routeA); err != nil {
		t.Fatalf("release route A: %v", err)
	}
	routeB, err := repositoryB.AcquireRoute(ctx, token.MachineID, 902, time.Minute)
	if err != nil {
		t.Fatalf("take over route B: %v", err)
	}
	if routeB.FencingToken <= routeA.FencingToken || routeB.HolderID != 902 {
		t.Fatalf("takeover did not advance fence: A=%+v B=%+v", routeA, routeB)
	}
	if current, err := repositoryA.RouteIsCurrent(ctx, routeA); err != nil || current {
		t.Fatalf("old route current after takeover = (%v, %v)", current, err)
	}
	if _, err := repositoryA.RenewRoute(ctx, routeA, time.Minute); !errors.Is(err, ErrRouteStale) {
		t.Fatalf("renew old route = %v", err)
	}
	if err := repositoryB.RevokeRoute(ctx, token.MachineID); err != nil {
		t.Fatalf("revoke route B: %v", err)
	}
	if _, err := repositoryB.ResolveActiveRoute(ctx, token.MachineID); !errors.Is(err, ErrMachineNotConnected) {
		t.Fatalf("resolve revoked route = %v", err)
	}
	if err := repositoryA.DeletePendingToken(ctx, token.MachineID, token.TokenHash); err != nil {
		t.Fatalf("delete pending token: %v", err)
	}
	if _, err := repositoryB.FindToken(ctx, token.TokenHash); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("deleted pending token = %v", err)
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
