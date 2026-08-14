package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	runnergateway "github.com/chaos-plus/chaosplus/apps/server-ai/internal/infra/runnergateway"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/infra/runnertransport"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/machine"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secure"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/nats-io/nats.go"
	"github.com/uptrace/bun"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func scriptAgentNode(source, validator string) *Node {
	node := &Node{ID: "a0", Type: NodeAgent, Agent: &ExecutorAgentSpec{
		ID: "a0", Role: "automation", Executor: "script", Script: source,
	}}
	if validator != "" {
		value := validator
		node.Agent.OutputSpec = &OutputSpec{OutputValidator: &value}
	}
	return node
}

func TestRunnerExecutorAppliesOutputValidator(t *testing.T) {
	workspace := t.TempDir()
	executor := runnerEnvironmentFor(t).Executor(t, workspace, "validator")
	pass := scriptAgentNode(successfulNodeScript, "cmd:true")
	output, err := executor.RunAgent(t.Context(), pass, json.RawMessage(`{"task":"validate"}`))
	if err != nil {
		t.Fatalf("passing validator: %v", err)
	}
	if !strings.Contains(string(output.Output), "passed") {
		t.Fatalf("validated output = %s", output.Output)
	}

	fail := scriptAgentNode(successfulNodeScript, "cmd:false")
	if _, err := executor.RunAgent(t.Context(), fail, nil); err == nil {
		t.Fatal("non-zero validator exit must fail the node")
	}
}

func TestRunnerExecutorAppliesValidatorSpecs(t *testing.T) {
	node := scriptAgentNode(successfulNodeScript, "")
	node.Agent.ValidatorSpecs = []ValidatorSpec{{Ref: "cmd:false", Layer: "automated", Required: true, TimeoutMs: 1000}}
	executor := runnerEnvironmentFor(t).Executor(t, t.TempDir(), "validator-spec")
	if _, err := executor.RunAgent(t.Context(), node, nil); err == nil || !strings.Contains(err.Error(), "cmd:false") {
		t.Fatalf("required validator failure = %v", err)
	}

	node.Agent.ValidatorSpecs[0].Required = false
	if _, err := executor.RunAgent(t.Context(), node, nil); err != nil {
		t.Fatalf("optional validator should remain evidence-only: %v", err)
	}
}

func TestValidatorCmdParsing(t *testing.T) {
	if got := validatorCmd(scriptAgentNode(successfulNodeScript, "")); got != "" {
		t.Errorf("no validator should yield empty, got %q", got)
	}
	if got := validatorCmd(scriptAgentNode(successfulNodeScript, "cmd: go test ./... ")); got != "go test ./..." {
		t.Errorf("validatorCmd = %q", got)
	}
	if got := validatorCmd(scriptAgentNode(successfulNodeScript, "schema:foo.json")); got != "" {
		t.Errorf("non-cmd validator should yield empty, got %q", got)
	}
	if got := validatorCmd(&Node{ID: "x", Type: NodeAgent}); got != "" {
		t.Errorf("agent-less node should yield empty, got %q", got)
	}
}

func TestRunnerExecutorApproveRequiresApprovalExecutor(t *testing.T) {
	executor := runnerEnvironmentFor(t).Executor(t, t.TempDir(), "approval-guard")
	decision, err := executor.Approve(context.Background(), scriptAgentNode(successfulNodeScript, ""))
	if err == nil || decision.OK {
		t.Fatalf("Approve without ApprovalExecutor must error, got (%v,%v)", decision.OK, err)
	}
}

func withProduces(node *Node, produces ...ProduceSpec) *Node {
	if node.Agent.OutputSpec == nil {
		node.Agent.OutputSpec = &OutputSpec{}
	}
	node.Agent.OutputSpec.Produces = produces
	return node
}

