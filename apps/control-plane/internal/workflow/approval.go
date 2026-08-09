package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// FeedbackCategory enumerates the structured-feedback categories a rejection
// must carry (PRD §13 / D.5).
type FeedbackCategory string

const (
	FeedbackFunctional FeedbackCategory = "功能缺陷"
	FeedbackStyle      FeedbackCategory = "样式"
	FeedbackDeviation  FeedbackCategory = "需求偏差"
	FeedbackOther      FeedbackCategory = "其他"
)

// Feedback is the structured rejection payload (PRD §13). Category and Detail
// are mandatory so the next attempt gets actionable context, not just "no".
type Feedback struct {
	Category FeedbackCategory `json:"category"`
	Location string           `json:"location,omitempty"`
	Expected string           `json:"expected,omitempty"`
	Detail   string           `json:"detail"`
}

// Validate enforces the required fields and the category enum.
func (f *Feedback) Validate() error {
	switch f.Category {
	case FeedbackFunctional, FeedbackStyle, FeedbackDeviation, FeedbackOther:
	case "":
		return fmt.Errorf("feedback.category is required (功能缺陷|样式|需求偏差|其他)")
	default:
		return fmt.Errorf("feedback.category %q is not one of 功能缺陷|样式|需求偏差|其他", f.Category)
	}
	if f.Detail == "" {
		return fmt.Errorf("feedback.detail is required")
	}
	return nil
}

// Decision is one human_approval resolution.
type Decision struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
	// Feedback is required on rejection (PRD §13); nil on approval.
	Feedback *Feedback `json:"feedback,omitempty"`
}

// ApprovalBroker holds pending human-approval gates for one run. Wait blocks
// until Resolve (via HTTP) or ctx cancellation; Resolve is idempotent.
type ApprovalBroker struct {
	mu      sync.Mutex
	pending map[string]chan Decision
	decided map[string]Decision
	// OnDecision fires after a successful Resolve (nil-safe). Used by RunManager
	// to emit REVIEW_APPROVED / REVIEW_REJECTED events live.
	OnDecision func(nodeID string, d Decision)
}

func NewApprovalBroker() *ApprovalBroker {
	return &ApprovalBroker{pending: make(map[string]chan Decision), decided: make(map[string]Decision)}
}

// Wait blocks until the node's gate is resolved. Only the first caller per
// nodeID waits; a second Wait for the same nodeID errors.
func (b *ApprovalBroker) Wait(ctx context.Context, nodeID string) (bool, error) {
	ch := make(chan Decision, 1)
	b.mu.Lock()
	if d, done := b.decided[nodeID]; done {
		b.mu.Unlock()
		return d.OK, nil
	}
	if _, exists := b.pending[nodeID]; exists {
		b.mu.Unlock()
		return false, fmt.Errorf("approval %q: already waiting", nodeID)
	}
	b.pending[nodeID] = ch
	b.mu.Unlock()

	select {
	case d := <-ch:
		return d.OK, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// Resolve delivers a decision to a waiting Wait. Idempotent: a second call for
// the same nodeID errors. When the decision lands it is recorded and OnDecision
// fires.
// Resolve records a human decision. A rejection MUST carry structured feedback
// (PRD §13) — a bare "no" gives the next attempt nothing to act on.
func (b *ApprovalBroker) Resolve(nodeID string, ok bool, reason string, fb *Feedback) error {
	if !ok {
		if fb == nil {
			return fmt.Errorf("approval %q: rejection requires structured feedback (category, detail)", nodeID)
		}
		if err := fb.Validate(); err != nil {
			return fmt.Errorf("approval %q: %w", nodeID, err)
		}
	}
	d := Decision{OK: ok, Reason: reason, Feedback: fb}
	b.mu.Lock()
	if _, done := b.decided[nodeID]; done {
		b.mu.Unlock()
		return fmt.Errorf("approval %q: already resolved", nodeID)
	}
	b.decided[nodeID] = d
	ch := b.pending[nodeID]
	onDec := b.OnDecision
	b.mu.Unlock()

	if ch != nil {
		ch <- d // buffered(1): never blocks; Wait may have been cancelled
	}
	if onDec != nil {
		onDec(nodeID, d)
	}
	return nil
}

// Decision returns the recorded decision for a nodeID (ok=false if unresolved).
func (b *ApprovalBroker) Decision(nodeID string) (Decision, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.decided[nodeID]
	return d, ok
}

// ApprovalExecutor makes human_approval gates block on the broker while agent
// nodes run on the wrapped base executor. The engine's execApproval already
// calls Approve — only the executor changes.
type ApprovalExecutor struct {
	base   Executor
	broker *ApprovalBroker
}

func NewApprovalExecutor(base Executor, broker *ApprovalBroker) *ApprovalExecutor {
	return &ApprovalExecutor{base: base, broker: broker}
}

func (a *ApprovalExecutor) RunAgent(ctx context.Context, node *Node, input json.RawMessage) (json.RawMessage, error) {
	return a.base.RunAgent(ctx, node, input)
}

func (a *ApprovalExecutor) Approve(ctx context.Context, node *Node) (bool, error) {
	return a.broker.Wait(ctx, node.ID)
}

var _ Executor = (*ApprovalExecutor)(nil)
