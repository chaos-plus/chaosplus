package gateway

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// startEmbedded starts an in-memory NATS server (no external broker needed).
func startEmbedded(t *testing.T) *nats.Conn {
	t.Helper()
	opts := &natsserver.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("nats server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(2 * time.Second) {
		t.Fatal("nats server not ready")
	}
	t.Cleanup(srv.Shutdown)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// fakeRunner emulates the daemon side: answers commands on its .cmd subject and
// publishes events on its .evt subject.
func fakeRunner(t *testing.T, nc *nats.Conn, runnerID string) chan string {
	t.Helper()
	cmdSubj := "chaos.runner." + runnerID + ".cmd"
	evtSubj := "chaos.runner." + runnerID + ".evt"
	spawns := make(chan string, 8)

	_, err := nc.Subscribe(cmdSubj, func(m *nats.Msg) {
		var cmd runnerCmd
		if err := json.Unmarshal(m.Data, &cmd); err != nil {
			_ = m.Respond([]byte(`{"ok":false}`))
			return
		}
		switch cmd.Type {
		case "spawn":
			spawns <- cmd.Spawn.SpawnID
			_ = nc.Publish(evtSubj, mustJSON(t, map[string]any{"type": "spawn-started", "spawnId": cmd.Spawn.SpawnID}))
			_ = m.Respond([]byte(`{"ok":true}`))
		case "kill":
			_ = nc.Publish(evtSubj, mustJSON(t, map[string]any{"type": "spawn-done", "spawnId": cmd.SpawnID, "ok": false}))
			_ = m.Respond([]byte(`{"ok":true}`))
		default:
			_ = m.Respond([]byte(`{"ok":true}`))
		}
	})
	if err != nil {
		t.Fatalf("fake runner subscribe: %v", err)
	}
	return spawns
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGatewaySpawnEventKill(t *testing.T) {
	nc := startEmbedded(t)
	g := New(nc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = g.Start(ctx) }()
	time.Sleep(100 * time.Millisecond) // let subscriptions register

	spawns := fakeRunner(t, nc, "runner-a")

	// register → should be tracked
	if _, err := nc.Request("chaos.runner.register", mustJSON(t, map[string]any{"runnerId": "runner-a"}), time.Second); err != nil {
		t.Fatalf("register: %v", err)
	}
	if got := g.RegisteredRunners(); len(got) != 1 || got[0] != "runner-a" {
		t.Fatalf("RegisteredRunners = %v, want [runner-a]", got)
	}

	// spawn → runner receives it, event flows back
	if err := g.Spawn(ctx, "runner-a", Spawn{SpawnID: "sp1", RunID: "r1", NodeID: "n1", Attempt: 1, ExecutorType: "mock", Prompt: "hi", Cwd: "/tmp"}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	select {
	case id := <-spawns:
		if id != "sp1" {
			t.Fatalf("runner got spawn %q, want sp1", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not receive spawn")
	}
	select {
	case ev := <-g.Events():
		if ev.RunnerID != "runner-a" || ev.Type != "spawn-started" {
			t.Fatalf("event = %+v, want runner-a spawn-started", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("gateway did not receive spawn-started event")
	}

	// kill → runner replies ok
	if err := g.Kill(ctx, "runner-a", "sp1"); err != nil {
		t.Fatalf("kill: %v", err)
	}
}

func TestGatewaySpawnToUnknownRunnerFails(t *testing.T) {
	nc := startEmbedded(t)
	g := New(nc)
	ctx := context.Background()
	// no fake runner on "ghost" → request times out / no responder
	err := g.Spawn(ctx, "ghost", Spawn{SpawnID: "spX", Prompt: "hi", Cwd: "/"})
	if err == nil {
		t.Fatal("expected error for unknown runner")
	}
}

// TestLiveSpawnAgainstRealDaemon is a cross-end smoke test (Go server-ai ↔
// TS daemon) against the shared NATS. Gated by CONTROL_SMOKE=1 like the server's
// TestRemoteSpiceDBSmoke. Requires a daemon registered as runner <RUNNER_ID>.
func TestLiveSpawnAgainstRealDaemon(t *testing.T) {
	if os.Getenv("CONTROL_SMOKE") == "" {
		t.Skip("set CONTROL_SMOKE=1 to run against the shared NATS")
	}
	url := envOrSmoke("CONTROL_NATS_URL", "nats://10.0.0.100:4222")
	runnerID := envOrSmoke("RUNNER_ID", "cptest")
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect %s: %v", url, err)
	}
	t.Cleanup(nc.Close)

	g := New(nc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = g.Start(ctx) }()
	time.Sleep(300 * time.Millisecond)

	if err := g.Spawn(ctx, runnerID, Spawn{
		SpawnID: "live1", RunID: "lr1", NodeID: "n1", Attempt: 1,
		ExecutorType: "mock", Prompt: "live cross-end", Cwd: "/tmp",
	}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-g.Events():
			if ev.RunnerID == runnerID && ev.Type == "spawn-done" {
				return // cross-end round-trip succeeded
			}
		case <-deadline:
			t.Fatal("daemon did not report spawn-done")
		}
	}
}

func envOrSmoke(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
