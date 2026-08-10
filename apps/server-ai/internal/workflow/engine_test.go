package workflow

import (
	"context"
	"encoding/json"
	"testing"
)

func mustDef(t *testing.T, raw string) *WorkflowDef {
	t.Helper()
	var d WorkflowDef
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("parse def: %v", err)
	}
	return &d
}

func statusOf(evs []Event, id string) Status {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].NodeID == id {
			return evs[i].Status
		}
	}
	return ""
}

func TestLinearAgentChain(t *testing.T) {
	def := mustDef(t, `{
	  "id":"t1","version":"1","name":"linear",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"a","type":"agent","agent":{"id":"a","role":"pm","executor":"mock"}},
	    {"id":"b","type":"agent","agent":{"id":"b","role":"pm","executor":"mock"}}
	  ],
	  "edges":[{"from":"start","to":"a"},{"from":"a","to":"b"}]
	}`)

	e, err := NewEngine(def, &MockExecutor{})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	evs, err := e.Run(context.Background(), json.RawMessage(`{"task":"x"}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, id := range []string{"start", "a", "b"} {
		if got := statusOf(evs, id); got != StatusCompleted {
			t.Errorf("node %s = %s, want completed", id, got)
		}
	}
}

func TestConditionRouting(t *testing.T) {
	def := mustDef(t, `{
	  "id":"t2","version":"1","name":"cond",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"c","type":"condition","condition":{"expr":{"==":[1,1]}}},
	    {"id":"pass","type":"agent","agent":{"id":"p","role":"r","executor":"mock"}},
	    {"id":"fail","type":"agent","agent":{"id":"f","role":"r","executor":"mock"}}
	  ],
	  "edges":[
	    {"from":"start","to":"c"},
	    {"from":"c","to":"pass","branchKey":"true"},
	    {"from":"c","to":"fail","branchKey":"false"}
	  ]
	}`)

	e, err := NewEngine(def, &MockExecutor{})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	evs, err := e.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := statusOf(evs, "pass"); got != StatusCompleted {
		t.Errorf("pass = %s, want completed", got)
	}
	if got := statusOf(evs, "fail"); got != StatusSkipped {
		t.Errorf("fail = %s, want skipped", got)
	}
}

func TestJoinWaitsForFork(t *testing.T) {
	def := mustDef(t, `{
	  "id":"t3","version":"1","name":"forkjoin",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"fork","type":"parallel_fork","fanOut":{"itemsExpr":{"var":"items"},"templateNodeId":"worker"}},
	    {"id":"worker","type":"agent","agent":{"id":"w","role":"r","executor":"mock"}},
	    {"id":"join","type":"join"}
	  ],
	  "edges":[
	    {"from":"start","to":"fork"},
	    {"from":"fork","to":"join","condition":"always"}
	  ]
	}`)

	e, err := NewEngine(def, &MockExecutor{})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	evs, err := e.Run(context.Background(), json.RawMessage(`{"items":["a","b","c"]}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := statusOf(evs, "join"); got != StatusCompleted {
		t.Errorf("join = %s, want completed", got)
	}
	for _, id := range []string{"worker#0", "worker#1", "worker#2"} {
		if got := statusOf(evs, id); got != StatusCompleted {
			t.Errorf("clone %s = %s, want completed", id, got)
		}
	}
}

