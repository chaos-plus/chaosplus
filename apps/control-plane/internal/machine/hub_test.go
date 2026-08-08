package machine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/gateway"
)

func dialHub(t *testing.T, ts *httptest.Server, token string) *websocket.Conn {
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
	tokens := NewTokenStore()
	hub := NewHub(tokens, nil)
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

func TestHubHandshakeAndSpawn(t *testing.T) {
	tokens := NewTokenStore()
	hub := NewHub(tokens, nil)
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer ts.Close()

	at := tokens.Issue("m1")
	ws := dialHub(t, ts, at.Token)

	// 收到 ready。
	var m map[string]any
	if err := ws.ReadJSON(&m); err != nil {
		t.Fatalf("read ready: %v", err)
	}
	if m["type"] != "ready" {
		t.Fatalf("got %v, want ready", m["type"])
	}
	if len(hub.RegisteredRunners()) != 1 || hub.RegisteredRunners()[0] != "m1" {
		t.Fatalf("registered = %v, want [m1]", hub.RegisteredRunners())
	}

	// 并行跑 SpawnAndWait,daemon 侧先回 reply 再发 spawn-done。
	done := make(chan error, 1)
	go func() {
		res, err := hub.SpawnAndWait(context.Background(), "m1", gateway.Spawn{SpawnID: "s1", RunID: "r", NodeID: "n", ExecutorType: "mock", Prompt: "hi"}, 0, 5*time.Second)
		if err != nil {
			done <- err
			return
		}
		if !res.OK || res.ExitCode != 0 {
			done <- fmt.Errorf("unexpected spawn result %+v", res)
			return
		}
		done <- nil
	}()

	// daemon 侧读 spawn cmd。
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
		t.Fatalf("unexpected cmd: %+v", cmdMsg)
	}
	// 回 reply + spawn-done。
	_ = ws.WriteJSON(map[string]any{"type": "reply", "reqId": cmdMsg.ReqID, "ok": true, "data": map[string]any{"agentId": "a"}})
	_ = ws.WriteJSON(map[string]any{"type": "event", "event": map[string]any{"type": "spawn-done", "spawnId": "s1", "ok": true, "exitCode": 0}})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("spawn: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SpawnAndWait did not return")
	}

	// 断线后 unregister + 一次性 token 失效。
	_ = ws.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(hub.RegisteredRunners()) != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(hub.RegisteredRunners()) != 0 {
		t.Fatal("runner still registered after disconnect")
	}
	if _, err := tokens.Validate(at.Token); err == nil {
		t.Fatal("unconfirmed token should be invalidated on disconnect")
	}
}

func TestHubReadArtifact(t *testing.T) {
	tokens := NewTokenStore()
	hub := NewHub(tokens, nil)
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer ts.Close()

	at := tokens.Issue("m1")
	ws := dialHub(t, ts, at.Token)
	var m map[string]any
	_ = ws.ReadJSON(&m) // ready

	done := make(chan []byte, 1)
	go func() {
		b, _ := hub.ReadArtifact(context.Background(), "m1", "s1", "output.json")
		done <- b
	}()
	var cmdMsg struct {
		Type  string `json:"type"`
		ReqID int64  `json:"reqId"`
		Cmd   struct {
			Type   string `json:"type"`
			SpawnID string `json:"spawnId"`
			Path   string `json:"path"`
		} `json:"cmd"`
	}
	_ = ws.ReadJSON(&cmdMsg)
	_ = ws.WriteJSON(map[string]any{"type": "reply", "reqId": cmdMsg.ReqID, "ok": true, "data": map[string]any{"content": "{\"ok\":true}"}})
	select {
	case b := <-done:
		if string(b) != `{"ok":true}` {
			t.Fatalf("artifact = %s", b)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReadArtifact did not return")
	}
}