func TestRunnerExecutorValidatesProducedArtifacts(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		produces  []ProduceSpec
		wantError string
	}{
		{
			name: "missing required", source: successfulNodeScript,
			produces:  []ProduceSpec{{ID: "report", Path: "report.json", Type: "json", Required: true}},
			wantError: "report",
		},
		{
			name: "satisfied required", source: `printf '{"ok":true,"summary":"done"}' > output.json`,
			produces: []ProduceSpec{{ID: "main", Path: "output.json", Type: "json", Required: true}},
		},
		{
			name: "missing optional", source: successfulNodeScript,
			produces: []ProduceSpec{{ID: "log", Path: "debug.log", Type: "text", Required: false}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := runnerEnvironmentFor(t).Executor(t, t.TempDir(), "artifacts-"+tt.name)
			node := withProduces(scriptAgentNode(tt.source, ""), tt.produces...)
			_, err := executor.RunAgent(t.Context(), node, nil)
			if tt.wantError == "" && err != nil {
				t.Fatalf("validate artifacts: %v", err)
			}
			if tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)) {
				t.Fatalf("error = %v, want detail %q", err, tt.wantError)
			}
		})
	}
}

func TestRunnerExecutorRejectsInvalidOutputJSON(t *testing.T) {
	executor := runnerEnvironmentFor(t).Executor(t, t.TempDir(), "invalid-json")
	node := withProduces(
		scriptAgentNode(`printf 'not json at all' > output.json`, ""),
		ProduceSpec{ID: "main", Path: "output.json", Type: "json", Required: true},
	)
	if _, err := executor.RunAgent(t.Context(), node, nil); err == nil {
		t.Fatal("invalid JSON artifact must fail")
	}
}

const integrationRunnerID guid.ID = 91001

var workflowRunner *runnerEnvironment

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

type runnerEnvironment struct {
	contextCancel context.CancelFunc
	database      *bun.DB
	natsConn      *nats.Conn
	hubNATSConn   *nats.Conn
	httpServer    *httptest.Server
	hub           *machine.Hub
	gateway       *runnergateway.Gateway
	process       *exec.Cmd
	processDone   chan error
	processOutput *synchronizedBuffer
}

func TestMain(m *testing.M) {
	environment, err := startRunnerEnvironment()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "start workflow runner environment: %v\n", err)
		os.Exit(1)
	}
	workflowRunner = environment
	code := m.Run()
	if environment != nil {
		environment.Close()
	}
	os.Exit(code)
}

