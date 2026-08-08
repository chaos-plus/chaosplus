package workflow

import "testing"

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

// testApprovalDef: trigger -(success)-> approval(human_approval) -(approved)-> agent
func testApprovalDef() *WorkflowDef {
	return &WorkflowDef{
		ID: "t", Version: "1",
		Nodes: []Node{
			{ID: "t0", Type: NodeTrigger, Trigger: &TriggerSpec{Source: "manual"}},
			{ID: "ap", Type: NodeHumanApproval, HumanApproval: &HumanApprovalSpec{Approvers: Approvers{Any: true}, TimeoutMs: 60000, OnTimeout: "pause", OnReject: "pause"}},
			{ID: "a0", Type: NodeAgent, Agent: &ExecutorAgentSpec{Executor: "mock", SystemPrompt: "x"}},
		},
		Edges: []Edge{
			{From: "t0", To: "ap", Condition: EdgeSuccess},
			{From: "ap", To: "a0", Condition: EdgeApproved},
		},
	}
}