func TestLoopConverges(t *testing.T) {
	def := mustDef(t, `{
	  "id":"t4","version":"1","name":"loop",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"work","type":"agent","agent":{"id":"w","role":"r","executor":"mock"}},
	    {"id":"loop","type":"loop","loop":{"bodyEntry":"work","condition":{"==":[{"var":"work.ok"},true]},"maxIterations":3}},
	    {"id":"done","type":"agent","agent":{"id":"d","role":"r","executor":"mock"}}
	  ],
	  "edges":[
	    {"from":"start","to":"loop"},
	    {"from":"loop","to":"done"}
	  ]
	}`)

	attempt := 0
	e, err := NewEngine(def, &MockExecutor{
		RunAgentFn: func(_ context.Context, n *Node, _ json.RawMessage) (AgentResult, error) {
			if n.ID == "work" {
				attempt++
				if attempt < 2 {
					return AgentResult{Output: json.RawMessage(`{"ok":false}`)}, nil
				}
				return AgentResult{Output: json.RawMessage(`{"ok":true}`)}, nil
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
	if got := statusOf(evs, "loop"); got != StatusCompleted {
		t.Errorf("loop = %s, want completed", got)
	}
	if got := statusOf(evs, "done"); got != StatusCompleted {
		t.Errorf("done = %s, want completed", got)
	}
	if attempt != 2 {
		t.Errorf("work ran %d times, want 2", attempt)
	}
}

func TestLoopMaxIterationsFails(t *testing.T) {
	def := mustDef(t, `{
	  "id":"t5","version":"1","name":"loopfail",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"work","type":"agent","agent":{"id":"w","role":"r","executor":"mock"}},
	    {"id":"loop","type":"loop","loop":{"bodyEntry":"work","condition":{"==":[{"var":"work.ok"},true]},"maxIterations":2}},
	    {"id":"done","type":"agent","agent":{"id":"d","role":"r","executor":"mock"}}
	  ],
	  "edges":[
	    {"from":"start","to":"loop"},
	    {"from":"loop","to":"done"}
	  ]
	}`)

	e, err := NewEngine(def, &MockExecutor{
		RunAgentFn: func(_ context.Context, n *Node, _ json.RawMessage) (AgentResult, error) {
			return AgentResult{Output: json.RawMessage(`{"ok":false}`)}, nil
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	evs, err := e.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := statusOf(evs, "loop"); got != StatusFailed {
		t.Errorf("loop = %s, want failed", got)
	}
	if got := statusOf(evs, "done"); got != StatusSkipped {
		t.Errorf("done = %s, want skipped (unreachable)", got)
	}
}

func TestHumanApprovalReject(t *testing.T) {
	def := mustDef(t, `{
	  "id":"t6","version":"1","name":"approval",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"gate","type":"human_approval","humanApproval":{"approvers":"any_human","timeoutMs":60000,"onTimeout":"pause","onReject":"pause"}},
	    {"id":"go","type":"agent","agent":{"id":"g","role":"r","executor":"mock"}},
	    {"id":"abort","type":"agent","agent":{"id":"x","role":"r","executor":"mock"}}
	  ],
	  "edges":[
	    {"from":"start","to":"gate"},
	    {"from":"gate","to":"go","condition":"approved"},
	    {"from":"gate","to":"abort","condition":"rejected"}
	  ]
	}`)

	e, err := NewEngine(def, &MockExecutor{})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	e.exec = &stubExecutor{approve: false}
	evs, err := e.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := statusOf(evs, "go"); got != StatusSkipped {
		t.Errorf("go = %s, want skipped", got)
	}
	if got := statusOf(evs, "abort"); got != StatusCompleted {
		t.Errorf("abort = %s, want completed", got)
	}
}

// stubExecutor lets tests drive approval outcomes.
type stubExecutor struct {
	approve bool
	runFn   func(ctx context.Context, node *Node, input json.RawMessage) (AgentResult, error)
}

func (s *stubExecutor) RunAgent(ctx context.Context, node *Node, input json.RawMessage) (AgentResult, error) {
	if s.runFn != nil {
		return s.runFn(ctx, node, input)
	}
	out, _ := json.Marshal(map[string]any{"ok": true})
	return AgentResult{Output: out}, nil
}

func (s *stubExecutor) Approve(ctx context.Context, node *Node) (bool, error) {
	return s.approve, nil
}

var _ Executor = (*stubExecutor)(nil)

func TestEngineOnEventHook(t *testing.T) {
	def := mustDef(t, `{
	  "id":"t1","version":"1","name":"linear",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"a","type":"agent","agent":{"id":"a","role":"pm","executor":"mock"}}
	  ],
	  "edges":[{"from":"start","to":"a"}]
	}`)

	var got []Event
	eng, err := NewEngine(def, &MockExecutor{})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	eng.OnEvent = func(ev Event) { got = append(got, ev) }
	if _, err := eng.Run(context.Background(), nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("OnEvent not called")
	}
	if got[0].Status != StatusRunning {
		t.Errorf("first event = %s, want running", got[0].Status)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Seq <= got[i-1].Seq {
			t.Errorf("events not ordered at %d", i)
		}
	}
}

func TestValidateRejectsCycle(t *testing.T) {
	def := mustDef(t, `{
	  "id":"t7","version":"1","name":"cycle",
	  "nodes":[
	    {"id":"a","type":"agent","agent":{"id":"a","role":"r","executor":"mock"}},
	    {"id":"b","type":"agent","agent":{"id":"b","role":"r","executor":"mock"}}
	  ],
	  "edges":[{"from":"a","to":"b"},{"from":"b","to":"a"}]
	}`)
	if err := def.Validate(); err == nil {
		t.Fatal("expected cycle rejection, got nil")
	}
}
