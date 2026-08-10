package workflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// loadExample reads the software-dev-agile example from disk and validates it.
func loadExample(t *testing.T) *WorkflowDef {
	t.Helper()
	p := filepath.Join("..", "..", "examples", "software-dev-agile.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	var def WorkflowDef
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatalf("parse example: %v", err)
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("validate example: %v", err)
	}
	return &def
}

// TestSoftwareDevAgileEndToEnd runs the full example through the engine with a
// scripted mock: qa fails once then passes, so the retry loop iterates.
func TestSoftwareDevAgileEndToEnd(t *testing.T) {
	def := loadExample(t)
	attempts := map[string]int{}
	e, err := NewEngine(def, &MockExecutor{
		RunAgentFn: func(_ context.Context, n *Node, _ json.RawMessage) (AgentResult, error) {
			switch n.Agent.Role {
			case "pm":
				if n.ID == "sprint-plan" {
					return AgentResult{Output: json.RawMessage(`{"tasks":["fe","be","mobile"]}`)}, nil
				}
				return AgentResult{Output: json.RawMessage(`{"done":true}`)}, nil
			case "arch":
				return AgentResult{Output: json.RawMessage(`{"done":true}`)}, nil
			case "coder":
				return AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
			case "qa":
				attempts["qa"]++
				if attempts["qa"] < 2 {
					return AgentResult{Output: json.RawMessage(`{"result":"failed"}`)}, nil
				}
				return AgentResult{Output: json.RawMessage(`{"result":"passed"}`)}, nil
			}
			return AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	evs, err := e.Run(context.Background(), json.RawMessage(`{"task":"build a feature"}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// All named nodes must terminate; the example must reach the final gate.
	expected := []string{
		"trigger", "requirements", "prd", "prd-approval",
		"design", "arch-approval", "sprint-plan", "fork", "join", "qa",
		"bug-fix", "qa-loop", "sprint-delivery",
	}
	for _, id := range expected {
		s := statusOf(evs, id)
		if s != StatusCompleted {
			t.Errorf("node %s = %s, want completed", id, s)
		}
	}

	// Fork clones (H-3): one per sprint task.
	for _, id := range []string{"coding#0", "coding#1", "coding#2"} {
		if s := statusOf(evs, id); s != StatusCompleted {
			t.Errorf("clone %s = %s, want completed", id, s)
		}
	}
	// QA failed once → loop ran bug-fix before passing.
	if attempts["qa"] != 2 {
		t.Errorf("qa ran %d times, want 2 (one retry loop iteration)", attempts["qa"])
	}

	// Event ordering: fork must precede its clones; join must precede qa-loop.
	order := make(map[string]int)
	for i, ev := range evs {
		if _, ok := order[ev.NodeID]; !ok {
			order[ev.NodeID] = i
		}
	}
	if order["fork"] > order["coding#0"] {
		t.Errorf("fork emitted after coding#0")
	}
	if order["join"] > order["qa-loop"] {
		t.Errorf("join emitted after qa-loop")
	}
}

// TestExampleSchemaValidation cross-checks the schema file parses (authoring
// surface can depend on it).
func TestExampleSchemaValidation(t *testing.T) {
	p := filepath.Join("..", "..", "schema", "workflow-def.schema.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if s["$schema"] == nil {
		t.Fatal("schema missing $schema")
	}
}
