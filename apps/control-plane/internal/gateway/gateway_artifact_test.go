package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// fakeArtifactRunner answers spawn + read-file on its .cmd subject and emits
// the matching event stream, emulating the daemon's contract.
func fakeArtifactRunner(t *testing.T, nc *nats.Conn, runnerID string) {
	t.Helper()
	cmdSubj := "chaos.runner." + runnerID + ".cmd"
	evtSubj := "chaos.runner." + runnerID + ".evt"
	_, err := nc.Subscribe(cmdSubj, func(m *nats.Msg) {
		var cmd runnerCmd
		if err := json.Unmarshal(m.Data, &cmd); err != nil {
			_ = m.Respond([]byte(`{"ok":false,"data":{"error":"bad payload"}}`))
			return
		}
		switch cmd.Type {
		case "spawn":
			_ = nc.Publish(evtSubj, mustJSON(t, map[string]any{"type": "spawn-started", "spawnId": cmd.Spawn.SpawnID}))
			_ = nc.Publish(evtSubj, mustJSON(t, map[string]any{"type": "spawn-done", "spawnId": cmd.Spawn.SpawnID, "ok": true, "exitCode": 0}))
			_ = m.Respond([]byte(`{"ok":true}`))
		case "read-file":
			_ = m.Respond([]byte(`{"ok":true,"data":{"content":"{\"ok\":true}"}}`))
		default:
			_ = m.Respond([]byte(`{"ok":true}`))
		}
	})
	if err != nil {
		t.Fatalf("fake artifact runner subscribe: %v", err)
	}
}

func TestGatewaySpawnAndWaitAndReadArtifact(t *testing.T) {
	nc := startEmbedded(t)
	g := New(nc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = g.Start(ctx) }()
	time.Sleep(100 * time.Millisecond)

	fakeArtifactRunner(t, nc, "runner-art")

	res, err := g.SpawnAndWait(ctx, "runner-art", Spawn{
		SpawnID: "sp-art", RunID: "r1", NodeID: "n1", Attempt: 1,
		ExecutorType: "claude", Prompt: "hi", Cwd: "/ws",
	})
	if err != nil {
		t.Fatalf("spawn+wait: %v", err)
	}
	if !res.OK {
		t.Fatalf("spawn result = %+v, want ok", res)
	}

	content, err := g.ReadArtifact(ctx, "runner-art", "sp-art", "output.json")
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if string(content) != `{"ok":true}` {
		t.Fatalf("artifact content = %q, want {\"ok\":true}", string(content))
	}
}

func TestGatewaySpawnAndWaitErrorEvent(t *testing.T) {
	nc := startEmbedded(t)
	g := New(nc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = g.Start(ctx) }()
	time.Sleep(100 * time.Millisecond)

	cmdSubj := "chaos.runner." + "runner-err" + ".cmd"
	evtSubj := "chaos.runner." + "runner-err" + ".evt"
	_, err := nc.Subscribe(cmdSubj, func(m *nats.Msg) {
		var cmd runnerCmd
		_ = json.Unmarshal(m.Data, &cmd)
		_ = nc.Publish(evtSubj, mustJSON(t, map[string]any{"type": "spawn-error", "spawnId": cmd.Spawn.SpawnID, "message": "boom"}))
		_ = m.Respond([]byte(`{"ok":true}`))
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	res, err := g.SpawnAndWait(ctx, "runner-err", Spawn{SpawnID: "sp-e", RunID: "r1", NodeID: "n1", Prompt: "hi", Cwd: "/"})
	if err != nil {
		t.Fatalf("spawn+wait: %v", err)
	}
	if res.OK {
		t.Fatal("want ok=false on spawn-error")
	}
	if res.Error != "boom" {
		t.Fatalf("error = %q, want boom", res.Error)
	}
}
