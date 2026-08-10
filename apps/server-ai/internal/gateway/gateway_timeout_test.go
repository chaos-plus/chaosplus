package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// fakeIdleRunner emits spawn events at a fixed interval and only ends the spawn
// after the server-ai's idle window would have elapsed (testing reset).
func fakeIdleRunner(t *testing.T, nc *nats.Conn, runnerID string, events []time.Duration) {
	t.Helper()
	cmdSubj := "chaos.runner." + runnerID + ".cmd"
	evtSubj := "chaos.runner." + runnerID + ".evt"
	_, err := nc.Subscribe(cmdSubj, func(m *nats.Msg) {
		var cmd runnerCmd
		if err := json.Unmarshal(m.Data, &cmd); err != nil {
			_ = m.Respond([]byte(`{"ok":false}`))
			return
		}
		_ = m.Respond([]byte(`{"ok":true}`))
		for i, gap := range events {
			time.Sleep(gap)
			if i == len(events)-1 {
				_ = nc.Publish(evtSubj, mustJSON(t, map[string]any{"type": "spawn-done", "spawnId": cmd.Spawn.SpawnID, "ok": true}))
			} else {
				_ = nc.Publish(evtSubj, mustJSON(t, map[string]any{"type": "spawn-event", "spawnId": cmd.Spawn.SpawnID}))
			}
		}
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
}

// TestSpawnAndWaitIdleReset: a stream of activity events keeps resetting the
// idle timer, so a long-running agent that keeps producing output does NOT time
// out even past the nominal idle window.
func TestSpawnAndWaitIdleReset(t *testing.T) {
	nc := startEmbedded(t)
	g := New(nc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = g.Start(ctx) }()
	time.Sleep(100 * time.Millisecond)

	// 6 events, 150ms apart, total ~900ms — far beyond a 400ms idle window.
	fakeIdleRunner(t, nc, "runner-idle", []time.Duration{
		150 * time.Millisecond, 150 * time.Millisecond, 150 * time.Millisecond,
		150 * time.Millisecond, 150 * time.Millisecond, 150 * time.Millisecond,
	})

	res, err := g.SpawnAndWaitOpts(ctx, "runner-idle", Spawn{SpawnID: "sp-idle", Prompt: "hi", Cwd: "/"},
		WithIdleTimeout(400*time.Millisecond), WithMaxTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("spawn+wait: %v", err)
	}
	if !res.OK {
		t.Fatalf("want ok, got %+v", res)
	}
}

// TestSpawnAndWaitIdleTimeout: no activity within the idle window → timeout,
// even though the run may still be alive upstream.
func TestSpawnAndWaitIdleTimeout(t *testing.T) {
	nc := startEmbedded(t)
	g := New(nc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = g.Start(ctx) }()
	time.Sleep(100 * time.Millisecond)

	// Single event after 800ms with a 300ms idle window → idle fires first.
	fakeIdleRunner(t, nc, "runner-stall", []time.Duration{800 * time.Millisecond})

	_, err := g.SpawnAndWaitOpts(ctx, "runner-stall", Spawn{SpawnID: "sp-stall", Prompt: "hi", Cwd: "/"},
		WithIdleTimeout(300*time.Millisecond), WithMaxTimeout(5*time.Second))
	if err == nil {
		t.Fatal("want idle timeout error, got nil")
	}
}

// TestSpawnAndWaitMaxTimeout: a flood of activity still cannot exceed max.
func TestSpawnAndWaitMaxTimeout(t *testing.T) {
	nc := startEmbedded(t)
	g := New(nc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = g.Start(ctx) }()
	time.Sleep(100 * time.Millisecond)

	// Events every 150ms forever-ish; max 500ms must cut it short.
	fakeIdleRunner(t, nc, "runner-max", []time.Duration{
		150 * time.Millisecond, 150 * time.Millisecond, 150 * time.Millisecond, 150 * time.Millisecond,
		150 * time.Millisecond, 150 * time.Millisecond, 150 * time.Millisecond, 150 * time.Millisecond,
	})

	_, err := g.SpawnAndWaitOpts(ctx, "runner-max", Spawn{SpawnID: "sp-max", Prompt: "hi", Cwd: "/"},
		WithIdleTimeout(10*time.Minute), WithMaxTimeout(500*time.Millisecond))
	if err == nil {
		t.Fatal("want max timeout error, got nil")
	}
}
