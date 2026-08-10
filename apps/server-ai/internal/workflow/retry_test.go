package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// PRD §13 / F.3 / F.5: an agent node with retry.maxAttempts re-runs after a
// failure (with backoff), and only stays failed once attempts are exhausted.
func TestEngineRetriesAgentOnFailure(t *testing.T) {
	def := mustDef(t, `{
	  "id":"rt","version":"1","name":"retry",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"a","type":"agent","agent":{"id":"a","role":"dev","executor":"mock","retry":{"maxAttempts":3,"backoffSeconds":[0,0],"notifyThreshold":2}}},
	    {"id":"done","type":"agent","agent":{"id":"d","role":"r","executor":"mock"}}
	  ],
	  "edges":[{"from":"start","to":"a"},{"from":"a","to":"done"}]
	}`)

	calls := 0
	e, err := NewEngine(def, &MockExecutor{
		RunAgentFn: func(_ context.Context, n *Node, input json.RawMessage) (AgentResult, error) {
			if n.ID == "a" {
				calls++
				if calls < 3 {
					return AgentResult{}, errors.New("compile error")
				}
			}
			return AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	evs, err := e.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := statusOf(evs, "a"); got != StatusCompleted {
		t.Fatalf("node a = %s, want completed after retries", got)
	}
	if calls != 3 {
		t.Fatalf("node a ran %d times, want 3", calls)
	}
	if got := statusOf(evs, "done"); got != StatusCompleted {
		t.Fatalf("done = %s, want completed", got)
	}
	// Transient attempts surface as retrying (F.2 NODE_RETRY_SCHEDULED), and a
	// retried node that eventually succeeds never emits a terminal failure.
	if retrying := countStatus(evs, "a", StatusRetrying); retrying != 2 {
		t.Fatalf("node a retried %d times, want 2", retrying)
	}
	if failed := countStatus(evs, "a", StatusFailed); failed != 0 {
		t.Fatalf("node a emitted %d terminal failures, want 0 (it succeeded)", failed)
	}
}

func TestEngineRetryExhaustsToFailed(t *testing.T) {
	def := mustDef(t, `{
	  "id":"rt2","version":"1","name":"retryfail",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"a","type":"agent","agent":{"id":"a","role":"dev","executor":"mock","retry":{"maxAttempts":2,"backoffSeconds":[0]}}},
	    {"id":"done","type":"agent","agent":{"id":"d","role":"r","executor":"mock"}}
	  ],
	  "edges":[{"from":"start","to":"a"},{"from":"a","to":"done"}]
	}`)

	calls := 0
	e, err := NewEngine(def, &MockExecutor{
		RunAgentFn: func(_ context.Context, n *Node, input json.RawMessage) (AgentResult, error) {
			if n.ID == "a" {
				calls++
			}
			return AgentResult{}, errors.New("always fails")
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	evs, err := e.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := statusOf(evs, "a"); got != StatusFailed {
		t.Fatalf("node a = %s, want failed after exhausting retries", got)
	}
	if calls != 2 {
		t.Fatalf("node a ran %d times, want maxAttempts=2", calls)
	}
	if got := statusOf(evs, "done"); got != StatusSkipped {
		t.Fatalf("done = %s, want skipped (failed upstream)", got)
	}
}

// PRD §13: each retry attempt carries the previous failure so the agent can
// act on it, not start blind.
func TestEngineRetryInjectsLastError(t *testing.T) {
	def := mustDef(t, `{
	  "id":"rt3","version":"1","name":"retryerr",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"a","type":"agent","agent":{"id":"a","role":"dev","executor":"mock","retry":{"maxAttempts":2,"backoffSeconds":[0]}}}
	  ],
	  "edges":[{"from":"start","to":"a"}]
	}`)

	calls := 0
	var secondInput json.RawMessage
	e, err := NewEngine(def, &MockExecutor{
		RunAgentFn: func(_ context.Context, n *Node, input json.RawMessage) (AgentResult, error) {
			if n.ID != "a" {
				return AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
			}
			calls++
			if calls == 1 {
				return AgentResult{}, errors.New("validate failed: schema mismatch")
			}
			secondInput = append(json.RawMessage(nil), input...)
			return AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if _, err := e.Run(context.Background(), nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(secondInput) == 0 {
		t.Fatal("second attempt never ran")
	}
	var scope map[string]json.RawMessage
	if err := json.Unmarshal(secondInput, &scope); err != nil {
		t.Fatalf("input is not JSON: %v", err)
	}
	if got := string(scope["last_error"]); !strings.Contains(got, "schema mismatch") {
		t.Fatalf("second attempt must carry last_error (got %q)", got)
	}
}

// PRD F.5: the configured backoff is actually waited between attempts.
func TestEngineRetryWaitsBackoff(t *testing.T) {
	def := mustDef(t, `{
	  "id":"rt4","version":"1","name":"retrybackoff",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"a","type":"agent","agent":{"id":"a","role":"dev","executor":"mock","retry":{"maxAttempts":2,"backoffSeconds":[1]}}}
	  ],
	  "edges":[{"from":"start","to":"a"}]
	}`)

	calls := 0
	start := time.Now()
	e, err := NewEngine(def, &MockExecutor{
		RunAgentFn: func(_ context.Context, n *Node, input json.RawMessage) (AgentResult, error) {
			if n.ID == "a" {
				calls++
				if calls == 1 {
					return AgentResult{}, errors.New("flaky")
				}
			}
			return AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	evs, err := e.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := statusOf(evs, "a"); got != StatusCompleted {
		t.Fatalf("node a = %s, want completed", got)
	}
	if calls != 2 {
		t.Fatalf("node a ran %d times, want 2", calls)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("backoff not waited (elapsed %v)", elapsed)
	}
}

// PRD §13: a run cancelled during backoff leaves the node failed instead of
// scheduling an unbounded retry.
func TestEngineRetryBackoffCancelLeavesFailed(t *testing.T) {
	def := mustDef(t, `{
	  "id":"rt5","version":"1","name":"retrycancel",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"a","type":"agent","agent":{"id":"a","role":"dev","executor":"mock","retry":{"maxAttempts":3,"backoffSeconds":[30]}}}
	  ],
	  "edges":[{"from":"start","to":"a"}]
	}`)

	e, err := NewEngine(def, &MockExecutor{
		RunAgentFn: func(_ context.Context, n *Node, input json.RawMessage) (AgentResult, error) {
			return AgentResult{}, errors.New("always fails")
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the run → first backoff aborts the retry
	evs, err := e.Run(ctx, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := statusOf(evs, "a"); got != StatusFailed {
		t.Fatalf("node a = %s, want failed when cancelled during backoff", got)
	}
}

// M3 (round-1 review): a fixer that fails its first attempt and retries must
// carry BOTH the rejection feedback and the previous failure on the second run.
func TestEngineFeedbackThenRetryCarriesBoth(t *testing.T) {
	def := mustDef(t, `{
	  "id":"fb3","version":"1","name":"fbretry",
	  "nodes":[
	    {"id":"a","type":"agent","agent":{"id":"a","role":"pm","executor":"mock"}},
	    {"id":"ap","type":"human_approval","humanApproval":{"approvers":"any_human","timeoutMs":60000,"onTimeout":"pause","onReject":"retry"}},
	    {"id":"fix","type":"agent","agent":{"id":"fix","role":"coder","executor":"mock","retry":{"maxAttempts":2,"backoffSeconds":[0]}}}
	  ],
	  "edges":[
	    {"from":"a","to":"ap"},
	    {"from":"ap","to":"fix","condition":"rejected"}
	  ]
	}`)

	calls := 0
	var secondInput json.RawMessage
	e, err := NewEngine(def, &stubExecutor{
		decision: Decision{OK: false, Feedback: &Feedback{Category: FeedbackFunctional, Detail: "缺空值校验"}},
		runFn: func(_ context.Context, n *Node, input json.RawMessage) (AgentResult, error) {
			if n.ID != "fix" {
				return AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
			}
			calls++
			if calls == 1 {
				return AgentResult{}, errors.New("validator failed")
			}
			secondInput = append(json.RawMessage(nil), input...)
			return AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	evs, err := e.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := statusOf(evs, "fix"); got != StatusCompleted {
		t.Fatalf("fix = %s, want completed", got)
	}
	if calls != 2 {
		t.Fatalf("fix ran %d times, want 2", calls)
	}
	var scope map[string]json.RawMessage
	if err := json.Unmarshal(secondInput, &scope); err != nil {
		t.Fatalf("second input is not JSON: %v", err)
	}
	if fb := string(scope["rejection_feedback"]); !strings.Contains(fb, "缺空值校验") {
		t.Fatalf("second attempt must carry rejection_feedback (got %q)", fb)
	}
	if le := string(scope["last_error"]); !strings.Contains(le, "validator failed") {
		t.Fatalf("second attempt must carry last_error (got %q)", le)
	}
}

// buildPrompt surfaces the previous attempt's failure (F.8 layer 4).
func TestBuildPromptSurfacesLastError(t *testing.T) {
	ex := NewRunnerExecutor(newLink(t, `{"ok":true}`, true, 0), "r1", t.TempDir(), "run-1")
	input, _ := json.Marshal(map[string]any{"last_error": "validator failed: exit 1"})
	p := ex.buildPrompt(agentNode(""), input)
	if !strings.Contains(p, "FAILED") || !strings.Contains(p, "exit 1") {
		t.Fatalf("prompt must surface last_error:\n%s", p)
	}
}

func countStatus(evs []Event, id string, s Status) int {
	n := 0
	for _, ev := range evs {
		if ev.NodeID == id && ev.Status == s {
			n++
		}
	}
	return n
}
