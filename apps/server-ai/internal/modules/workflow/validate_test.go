package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateApprovalEdges(t *testing.T) {
	base := testApprovalDef() // trigger -> approval -> agent
	if err := base.Validate(); err != nil {
		t.Fatalf("approved edge should validate: %v", err)
	}

	// 非法:approval 出边 = success(默认) → 拒绝后仍会流向 downstream。
	bad := testApprovalDef()
	bad.Edges[1].Condition = EdgeSuccess
	if err := bad.Validate(); err == nil {
		t.Fatal("approval node with success edge should fail validation")
	}

	// 合法:terminal 审批(无出边,sprint-delivery 同款)。
	terminal := testApprovalDef()
	terminal.Edges = terminal.Edges[:1]
	if err := terminal.Validate(); err != nil {
		t.Fatalf("terminal approval node should validate: %v", err)
	}
}

func TestValidateContextSchema(t *testing.T) {
	def := testApprovalDef()
	def.ContextSchema = json.RawMessage(`{
		"type":"object",
		"required":["ticket"],
		"additionalProperties":false,
		"properties":{"ticket":{"type":"string","minLength":1}}
	}`)
	if err := def.Validate(); err != nil {
		t.Fatalf("valid context schema: %v", err)
	}
	if err := def.ValidateContext(json.RawMessage(`{"ticket":"CP-42"}`)); err != nil {
		t.Fatalf("matching context: %v", err)
	}
	if err := def.ValidateContext(json.RawMessage(`{"ticket":42}`)); err == nil || !strings.Contains(err.Error(), "contextSchema") {
		t.Fatalf("mismatching context should fail with contextSchema detail, got %v", err)
	}

	def.ContextSchema = json.RawMessage(`{"type":"not-a-json-schema-type"}`)
	if err := def.Validate(); err == nil {
		t.Fatal("invalid contextSchema must fail definition validation")
	}
}

func TestValidateFailsClosedForUnsupportedAgentConstraints(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ExecutorAgentSpec)
	}{
		{"forbidden actions", func(a *ExecutorAgentSpec) { a.ForbiddenActions = []string{"git push --force"} }},
		{"MCP tools", func(a *ExecutorAgentSpec) { a.AllowedMCPTools = []string{"github.search"} }},
		{"required skills", func(a *ExecutorAgentSpec) { a.RequiredSkills = []string{"security-review"} }},
		{"hooks", func(a *ExecutorAgentSpec) { command := "cmd:true"; a.Hooks = &HooksSpec{Pre: &command} }},
		{"input validator", func(a *ExecutorAgentSpec) {
			validator := "cmd:true"
			a.InputSpec = &InputSpec{InputValidator: &validator}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := testApprovalDef()
			tt.mutate(def.Nodes[2].Agent)
			if err := def.Validate(); err == nil {
				t.Fatal("unsupported constraint must be rejected, never ignored")
			}
		})
	}
}

func TestValidateScriptExecutorContract(t *testing.T) {
	def := testApprovalDef()
	agent := def.Nodes[2].Agent
	agent.Executor = "script"
	if err := def.Validate(); err == nil || !strings.Contains(err.Error(), "requires a non-empty script") {
		t.Fatalf("script executor without source must fail, got %v", err)
	}

	agent.Script = `printf '{"ok":true}' > output.json`
	if err := def.Validate(); err != nil {
		t.Fatalf("script executor with source must validate: %v", err)
	}

	agent.Executor = "claude"
	if err := def.Validate(); err == nil || !strings.Contains(err.Error(), "only valid for the script executor") {
		t.Fatalf("non-script executor carrying script source must fail, got %v", err)
	}
}

func TestValidateRejectsSubworkflowAtSubmission(t *testing.T) {
	def := &WorkflowDef{ID: "parent", Version: "1", Nodes: []Node{{
		ID: "child", Type: NodeSubworkflow, Subworkflow: &SubworkflowSpec{Ref: "workflow:child"},
	}}}
	if err := def.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported subworkflow") {
		t.Fatalf("subworkflow must fail before runtime, got %v", err)
	}
}

// testApprovalDef: trigger -(success)-> approval(human_approval) -(approved)-> agent
func testApprovalDef() *WorkflowDef {
	return &WorkflowDef{
		ID: "t", Version: "1",
		Nodes: []Node{
			{ID: "t0", Type: NodeTrigger, Trigger: &TriggerSpec{Source: "manual"}},
			{ID: "ap", Type: NodeHumanApproval, HumanApproval: &HumanApprovalSpec{Approvers: Approvers{Any: true}, TimeoutMs: 60000, OnTimeout: "pause", OnReject: "pause"}},
			{ID: "a0", Type: NodeAgent, Agent: &ExecutorAgentSpec{Executor: "claude", SystemPrompt: "x"}},
		},
		Edges: []Edge{
			{From: "t0", To: "ap", Condition: EdgeSuccess},
			{From: "ap", To: "a0", Condition: EdgeApproved},
		},
	}
}
