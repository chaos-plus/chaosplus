// command run-workflow loads a WorkflowDef JSON and executes it end-to-end
// against a real machine runner (NATS → daemon → claude/codex), reading each
// agent node's output back from the workspace. No mock involved.
//
// Usage (env):
//   WORKFLOW_FILE      path to workflow-def JSON (required)
//   WORKFLOW_CONTEXT   path to context_json file, or inline JSON (optional)
//   CONTROL_NATS_URL   nats://... (default nats://127.0.0.1:4222)
//   RUNNER_ID          runner to dispatch to (default first registered)
//   WORKSPACE          cwd agents spawn in (default temp dir)
//   RUN_ID             run identity (default "run-<n>")
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/gateway"
	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/workflow"
)

func main() {
	wfFile := os.Getenv("WORKFLOW_FILE")
	if wfFile == "" {
		log.Fatal("WORKFLOW_FILE is required")
	}

	raw, err := os.ReadFile(wfFile)
	if err != nil {
		log.Fatalf("read workflow: %v", err)
	}
	var def workflow.WorkflowDef
	if err := json.Unmarshal(raw, &def); err != nil {
		log.Fatalf("parse workflow: %v", err)
	}
	if err := def.Validate(); err != nil {
		log.Fatalf("validate workflow: %v", err)
	}

	var contextJSON json.RawMessage
	if p := os.Getenv("WORKFLOW_CONTEXT"); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			contextJSON = b
		} else if json.Valid([]byte(p)) {
			contextJSON = []byte(p)
		} else {
			log.Fatalf("WORKFLOW_CONTEXT is neither a readable file nor inline JSON: %v", err)
		}
	}

	url := envOr("CONTROL_NATS_URL", "nats://127.0.0.1:4222")
	nc, err := nats.Connect(url, nats.Name("chaosplus-run-workflow"), nats.Timeout(5*time.Second))
	if err != nil {
		log.Fatalf("connect NATS %s: %v", url, err)
	}
	defer nc.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	g := gateway.New(nc)
	go func() {
		if err := g.Start(ctx); err != nil {
			log.Printf("gateway stopped: %v", err)
		}
	}()
	time.Sleep(300 * time.Millisecond) // let subscriptions register

	runnerID := os.Getenv("RUNNER_ID")
	if runnerID == "" {
		reg := g.RegisteredRunners()
		if len(reg) == 0 {
			log.Fatal("no runner registered; set RUNNER_ID or start a daemon")
		}
		runnerID = reg[0]
	}

	workspace := os.Getenv("WORKSPACE")
	if workspace == "" {
		workspace, err = os.MkdirTemp("", "chaosplus-run-*")
		if err != nil {
			log.Fatalf("workspace: %v", err)
		}
		log.Printf("workspace: %s", workspace)
	}

	runID := os.Getenv("RUN_ID")
	if runID == "" {
		runID = fmt.Sprintf("run-%d", time.Now().UnixNano())
	}

	log.Printf("running %s (%s) on runner %s, workspace %s", def.Name, def.ID, runnerID, workspace)

	exec := workflow.NewRunnerExecutor(g, runnerID, workspace, runID)
	if d := os.Getenv("SPAWN_IDLE_TIMEOUT"); d != "" {
		if t, err := time.ParseDuration(d); err == nil {
			exec.WithSpawnTimeout(t, 0)
		} else {
			log.Printf("invalid SPAWN_IDLE_TIMEOUT %q: %v", d, err)
		}
	}
	if d := os.Getenv("SPAWN_MAX_TIMEOUT"); d != "" {
		if t, err := time.ParseDuration(d); err == nil {
			exec.WithSpawnTimeout(0, t)
		} else {
			log.Printf("invalid SPAWN_MAX_TIMEOUT %q: %v", d, err)
		}
	}
	eng, err := workflow.NewEngine(&def, exec)
	if err != nil {
		log.Fatalf("new engine: %v", err)
	}

	events, err := eng.Run(ctx, contextJSON)
	if err != nil {
		log.Printf("run error: %v", err)
	}
	for _, ev := range events {
		line := fmt.Sprintf("%3d %-6s %-20s", ev.Seq, ev.Status, ev.NodeID)
		if ev.Status == workflow.StatusCompleted && len(ev.Output) > 0 {
			line += "  => " + string(ev.Output)
		}
		if ev.Error != "" {
			line += "  ! " + ev.Error
		}
		log.Print(line)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fmt.Sprint(def)
}
