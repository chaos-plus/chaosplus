// Package workflow implements the static-DAG execution core (PRD §7, F.4/F.5).
//
// The engine is language-agnostic: it only reads WorkflowDef JSON. Node
// semantics are fixed at authoring time and executed statically at runtime —
// nothing about the graph is decided by an LLM during a run (PRD C7).
package workflow

import (
	"encoding/json"
	"fmt"
)

// NodeType is a static node kind (PRD §7.2).
type NodeType string

const (
	NodeAgent         NodeType = "agent"
	NodeHumanApproval NodeType = "human_approval"
	NodeCondition     NodeType = "condition"
	NodeParallelFork  NodeType = "parallel_fork"
	NodeJoin          NodeType = "join"
	NodeTransform     NodeType = "transform"
	NodeTrigger       NodeType = "trigger"
	NodeLoop          NodeType = "loop"
	NodeSubworkflow   NodeType = "subworkflow"
)

// EdgeCondition is a predefined (non-eval) edge selector (PRD §7.3).
type EdgeCondition string

const (
	EdgeSuccess  EdgeCondition = "success"
	EdgeFailed   EdgeCondition = "failed"
	EdgeApproved EdgeCondition = "approved"
	EdgeRejected EdgeCondition = "rejected"
	EdgeAlways   EdgeCondition = "always"
)

// WorkflowDef is the single source of truth (PRD §7.1, F.4).
type WorkflowDef struct {
	ID            string          `json:"id"`
	Version       string          `json:"version"`
	Name          string          `json:"name"`
	ContextSchema json.RawMessage `json:"contextSchema,omitempty"`
	Nodes         []Node          `json:"nodes"`
	Edges         []Edge          `json:"edges"`
}

// Node is one step of a workflow.
type Node struct {
	ID            string             `json:"id"`
	Type          NodeType           `json:"type"`
	Name          string             `json:"name,omitempty"`
	Agent         *ExecutorAgentSpec `json:"agent,omitempty"`
	HumanApproval *HumanApprovalSpec `json:"humanApproval,omitempty"`
	Condition     *ConditionSpec     `json:"condition,omitempty"`
	Transform     *TransformSpec     `json:"transform,omitempty"`
	Trigger       *TriggerSpec       `json:"trigger,omitempty"`
	Loop          *LoopSpec          `json:"loop,omitempty"`
	Subworkflow   *SubworkflowSpec   `json:"subworkflow,omitempty"`
	FanOut        *FanOutSpec        `json:"fanOut,omitempty"`
}

// Edge links two nodes; Condition defaults to "success" when empty.
type Edge struct {
	From      string        `json:"from"`
	To        string        `json:"to"`
	Condition EdgeCondition `json:"condition,omitempty"`
	BranchKey string        `json:"branchKey,omitempty"`
}

// ConditionSpec is a sandboxed JSON Logic expression; variables are limited
// to run context ∪ completed node outputs (PRD F.4).
type ConditionSpec struct {
	Expr json.RawMessage `json:"expr"`
}

// TransformSpec maps a JSON Logic expression to an output logical artifact id.
type TransformSpec struct {
	Expr   json.RawMessage `json:"expr"`
	Output string          `json:"output"`
}

// TriggerSpec is the workflow entry point (PRD §7.2).
type TriggerSpec struct {
	Source       string `json:"source"` // manual|schedule|webhook|connector
	ScheduleCron string `json:"scheduleCron,omitempty"`
}

// LoopSpec re-runs a body subgraph until condition holds (PRD §7.2).
type LoopSpec struct {
	BodyEntry     string          `json:"bodyEntry"`
	Condition     json.RawMessage `json:"condition"` // JSON Logic over body outputs
	MaxIterations int             `json:"maxIterations"`
}

// SubworkflowSpec references another WorkflowDef (reserved, PRD §7.2).
type SubworkflowSpec struct {
	Ref           string            `json:"ref"`
	InputMapping  map[string]string `json:"inputMapping,omitempty"`
	OutputMapping map[string]string `json:"outputMapping,omitempty"`
}

