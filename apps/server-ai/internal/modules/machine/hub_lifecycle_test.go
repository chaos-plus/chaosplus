package machine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secure"
	coreid "github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func machineTestContext() context.Context {
	return authn.WithClaims(context.Background(), &authn.Claims{
		TenantID: 201, EntityID: 301, PrincipalID: 401,
	})
}

func newHubWithStore(t *testing.T) (*Hub, *TokenStore, *BunRepository, *httptest.Server) {
	t.Helper()
	datasource := bunx.Datasource{Type: "sqlite", Dsn: filepath.Join(t.TempDir(), "hub.db"), Writable: true}
	db, err := datasource.Open()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate machine: %v", err)
	}
	repository := NewRepository(db)

	nc := startTestNATS(t)
	tokens := NewTokenStore()
	hub := NewHub(nc, tokens, repository, testNextID(), secure.SameOriginPolicy())
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	t.Cleanup(ts.Close)
	return hub, tokens, repository, ts
}

// PRD §5.3.1:一次性令牌 → 握手 → 确认 → 长期令牌(仅手动轮换)。
func TestMachineOnboardingLifecycle(t *testing.T) {
	hub, _, st, ts := newHubWithStore(t)
	ctx := machineTestContext()

	machineID, token, expiresAt, err := hub.IssueTokenFor(ctx)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if machineID.Zero() || token == "" {
		t.Fatal("IssueToken returned empty values")
	}
	if remaining := time.Until(expiresAt); remaining < TokenTTL-time.Second || remaining > TokenTTL+time.Second {
		t.Fatalf("onboarding token lifetime = %v, want %v", remaining, TokenTTL)
	}
	if err := hub.Confirm(ctx, machineID, token); err != ErrMachineNotConnected {
		t.Fatalf("confirm before connection = %v, want %v", err, ErrMachineNotConnected)
	}
	if state, _ := hub.OnboardingStatus(machineID, token); state != "waiting" {
		t.Fatalf("initial onboarding state = %q, want waiting", state)
	}

	ws := dialDaemon(t, ts, token)
	var ready map[string]any
	if err := ws.ReadJSON(&ready); err != nil {
		t.Fatalf("read ready: %v", err)
	}
	if state, _ := hub.OnboardingStatus(machineID, token); state != "connected" {
		t.Fatalf("connected onboarding state = %q, want connected", state)
	}
	_ = ws.WriteJSON(map[string]any{"type": "register", "meta": map[string]string{"name": "box"}})

	// 在线后进入 RegisteredRunners,名字可查。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(hub.RegisteredRunners()) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	runners := hub.RegisteredRunners()
	if len(runners) == 0 {
		t.Fatal("connected daemon is not in RegisteredRunners")
	}
	// register 是异步的,等名字落进 hub。
	nameDeadline := time.Now().Add(3 * time.Second)
	name := ""
	for time.Now().Before(nameDeadline) {
		if name = hub.MachineName(machineID); name != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if name == "" {
		t.Fatalf("MachineName empty for %q", runners[0])
	}

	// 确认 → 落库为 confirmed。
	if err := hub.Confirm(ctx, machineID, token); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if state, _ := hub.OnboardingStatus(machineID, token); state != "confirmed" {
		t.Fatalf("confirmed onboarding state = %q, want confirmed", state)
	}
	list, err := hub.ListMachines(ctx)
	if err != nil {
		t.Fatalf("list machines: %v", err)
	}
	found := false
	for _, m := range list {
		if m.ID == machineID {
			found = true
			if m.Status != "confirmed" {
				t.Errorf("status = %q, want confirmed", m.Status)
			}
			if m.TokenHash == "" {
				t.Error("long-term token hash not stored")
			}
		}
	}
	if !found {
		t.Fatalf("confirmed machine missing from ListMachines: %+v", list)
	}

	// 错误令牌不能确认。
	if err := hub.Confirm(ctx, machineID, "wrong-token"); err == nil {
		t.Error("confirm with a wrong token must fail")
	}

	// 手动轮换:换出新令牌,旧令牌失效。
	rotated, err := hub.RefreshToken(ctx, machineID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if rotated == "" || rotated == token {
		t.Fatalf("rotated token must be new and non-empty (old=%q new=%q)", token, rotated)
	}
	if err := hub.Confirm(ctx, machineID, token); err == nil {
		t.Error("the old token must stop working after rotation")
	}

	// 断连不删除机器(重连仍是同一台)。
	hub.Disconnect(machineID)
	if list, _ := hub.ListMachines(ctx); len(list) == 0 {
		t.Error("Disconnect must not remove the machine record")
	}

	// 取消:彻底移除。
	if err := hub.Cancel(ctx, machineID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	after, _ := st.List(ctx)
	for _, m := range after {
		if m.ID == machineID {
			t.Fatalf("Cancel should remove the machine, still present: %+v", m)
		}
	}
}

func TestRefreshTokenUnknownMachine(t *testing.T) {
	hub, _, _, _ := newHubWithStore(t)
	if _, err := hub.RefreshToken(machineTestContext(), coreid.ID(999)); err == nil {
		t.Fatal("refreshing an unknown machine must fail")
	}
}

func TestIssueLongTermTokenValidatesAndPersists(t *testing.T) {
	ts := NewTokenStore()
	machineID := coreid.ID(501)
	tok := ts.IssueLongTerm(machineID, 201, 301, 401)
	if tok.Token == "" {
		t.Fatal("empty long-term token")
	}
	got, err := ts.Validate(tok.Token)
	if err != nil || got == nil || got.MachineID != machineID {
		t.Fatalf("Validate = (%+v, %v), want machine %s", got, err, machineID)
	}
	ts.Invalidate(machineID)
	if _, err := ts.Validate(tok.Token); err == nil {
		t.Fatal("invalidated token must not validate")
	}
}
