package machine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/gateway"
)

func startTestNATS(t *testing.T) *nats.Conn {
	t.Helper()
	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatalf("nats server: %v", err)
	}
	go ns.Start()
	t.Cleanup(ns.Shutdown)
	if !ns.ReadyForConnections(2 * time.Second) {
		t.Fatal("nats not ready")
	}
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

func dialDaemon(t *testing.T, ts *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/machines/ws?token=" + token + "&name=box"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func TestHubHandshakeInvalidToken(t *testing.T) {
	nc := startTestNATS(t)
	hub := NewHub(nc, NewTokenStore(), nil)
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/machines/ws?token=garbage")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid token status = %d, want 401", resp.StatusCode)
	}
}

// TestHubBridgeSpawn covers the full bridge path:
// control-plane gateway → NATS chaos.runner.{id}.cmd → hub → WS cmd → daemon
// reply → hub → NATS reply → gateway; and daemon event → hub → NATS evt → gateway.
func TestHubBridgeSpawn(t *testing.T) {
	nc := startTestNATS(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	g := gateway.New(nc)
	go g.Start(ctx)
	time.Sleep(150 * time.Millisecond)

	tokens := NewTokenStore()
	hub := NewHub(nc, tokens, nil)
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer ts.Close()

	at := tokens.Issue("m1")
	ws := dialDaemon(t, ts, at.Token)
	var ready map[string]any
	if err := ws.ReadJSON(&ready); err != nil {
		t.Fatalf("read ready: %v", err)
	}

	// daemon registers → gateway should see the runner.
	_ = ws.WriteJSON(map[string]any{"type": "register", "meta": map[string]string{"name": "box"}})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(g.RegisteredRunners()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(g.RegisteredRunners()) != 1 || g.RegisteredRunners()[0] != "m1" {
		t.Fatalf("gateway registered = %v, want [m1]", g.RegisteredRunners())
	}

	// gateway spawns; daemon replies + sends spawn-done over WS.
	done := make(chan error, 1)
	go func() {
		res, err := g.SpawnAndWaitOpts(ctx, "m1", gateway.Spawn{SpawnID: "s1", RunID: "r", NodeID: "n", ExecutorType: "mock", Prompt: "hi"}, gateway.WithMaxTimeout(5*time.Second))
		if err != nil {
			done <- err
			return
		}
		if !res.OK || res.ExitCode != 0 {
			done <- &badResult{}
			return
		}
		done <- nil
	}()

	var cmdMsg struct {
		Type  string `json:"type"`
		ReqID int64  `json:"reqId"`
		Cmd   struct {
			Type  string `json:"type"`
			Spawn *struct {
				SpawnID string `json:"spawnId"`
			} `json:"spawn"`
		} `json:"cmd"`
	}
	if err := ws.ReadJSON(&cmdMsg); err != nil {
		t.Fatalf("read cmd: %v", err)
	}
	if cmdMsg.Type != "cmd" || cmdMsg.Cmd.Type != "spawn" || cmdMsg.Cmd.Spawn.SpawnID != "s1" {
		t.Fatalf("unexpected forwarded cmd: %+v", cmdMsg)
	}
	_ = ws.WriteJSON(map[string]any{"type": "reply", "reqId": cmdMsg.ReqID, "ok": true, "data": map[string]any{"agentId": "a"}})
	_ = ws.WriteJSON(map[string]any{"type": "event", "event": map[string]any{"type": "spawn-done", "spawnId": "s1", "ok": true, "exitCode": 0}})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("spawn: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("gateway SpawnAndWait did not return")
	}
}

// TestHubUnconfirmedDisconnectInvalidates: a pending machine that disconnects
// loses its one-time token (§5.3.1).
func TestHubUnconfirmedDisconnectInvalidates(t *testing.T) {
	nc := startTestNATS(t)
	tokens := NewTokenStore()
	hub := NewHub(nc, tokens, nil)
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer ts.Close()

	at := tokens.Issue("m1")
	ws := dialDaemon(t, ts, at.Token)
	var ready map[string]any
	_ = ws.ReadJSON(&ready)

	_ = ws.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && hub.IsConnected("m1") {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.IsConnected("m1") {
		t.Fatal("machine still connected after disconnect")
	}
	if _, err := tokens.Validate(at.Token); err == nil {
		t.Fatal("unconfirmed token should be invalidated on disconnect")
	}
}

// badResult satisfies error so the success-path goroutine compiles.
type badResult struct{}

func (b *badResult) Error() string { return "unexpected spawn result" }
