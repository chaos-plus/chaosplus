package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// cmdRunner answers run-cmd / switch-provider and echoes events.
func cmdRunner(t *testing.T, nc *nats.Conn, runnerID string, exit int, fail bool) {
	t.Helper()
	cmdSubj := "chaos.runner." + runnerID + ".cmd"
	_, err := nc.Subscribe(cmdSubj, func(m *nats.Msg) {
		var cmd runnerCmd
		if err := json.Unmarshal(m.Data, &cmd); err != nil {
			_ = m.Respond([]byte(`{"ok":false,"data":{"error":"bad payload"}}`))
			return
		}
		if fail {
			_ = m.Respond([]byte(`{"ok":false,"data":{"error":"runner refused"}}`))
			return
		}
		switch cmd.Type {
		case "run-cmd":
			resp, _ := json.Marshal(map[string]any{"ok": true, "data": map[string]any{"exitCode": exit, "stdout": "hello", "stderr": "warn"}})
			_ = m.Respond(resp)
		default:
			_ = m.Respond([]byte(`{"ok":true}`))
		}
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
}

func newStartedGateway(t *testing.T) (*Gateway, *nats.Conn) {
	t.Helper()
	nc := startEmbedded(t)
	g := New(nc)
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	if err := g.Start(ctx); err != nil {
		t.Fatalf("start gateway: %v", err)
	}
	return g, nc
}

func TestGatewayRunCmdReturnsExitAndStreams(t *testing.T) {
	g, nc := newStartedGateway(t)
	cmdRunner(t, nc, "r-cmd", 3, false)

	res, err := g.RunCmd(context.Background(), "r-cmd", "s1", "go test ./...", 5000)
	if err != nil {
		t.Fatalf("RunCmd: %v", err)
	}
	if res.ExitCode != 3 || res.Stdout != "hello" || res.Stderr != "warn" {
		t.Fatalf("RunCmd result wrong: %+v", res)
	}
}

func TestGatewayRunCmdSurfacesRunnerError(t *testing.T) {
	g, nc := newStartedGateway(t)
	cmdRunner(t, nc, "r-bad", 0, true)

	if _, err := g.RunCmd(context.Background(), "r-bad", "s1", "boom", 2000); err == nil {
		t.Fatal("a refusing runner must surface an error")
	}
}

func TestGatewayRunCmdOnUnknownRunnerTimesOut(t *testing.T) {
	g, _ := newStartedGateway(t)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, err := g.RunCmd(ctx, "ghost", "s1", "x", 500); err == nil {
		t.Fatal("no responder must produce an error")
	}
}

func TestGatewaySwitchProvider(t *testing.T) {
	g, nc := newStartedGateway(t)
	cmdRunner(t, nc, "r-sp", 0, false)

	if err := g.SwitchProvider(context.Background(), "r-sp", "s1", "bedrock", "key-123"); err != nil {
		t.Fatalf("SwitchProvider: %v", err)
	}

	cmdRunner(t, nc, "r-sp-bad", 0, true)
	if err := g.SwitchProvider(context.Background(), "r-sp-bad", "s1", "vertex", "k"); err == nil {
		t.Fatal("a refusing runner must surface an error")
	}
}

// OnEvent 回调应收到 runner 事件(与 Events() 通道等价的推送路径)。
func TestGatewayOnEventCallback(t *testing.T) {
	g, nc := newStartedGateway(t)

	got := make(chan RunnerEvent, 4)
	g.OnEvent(func(ev RunnerEvent) { got <- ev })

	time.Sleep(80 * time.Millisecond)
	_ = nc.Publish("chaos.runner.r-evt.evt", []byte(`{"type":"spawn-done","spawnId":"s9","ok":true}`))

	select {
	case ev := <-got:
		if ev.Type != "spawn-done" || ev.RunnerID != "r-evt" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OnEvent callback never fired")
	}
}
