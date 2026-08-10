package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/gateway"
)

func startNATS(t *testing.T) *nats.Conn {
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
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// execRunner answers spawn / read-file / run-cmd over real NATS.
func execRunner(t *testing.T, nc *nats.Conn, runnerID, artifact string, spawnOK bool, cmdExit int) {
	t.Helper()
	cmdSubj := "chaos.runner." + runnerID + ".cmd"
	evtSubj := "chaos.runner." + runnerID + ".evt"
	_, err := nc.Subscribe(cmdSubj, func(m *nats.Msg) {
		var cmd struct {
			Type  string `json:"type"`
			Path  string `json:"path"`
			Spawn struct {
				SpawnID string `json:"spawnId"`
			} `json:"spawn"`
		}
		if err := json.Unmarshal(m.Data, &cmd); err != nil {
			_ = m.Respond([]byte(`{"ok":false,"data":{"error":"bad"}}`))
			return
		}
		switch cmd.Type {
		case "spawn":
			_ = m.Respond([]byte(`{"ok":true}`))
			go func() {
				time.Sleep(80 * time.Millisecond)
				if spawnOK {
					d, _ := json.Marshal(map[string]any{"type": "spawn-done", "spawnId": cmd.Spawn.SpawnID, "ok": true, "exitCode": 0})
					_ = nc.Publish(evtSubj, d)
					return
				}
				e, _ := json.Marshal(map[string]any{"type": "spawn-error", "spawnId": cmd.Spawn.SpawnID, "message": "agent crashed"})
				_ = nc.Publish(evtSubj, e)
			}()
		case "read-file":
			// Only serve content for output.json; other paths get an error
			// so missing-artifact tests work correctly.
			if cmd.Path == "output.json" {
				resp, _ := json.Marshal(map[string]any{"ok": true, "data": map[string]any{"content": artifact}})
				_ = m.Respond(resp)
			} else {
				_ = m.Respond([]byte(`{"ok":false,"data":{"error":"file not found"}}`))
			}
		case "run-cmd":
			resp, _ := json.Marshal(map[string]any{"ok": true, "data": map[string]any{"exitCode": cmdExit, "stdout": "out", "stderr": ""}})
			_ = m.Respond(resp)
		default:
			_ = m.Respond([]byte(`{"ok":true}`))
		}
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
}

func newLink(t *testing.T, artifact string, spawnOK bool, cmdExit int) *NatsRunnerLink {
	t.Helper()
	nc := startNATS(t)
	g := gateway.New(nc)
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	go func() { _ = g.Start(ctx) }()
	time.Sleep(120 * time.Millisecond)
	execRunner(t, nc, "r1", artifact, spawnOK, cmdExit)
	if _, err := nc.Request("chaos.runner.register", []byte(`{"runnerId":"r1"}`), 2*time.Second); err != nil {
		t.Fatalf("register: %v", err)
	}
	return &NatsRunnerLink{G: g}
}

func agentNode(validator string) *Node {
	n := &Node{ID: "a0", Type: NodeAgent, Agent: &ExecutorAgentSpec{
		ID: "a0", Role: "dev", Executor: "claude", SystemPrompt: "你是开发",
	}}
	if validator != "" {
		v := validator
		n.Agent.OutputSpec = &OutputSpec{OutputValidator: &v}
	}
	return n
}

func TestRunnerExecutorReadsAgentOutput(t *testing.T) {
	link := newLink(t, `{"ok":true,"summary":"done"}`, true, 0)
	ex := NewRunnerExecutor(link, "r1", t.TempDir(), "run-1").WithSpawnTimeout(5*time.Second, 20*time.Second)

	out, err := ex.RunAgent(context.Background(), agentNode(""), json.RawMessage(`{"task":"x"}`))
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Output, &got); err != nil {
		t.Fatalf("output not JSON: %s", string(out.Output))
	}
	if got["summary"] != "done" {
		t.Fatalf("unexpected output: %s", string(out.Output))
	}
}

func TestRunnerExecutorFailsWhenSpawnErrors(t *testing.T) {
	link := newLink(t, "", false, 0)
	ex := NewRunnerExecutor(link, "r1", t.TempDir(), "run-1").WithSpawnTimeout(5*time.Second, 20*time.Second)

	if _, err := ex.RunAgent(context.Background(), agentNode(""), nil); err == nil {
		t.Fatal("a failed spawn must fail the node")
	}
}

// F.5:validator 命令非零退出 → 节点失败;通过 → 输出被判定为 passed。
func TestRunnerExecutorAppliesOutputValidator(t *testing.T) {
	pass := newLink(t, `{"ok":true}`, true, 0)
	exPass := NewRunnerExecutor(pass, "r1", t.TempDir(), "run-1").WithSpawnTimeout(5*time.Second, 20*time.Second)
	out, err := exPass.RunAgent(context.Background(), agentNode("cmd:pytest -q"), nil)
	if err != nil {
		t.Fatalf("validator pass should succeed: %v", err)
	}
	if !strings.Contains(string(out.Output), "passed") && !strings.Contains(string(out.Output), "ok") {
		t.Fatalf("unexpected validated output: %s", string(out.Output))
	}

	fail := newLink(t, `{"ok":true}`, true, 1)
	exFail := NewRunnerExecutor(fail, "r1", t.TempDir(), "run-1").WithSpawnTimeout(5*time.Second, 20*time.Second)
	if _, err := exFail.RunAgent(context.Background(), agentNode("cmd:pytest -q"), nil); err == nil {
		t.Fatal("non-zero validator exit must fail the node")
	}
}

func TestValidatorCmdParsing(t *testing.T) {
	if got := validatorCmd(agentNode("")); got != "" {
		t.Errorf("no validator should yield empty, got %q", got)
	}
	if got := validatorCmd(agentNode("cmd: go test ./... ")); got != "go test ./..." {
		t.Errorf("validatorCmd = %q", got)
	}
	if got := validatorCmd(agentNode("schema:foo.json")); got != "" {
		t.Errorf("non-cmd validator should yield empty, got %q", got)
	}
	if got := validatorCmd(&Node{ID: "x", Type: NodeAgent}); got != "" {
		t.Errorf("agent-less node should yield empty, got %q", got)
	}
}

func TestRunnerExecutorApproveRequiresApprovalExecutor(t *testing.T) {
	link := newLink(t, `{"ok":true}`, true, 0)
	ex := NewRunnerExecutor(link, "r1", t.TempDir(), "run-1")
	ok, err := ex.Approve(context.Background(), agentNode(""))
	if err == nil || ok {
		t.Fatalf("Approve without ApprovalExecutor must error, got (%v,%v)", ok, err)
	}
}

// ── outputSpec.produces validation ──

func withProduces(node *Node, produces ...ProduceSpec) *Node {
	if node.Agent.OutputSpec == nil {
		node.Agent.OutputSpec = &OutputSpec{}
	}
	node.Agent.OutputSpec.Produces = produces
	return node
}

func TestRunnerExecutorRejectsMissingRequiredArtifact(t *testing.T) {
	link := newLink(t, `{"ok":true}`, true, 0)
	ex := NewRunnerExecutor(link, "r1", t.TempDir(), "run-1").WithSpawnTimeout(5*time.Second, 20*time.Second)

	// output.json has no "report" field, but agent claims it produced report.json
	node := withProduces(agentNode(""), ProduceSpec{ID: "report", Path: "report.json", Type: "json", Required: true})
	_, err := ex.RunAgent(context.Background(), node, nil)
	if err == nil {
		t.Fatal("missing required artifact must fail the node")
	}
	if !strings.Contains(err.Error(), "report") {
		t.Fatalf("error should mention artifact id, got: %v", err)
	}
}

func TestRunnerExecutorAcceptsSatisfiedRequiredProduces(t *testing.T) {
	// output.json IS the required artifact — validateProduces should accept it
	link := newLink(t, `{"ok":true,"summary":"done"}`, true, 0)
	ex := NewRunnerExecutor(link, "r1", t.TempDir(), "run-1").WithSpawnTimeout(5*time.Second, 20*time.Second)

	node := withProduces(agentNode(""), ProduceSpec{ID: "main", Path: "output.json", Type: "json", Required: true})
	out, err := ex.RunAgent(context.Background(), node, nil)
	if err != nil {
		t.Fatalf("RunAgent should succeed when required artifact exists: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Output, &got); err != nil {
		t.Fatalf("output not JSON: %s", string(out.Output))
	}
}

func TestRunnerExecutorSkipsNonRequiredProduces(t *testing.T) {
	link := newLink(t, `{"ok":true}`, true, 0)
	ex := NewRunnerExecutor(link, "r1", t.TempDir(), "run-1").WithSpawnTimeout(5*time.Second, 20*time.Second)

	// non-required artifact missing → no error
	node := withProduces(agentNode(""), ProduceSpec{ID: "log", Path: "debug.log", Type: "text", Required: false})
	if _, err := ex.RunAgent(context.Background(), node, nil); err != nil {
		t.Fatalf("non-required artifact should be skipped: %v", err)
	}
}

func TestRunnerExecutorRejectsInvalidJSONTypeArtifact(t *testing.T) {
	link := newLink(t, "not json at all", true, 0)
	ex := NewRunnerExecutor(link, "r1", t.TempDir(), "run-1").WithSpawnTimeout(5*time.Second, 20*time.Second)

	// output.json content is "not json at all" which is invalid JSON
	node := withProduces(agentNode(""), ProduceSpec{ID: "main", Path: "output.json", Type: "json", Required: true})
	_, err := ex.RunAgent(context.Background(), node, nil)
	if err == nil {
		t.Fatal("invalid JSON artifact must fail")
	}
}

func TestNatsRunnerLinkDelegates(t *testing.T) {
	link := newLink(t, "artifact-body", true, 0)
	ctx := context.Background()

	if got := link.RegisteredRunners(); len(got) == 0 {
		t.Fatal("registered runner missing")
	}
	body, err := link.ReadArtifact(ctx, "r1", "s1", "output.json")
	if err != nil || string(body) != "artifact-body" {
		t.Fatalf("ReadArtifact = (%q,%v)", body, err)
	}
	res, err := link.RunCmd(ctx, "r1", "s1", "echo hi", 1000)
	if err != nil || res.ExitCode != 0 || res.Stdout != "out" {
		t.Fatalf("RunCmd = (%+v,%v)", res, err)
	}
	if err := link.Kill(ctx, "r1", "s1"); err != nil {
		t.Fatalf("Kill: %v", err)
	}
}
