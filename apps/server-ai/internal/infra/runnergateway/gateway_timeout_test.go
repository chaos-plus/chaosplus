package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
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

func TestSpawnAndWaitHeartbeatLost(t *testing.T) {
	nc := startEmbedded(t)
	g := New(nc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := g.Start(ctx); err != nil {
		t.Fatalf("start gateway: %v", err)
	}
	fakeIdleRunner(t, nc, "runner-lost", []time.Duration{2 * time.Second})

	_, err := g.SpawnAndWaitOpts(ctx, "runner-lost", Spawn{SpawnID: "sp-lost", Prompt: "hi", Cwd: "/"},
		WithIdleTimeout(5*time.Second), WithMaxTimeout(5*time.Second), WithHeartbeatTimeout(250*time.Millisecond))
	if !errors.Is(err, ErrRunnerHeartbeatLost) {
		t.Fatalf("heartbeat loss = %v, want ErrRunnerHeartbeatLost", err)
	}
}

func TestSpawnAndWaitHeartbeatActivityPreventsFalsePositive(t *testing.T) {
	nc := startEmbedded(t)
	g := New(nc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := g.Start(ctx); err != nil {
		t.Fatalf("start gateway: %v", err)
	}
	cmdSubj := "chaos.runner.runner-live.cmd"
	evtSubj := "chaos.runner.runner-live.evt"
	_, err := nc.Subscribe(cmdSubj, func(m *nats.Msg) {
		var cmd runnerCmd
		_ = json.Unmarshal(m.Data, &cmd)
		_ = m.Respond([]byte(`{"ok":true}`))
		go func() {
			for range 6 {
				time.Sleep(100 * time.Millisecond)
				_ = nc.Publish(evtSubj, mustJSON(t, map[string]any{"type": "heartbeat"}))
			}
			_ = nc.Publish(evtSubj, mustJSON(t, map[string]any{"type": "spawn-done", "spawnId": cmd.Spawn.SpawnID, "ok": true}))
		}()
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := g.SpawnAndWaitOpts(ctx, "runner-live", Spawn{SpawnID: "sp-live", Prompt: "hi", Cwd: "/"},
		WithIdleTimeout(2*time.Second), WithMaxTimeout(3*time.Second), WithHeartbeatTimeout(250*time.Millisecond))
	if err != nil || !res.OK {
		t.Fatalf("live runner result = (%+v, %v)", res, err)
	}
}

func TestConcurrentSpawnWaitersDoNotConsumeEachOthersEvents(t *testing.T) {
	nc := startEmbedded(t)
	g := New(nc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := g.Start(ctx); err != nil {
		t.Fatalf("start gateway: %v", err)
	}
	cmdSubj := "chaos.runner.runner-concurrent.cmd"
	evtSubj := "chaos.runner.runner-concurrent.evt"
	_, err := nc.Subscribe(cmdSubj, func(m *nats.Msg) {
		var cmd runnerCmd
		_ = json.Unmarshal(m.Data, &cmd)
		_ = m.Respond([]byte(`{"ok":true}`))
		go func(spawnID string) {
			time.Sleep(50 * time.Millisecond)
			_ = nc.Publish(evtSubj, mustJSON(t, map[string]any{"type": "spawn-done", "spawnId": spawnID, "ok": true}))
		}(cmd.Spawn.SpawnID)
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, spawnID := range []string{"sp-a", "sp-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := g.SpawnAndWaitOpts(ctx, "runner-concurrent", Spawn{SpawnID: spawnID, Prompt: "hi", Cwd: "/"},
				WithIdleTimeout(time.Second), WithMaxTimeout(2*time.Second), WithHeartbeatTimeout(time.Second))
			if err != nil {
				errs <- err
				return
			}
			if !res.OK {
				errs <- errors.New("spawn returned not ok")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
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
	if err := g.Start(ctx); err != nil {
		t.Fatalf("start gateway: %v", err)
	}

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
	if err := g.Start(ctx); err != nil {
		t.Fatalf("start gateway: %v", err)
	}

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
	if err := g.Start(ctx); err != nil {
		t.Fatalf("start gateway: %v", err)
	}

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