// FanOutSpec is data-driven fan-out for parallel_fork (PRD H-3).
type FanOutSpec struct {
	ItemsExpr      json.RawMessage `json:"itemsExpr"`
	TemplateNodeID string          `json:"templateNodeId"`
}

// ExecutorAgentSpec is a hard-constraint agent contract (PRD §6.1.1).
type ExecutorAgentSpec struct {
	ID               string               `json:"id"`
	Role             string               `json:"role"`
	Executor         string               `json:"executor"`
	TokenProfile     string               `json:"tokenProfile,omitempty"`
	MaxContextTokens int                  `json:"maxContextTokens,omitempty"`
	SystemPrompt     string               `json:"systemPrompt,omitempty"`
	AllowedTools     []string             `json:"allowedTools,omitempty"`
	ForbiddenActions []string             `json:"forbiddenActions,omitempty"`
	AllowedMCPTools  []string             `json:"allowedMCPTools,omitempty"`
	RequiredSkills   []string             `json:"requiredSkills,omitempty"`
	InputSpec        *InputSpec           `json:"inputSpec,omitempty"`
	OutputSpec       *OutputSpec          `json:"outputSpec,omitempty"`
	ArtifactSpecs    map[string]ArtifactSpec `json:"artifactSpecs,omitempty"`
	ValidatorSpecs   []ValidatorSpec      `json:"validatorSpecs,omitempty"`
	HumanApproval    *HumanApprovalSpec   `json:"humanApproval,omitempty"`
	Retry            *RetrySpec           `json:"retry,omitempty"`
	Hooks            *HooksSpec           `json:"hooks,omitempty"`
}

// InputSpec is the in-spec (consumes) of an executor agent.
type InputSpec struct {
	Consumes       []ConsumeSpec `json:"consumes,omitempty"`
	InputValidator *string       `json:"inputValidator,omitempty"`
}

// ConsumeSpec is one consumed artifact.
type ConsumeSpec struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// OutputSpec is the out-spec (produces) of an executor agent.
type OutputSpec struct {
	Produces        []ProduceSpec `json:"produces,omitempty"`
	OutputValidator *string       `json:"outputValidator,omitempty"`
}

// ProduceSpec is one produced artifact (logical id defaulting to path).
type ProduceSpec struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// ArtifactSpec is a domain artifact contract (PRD F.5).
type ArtifactSpec struct {
	Type       string   `json:"type"`
	SchemaRef  string   `json:"schemaRef,omitempty"`
	Validators []string `json:"validators,omitempty"`
}

// ValidatorSpec routes a node's artifacts to validators (PRD F.5).
type ValidatorSpec struct {
	Ref       string `json:"ref"`
	Layer     string `json:"layer"` // automated|ai_assisted|human
	Required  bool   `json:"required"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
}

// HumanApprovalSpec is a human gate (PRD F.5).
type HumanApprovalSpec struct {
	Approvers Approvers `json:"approvers"` // 'any_human' or memberId whitelist
	Channel   string    `json:"channel,omitempty"`
	TimeoutMs int       `json:"timeoutMs"`
	OnTimeout string    `json:"onTimeout"` // pause|auto_reject
	OnReject  string    `json:"onReject"`  // retry|pause
}

// RetrySpec is the retry policy for a node (PRD F.5).
type RetrySpec struct {
	MaxAttempts     int   `json:"maxAttempts"`
	BackoffSeconds  []int `json:"backoffSeconds"`
	NotifyThreshold int   `json:"notifyThreshold"`
}

// HooksSpec are runner-side command hooks (PRD F.5).
type HooksSpec struct {
	Pre     *string `json:"pre,omitempty"`
	Post    *string `json:"post,omitempty"`
	OnError *string `json:"onError,omitempty"`
}

// Approvers is 'any_human' or a memberId whitelist.
type Approvers struct {
	Any    bool
	Member []string
}

// UnmarshalJSON accepts either the string "any_human" or a string array.
func (a *Approvers) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		if s != "any_human" {
			return fmt.Errorf("approvers: unknown value %q", s)
		}
		*a = Approvers{Any: true}
		return nil
	}
	return json.Unmarshal(b, &a.Member)
}
