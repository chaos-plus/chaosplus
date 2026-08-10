package workflow

import (
	"context"
	"encoding/json"
	"testing"
)

func TestTransformAndApprovalList(t *testing.T) {
	def := mustDef(t, `{
	  "id":"t8","version":"1","name":"transform",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"x","type":"transform","transform":{"expr":{"+":[1,2]},"output":"sum"}},
	    {"id":"gate","type":"human_approval","humanApproval":{"approvers":["u1","u2"],"timeoutMs":0,"onTimeout":"auto_reject","onReject":"pause"}},
	    {"id":"done","type":"agent","agent":{"id":"d","role":"r","executor":"mock"}}
	  ],
	  "edges":[
	    {"from":"start","to":"x"},
	    {"from":"x","to":"gate"},
	    {"from":"gate","to":"done","condition":"approved"}
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
	if got := statusOf(evs, "x"); got != StatusCompleted {
		t.Errorf("transform = %s, want completed", got)
	}
	if got := statusOf(evs, "gate"); got != StatusCompleted {
		t.Errorf("gate = %s, want completed", got)
	}
	if got := statusOf(evs, "done"); got != StatusCompleted {
		t.Errorf("done = %s, want completed", got)
	}
	var out json.RawMessage
	for _, ev := range evs {
		if ev.NodeID == "x" && ev.Status == StatusCompleted {
			out = ev.Output
		}
	}
	if string(out) != "3" {
		t.Errorf("transform output = %s, want 3", string(out))
	}
}

func TestConditionNoMatchFails(t *testing.T) {
	def := mustDef(t, `{
	  "id":"t9","version":"1","name":"condfail",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"c","type":"condition","condition":{"expr":{"var":"k"}}},
	    {"id":"a","type":"agent","agent":{"id":"a","role":"r","executor":"mock"}}
	  ],
	  "edges":[
	    {"from":"start","to":"c"},
	    {"from":"c","to":"a","branchKey":"yes"}
	  ]
	}`)

	e, err := NewEngine(def, &MockExecutor{})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	evs, err := e.Run(context.Background(), json.RawMessage(`{"k":"no"}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := statusOf(evs, "c"); got != StatusFailed {
		t.Errorf("condition = %s, want failed (no matching branch)", got)
	}
	if got := statusOf(evs, "a"); got != StatusSkipped {
		t.Errorf("downstream = %s, want skipped", got)
	}
}

func TestAlwaysFallbackEdge(t *testing.T) {
	def := mustDef(t, `{
	  "id":"t10","version":"1","name":"alwaysfb",
	  "nodes":[
	    {"id":"start","type":"trigger","trigger":{"source":"manual"}},
	    {"id":"c","type":"condition","condition":{"expr":{"var":"k"}}},
	    {"id":"def","type":"agent","agent":{"id":"d","role":"r","executor":"mock"}}
	  ],
	  "edges":[
	    {"from":"start","to":"c"},
	    {"from":"c","to":"def","condition":"always"}
	  ]
	}`)

	e, err := NewEngine(def, &MockExecutor{})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	evs, err := e.Run(context.Background(), json.RawMessage(`{"k":"anything"}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := statusOf(evs, "c"); got != StatusCompleted {
		t.Errorf("condition = %s, want completed via always fallback", got)
	}
	if got := statusOf(evs, "def"); got != StatusCompleted {
		t.Errorf("default = %s, want completed", got)
	}
}

func TestValidateNodeFieldGaps(t *testing.T) {
	cases := []struct {
		name string
		def  string
	}{
		{"agent missing spec", `{"id":"w","version":"1","nodes":[{"id":"a","type":"agent"}],"edges":[]}`},
		{"condition missing expr", `{"id":"w","version":"1","nodes":[{"id":"c","type":"condition"}],"edges":[]}`},
		{"transform missing expr", `{"id":"w","version":"1","nodes":[{"id":"x","type":"transform"}],"edges":[]}`},
		{"fork missing fanout", `{"id":"w","version":"1","nodes":[{"id":"f","type":"parallel_fork"}],"edges":[]}`},
		{"loop missing spec", `{"id":"w","version":"1","nodes":[{"id":"l","type":"loop"}],"edges":[]}`},
		{"unknown type", `{"id":"w","version":"1","nodes":[{"id":"z","type":"nope"}],"edges":[]}`},
		{"bad edge condition", `{"id":"w","version":"1","nodes":[{"id":"a","type":"agent","agent":{"id":"a","role":"r","executor":"m"}},{"id":"b","type":"agent","agent":{"id":"b","role":"r","executor":"m"}}],"edges":[{"from":"a","to":"b","condition":"bogus"}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var d WorkflowDef
			if err := json.Unmarshal([]byte(c.def), &d); err != nil {
				t.Fatalf("parse: %v", err)
			}
			if err := d.Validate(); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}
