package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/gateway"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/store"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/workflow"
)

func silentRunner(t *testing.T, nc *nats.Conn, runnerID string, spawns *atomic.Int32) {
	t.Helper()
	_, err := nc.Subscribe("chaos.runner."+runnerID+".cmd", func(msg *nats.Msg) {
		var command struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(msg.Data, &command) != nil {
			_ = msg.Respond([]byte(`{"ok":false,"data":{"error":"bad command"}}`))
			return
		}
		if command.Type == "spawn" {
			spawns.Add(1)
		}
		_ = msg.Respond([]byte(`{"ok":true}`))
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunnerHeartbeatLossRetriesThenPausesRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	nc := startTestNATS(t)
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "heartbeat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	g := gateway.New(nc)
	go func() { _ = g.Start(ctx) }()
	time.Sleep(100 * time.Millisecond)
	var spawns atomic.Int32
	silentRunner(t, nc, "runner-silent", &spawns)
	if _, err := nc.Request("chaos.runner.register", []byte(`{"runnerId":"runner-silent"}`), time.Second); err != nil {
		t.Fatal(err)
	}
	m := NewRunManager(nc, &workflow.NatsRunnerLink{G: g}, st, "runner-silent")
	m.heartbeatTimeout = 200 * time.Millisecond
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	def := json.RawMessage(`{
	  "id":"heartbeat-recovery","version":"1",
	  "nodes":[{"id":"agent","type":"agent","agent":{
	    "id":"agent","role":"dev","executor":"mock",
	    "retry":{"maxAttempts":2,"backoffSeconds":[0],"notifyThreshold":1}
	  }}],"edges":[]
	}`)
	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: def, Workspace: t.TempDir(), InstanceID: "i", ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return run.Status() == RunPaused }, 4*time.Second)
	if spawns.Load() != 2 {
		t.Fatalf("heartbeat recovery spawned %d attempts, want 2", spawns.Load())
	}
	var started, retrying, paused int
	for _, event := range run.Events() {
		if event.NodeID != "agent" {
			continue
		}
		switch event.Status {
		case workflow.StatusRunning:
			started++
		case workflow.StatusRetrying:
			retrying++
		case workflow.StatusPausedForHuman:
			paused++
		}
	}
	if started != 2 || retrying != 1 || paused != 1 {
		t.Fatalf("heartbeat events started=%d retrying=%d paused=%d", started, retrying, paused)
	}
	persisted, err := st.GetRunDef(ctx, run.ID)
	if err != nil || persisted.Status != string(RunPaused) {
		t.Fatalf("persisted run after heartbeat loss = (%+v, %v)", persisted, err)
	}
}