func startRunnerEnvironment() (*runnerEnvironment, error) {
	natsURL := strings.TrimSpace(os.Getenv("TEST_NATS_URL"))
	if natsURL == "" {
		return nil, nil
	}

	natsConn, err := nats.Connect(natsURL, nats.Timeout(5*time.Second), nats.Name("workflow-runner-integration-test"))
	if err != nil {
		return nil, fmt.Errorf("connect real NATS service: %w", err)
	}
	hubNATSConn, err := nats.Connect(natsURL, nats.Timeout(5*time.Second), nats.Name("machine-hub-instance-a-integration-test"))
	if err != nil {
		natsConn.Close()
		return nil, fmt.Errorf("connect machine hub instance A to real NATS service: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	database, err := bunxtest.Memory()
	if err != nil {
		cancel()
		hubNATSConn.Close()
		natsConn.Close()
		return nil, fmt.Errorf("open workflow runner database: %w", err)
	}
	if err := machine.Migrate(ctx, database); err != nil {
		cancel()
		_ = database.Close()
		hubNATSConn.Close()
		natsConn.Close()
		return nil, fmt.Errorf("migrate workflow runner database: %w", err)
	}
	repository := machine.NewRepository(database)
	gatewayTransport := runnertransport.NewNATS(natsConn)
	hubTransport := runnertransport.NewNATS(hubNATSConn)
	gateway := runnergateway.New(gatewayTransport)
	gateway.SetDirectory(repository)
	if err := gateway.Start(ctx); err != nil {
		cancel()
		_ = database.Close()
		hubNATSConn.Close()
		natsConn.Close()
		return nil, fmt.Errorf("start runner gateway: %w", err)
	}

	tokens := machine.NewTokenStore()
	hub := machine.NewHub(hubTransport, tokens, repository, func() (guid.ID, error) {
		return integrationRunnerID, nil
	}, integrationRunnerID+1, secure.SameOriginPolicy())
	claimsContext := authn.WithClaims(ctx, &authn.Claims{TenantID: 201, EntityID: 301, PrincipalID: 401})
	_, rawToken, _, err := hub.IssueTokenFor(claimsContext)
	if err != nil {
		cancel()
		_ = hub.Close()
		_ = database.Close()
		hubNATSConn.Close()
		natsConn.Close()
		return nil, fmt.Errorf("issue workflow runner token: %w", err)
	}
	httpServer := httptest.NewServer(http.HandlerFunc(hub.HandleWS))

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		cancel()
		httpServer.Close()
		_ = hub.Close()
		_ = database.Close()
		hubNATSConn.Close()
		natsConn.Close()
		return nil, fmt.Errorf("resolve repository root")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../../.."))
	runnerRoot := filepath.Join(repositoryRoot, "apps", "runner")
	runnerEntry := filepath.Join(runnerRoot, "src", "serve.ts")
	process := exec.Command("bun", "run", runnerEntry, "--server", httpServer.URL, "--name", "workflow-integration")
	process.Dir = runnerRoot
	process.Env = append(os.Environ(), "RUNNER_TOKEN="+rawToken)
	output := &synchronizedBuffer{}
	process.Stdout = output
	process.Stderr = output
	if err := process.Start(); err != nil {
		cancel()
		httpServer.Close()
		_ = hub.Close()
		_ = database.Close()
		natsConn.Close()
		return nil, fmt.Errorf("start Bun runner: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()

	environment := &runnerEnvironment{
		contextCancel: cancel,
		database:      database,
		natsConn:      natsConn,
		hubNATSConn:   hubNATSConn,
		httpServer:    httpServer,
		hub:           hub,
		gateway:       gateway,
		process:       process,
		processDone:   done,
		processOutput: output,
	}
	deadline := time.NewTimer(10 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		if runners := gateway.RegisteredRunnersContext(claimsContext); len(runners) == 1 && runners[0] == integrationRunnerID.String() {
			return environment, nil
		}
		select {
		case err := <-done:
			environment.closeServices()
			return nil, fmt.Errorf("Bun runner exited before registration: %v\n%s", err, output.String())
		case <-deadline.C:
			environment.Close()
			return nil, fmt.Errorf("Bun runner registration timed out\n%s", output.String())
		case <-ticker.C:
		}
	}
}

func (e *runnerEnvironment) Executor(t *testing.T, workspace, runID string) *RunnerExecutor {
	t.Helper()
	if e == nil || e.gateway == nil {
		t.Fatal("workflow runner environment is unavailable")
	}
	return NewRunnerExecutor(&GatewayRunnerLink{G: e.gateway}, integrationRunnerID.String(), workspace, runID).
		WithSpawnTimeout(5*time.Second, 20*time.Second).
		WithHeartbeatTimeout(45 * time.Second)
}

func runnerEnvironmentFor(t *testing.T) *runnerEnvironment {
	t.Helper()
	if workflowRunner == nil {
		t.Skip("set TEST_NATS_URL to run the real NATS and Bun runner integration tests")
	}
	return workflowRunner
}

const successfulNodeScript = `printf '{"ok":true}' > output.json`

func runnerExecutorForDefinition(t *testing.T, def *WorkflowDef, scripts map[string]string) *RunnerExecutor {
	return runnerExecutorAt(t, def, scripts, t.TempDir())
}

func runnerExecutorAt(t *testing.T, def *WorkflowDef, scripts map[string]string, workspace string) *RunnerExecutor {
	t.Helper()
	for i := range def.Nodes {
		node := &def.Nodes[i]
		if node.Type != NodeAgent || node.Agent == nil {
			continue
		}
		node.Agent.Executor = "script"
		node.Agent.Script = successfulNodeScript
		if script := scripts[node.ID]; script != "" {
			node.Agent.Script = script
		}
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("validate real-runner workflow: %v", err)
	}
	return runnerEnvironmentFor(t).Executor(t, workspace, fmt.Sprintf("test-%d", time.Now().UnixNano()))
}

func newRunnerEngine(t *testing.T, def *WorkflowDef, scripts map[string]string) *Engine {
	t.Helper()
	engine, err := NewEngine(def, runnerExecutorForDefinition(t, def, scripts))
	if err != nil {
		t.Fatalf("new real-runner engine: %v", err)
	}
	return engine
}

func (e *runnerEnvironment) Close() {
	if e.process != nil && e.process.Process != nil {
		_ = e.process.Process.Signal(os.Interrupt)
		select {
		case <-e.processDone:
		case <-time.After(5 * time.Second):
			_ = e.process.Process.Kill()
			<-e.processDone
		}
	}
	e.closeServices()
}

func (e *runnerEnvironment) closeServices() {
	if e.contextCancel != nil {
		e.contextCancel()
	}
	if e.httpServer != nil {
		e.httpServer.Close()
	}
	if e.hub != nil {
		_ = e.hub.Close()
	}
	if e.natsConn != nil {
		e.natsConn.Close()
	}
	if e.hubNATSConn != nil {
		e.hubNATSConn.Close()
	}
	if e.database != nil {
		_ = e.database.Close()
	}
}

func TestScriptExecutorRunsThroughMachineRunner(t *testing.T) {
	runner := runnerEnvironmentFor(t)
	workspace := t.TempDir()
	executor := runner.Executor(t, workspace, "script-contract")
	node := &Node{ID: "write-output", Type: NodeAgent, Agent: &ExecutorAgentSpec{
		ID:       "write-output",
		Role:     "automation",
		Executor: "script",
		Script:   `printf '{"ok":true,"source":"runner"}' > output.json`,
	}}
	result, err := executor.RunAgent(t.Context(), node, json.RawMessage(`{"task":"verify"}`))
	if err != nil {
		t.Fatalf("run script node: %v\nrunner output:\n%s", err, runner.processOutput.String())
	}
	if string(result.Output) != `{"ok":true,"source":"runner"}` {
		t.Fatalf("output = %s", result.Output)
	}
}

func TestGatewayConcurrentSpawnsUseIndependentWaiters(t *testing.T) {
	runner := runnerEnvironmentFor(t)
	var wait sync.WaitGroup
	errs := make(chan error, 2)
	for _, spawnID := range []string{"concurrent-a", "concurrent-b"} {
		spawnID := spawnID
		wait.Add(1)
		go func() {
			defer wait.Done()
			workspace := t.TempDir()
			result, err := runner.gateway.SpawnAndWaitOpts(t.Context(), integrationRunnerID.String(), runnergateway.Spawn{
				SpawnID: spawnID, RunID: "concurrent", NodeID: spawnID, Attempt: 1,
				ExecutorType: "script", Prompt: `printf '{"ok":true}' > output.json`, Cwd: workspace,
			}, runnergateway.WithIdleTimeout(2*time.Second), runnergateway.WithMaxTimeout(5*time.Second))
			if err != nil {
				errs <- err
				return
			}
			if !result.OK {
				errs <- fmt.Errorf("spawn %s failed: %+v", spawnID, result)
			}
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestGatewayReadsArtifactFromRealRunner(t *testing.T) {
	runner := runnerEnvironmentFor(t)
	workspace := t.TempDir()
	spawnID := "artifact-round-trip"
	result, err := runner.gateway.SpawnAndWait(t.Context(), integrationRunnerID.String(), runnergateway.Spawn{
		SpawnID: spawnID, RunID: "artifact", NodeID: "write", Attempt: 1,
		ExecutorType: "script", Prompt: `printf '{"source":"real-runner"}' > output.json`, Cwd: workspace,
	})
	if err != nil || !result.OK {
		t.Fatalf("spawn = (%+v, %v)", result, err)
	}
	content, err := runner.gateway.ReadArtifact(t.Context(), integrationRunnerID.String(), spawnID, "output.json")
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if string(content) != `{"source":"real-runner"}` {
		t.Fatalf("artifact = %s", content)
	}
	command, err := runner.gateway.RunCmd(t.Context(), integrationRunnerID.String(), spawnID, "printf 'validator-output'", 5000)
	if err != nil {
		t.Fatalf("run validator command: %v", err)
	}
	if command.ExitCode != 0 || command.Stdout != "validator-output" {
		t.Fatalf("validator command = %+v", command)
	}
}

func TestGatewaySwitchesProviderOnRealSession(t *testing.T) {
	runner := runnerEnvironmentFor(t)
	spawnID := "switch-provider"
	if err := runner.gateway.Spawn(t.Context(), integrationRunnerID.String(), runnergateway.Spawn{
		SpawnID: spawnID, RunID: "provider", NodeID: "switch", Attempt: 1,
		ExecutorType: "script", Prompt: `sleep 10`, Cwd: t.TempDir(),
	}); err != nil {
		t.Fatalf("spawn long-running session: %v", err)
	}
	if err := runner.gateway.SwitchProvider(t.Context(), integrationRunnerID.String(), spawnID, "bedrock", "test-only-key"); err != nil {
		t.Fatalf("switch provider: %v", err)
	}
}

func TestGatewayOnEventReceivesRealRunnerEvent(t *testing.T) {
	runner := runnerEnvironmentFor(t)
	events := make(chan runnergateway.RunnerEvent, 8)
	runner.gateway.OnEvent(func(event runnergateway.RunnerEvent) { events <- event })
	t.Cleanup(func() { runner.gateway.OnEvent(nil) })
	spawnID := "event-callback"
	result, err := runner.gateway.SpawnAndWait(t.Context(), integrationRunnerID.String(), runnergateway.Spawn{
		SpawnID: spawnID, RunID: "events", NodeID: "event", Attempt: 1,
		ExecutorType: "script", Prompt: successfulNodeScript, Cwd: t.TempDir(),
	})
	if err != nil || !result.OK {
		t.Fatalf("spawn = (%+v, %v)", result, err)
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-events:
			if event.RunnerID == integrationRunnerID.String() && event.Type == "spawn-done" {
				return
			}
		case <-deadline.C:
			t.Fatal("event callback did not receive spawn-done")
		}
	}
}

func TestGatewayTimeoutsAndKillRealProcesses(t *testing.T) {
	runner := runnerEnvironmentFor(t)
	tests := []struct {
		name      string
		idle      time.Duration
		max       time.Duration
		heartbeat time.Duration
		check     func(error) bool
	}{
		{name: "idle", idle: 150 * time.Millisecond, max: 5 * time.Second, check: func(err error) bool { return err != nil && !errors.Is(err, context.DeadlineExceeded) }},
		{name: "maximum", idle: 5 * time.Second, max: 150 * time.Millisecond, check: func(err error) bool { return errors.Is(err, context.DeadlineExceeded) }},
		{name: "heartbeat", idle: 5 * time.Second, max: 5 * time.Second, heartbeat: 150 * time.Millisecond, check: func(err error) bool { return errors.Is(err, runnergateway.ErrRunnerHeartbeatLost) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spawnID := "timeout-" + tt.name
			_, err := runner.gateway.SpawnAndWaitOpts(t.Context(), integrationRunnerID.String(), runnergateway.Spawn{
				SpawnID: spawnID, RunID: "timeouts", NodeID: tt.name, Attempt: 1,
				ExecutorType: "script", Prompt: `sleep 10`, Cwd: t.TempDir(),
			}, runnergateway.WithIdleTimeout(tt.idle), runnergateway.WithMaxTimeout(tt.max), runnergateway.WithHeartbeatTimeout(tt.heartbeat))
			if !tt.check(err) {
				t.Fatalf("unexpected timeout result: %v", err)
			}
			if err := runner.gateway.Kill(t.Context(), integrationRunnerID.String(), spawnID); err != nil {
				t.Fatalf("kill timed-out process: %v", err)
			}
		})
	}
}

func TestGatewayUnknownRunnerFails(t *testing.T) {
	runner := runnerEnvironmentFor(t)
	err := runner.gateway.Spawn(t.Context(), "999999", runnergateway.Spawn{
		SpawnID: "unknown", RunID: "unknown", NodeID: "unknown", Attempt: 1,
		ExecutorType: "script", Prompt: successfulNodeScript, Cwd: t.TempDir(),
	})
	if err == nil {
		t.Fatal("unknown runner must fail")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	if _, err := runner.gateway.RunCmd(ctx, "999999", "unknown", "true", 500); err == nil {
		t.Fatal("unknown runner command must fail")
	}
}
