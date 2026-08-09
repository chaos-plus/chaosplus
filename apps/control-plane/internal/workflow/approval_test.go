package workflow

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestApprovalBrokerWaitResolve(t *testing.T) {
	b := NewApprovalBroker()
	done := make(chan Decision, 1)
	go func() { d, _ := b.Wait(context.Background(), "n1"); done <- d }()
	select {
	case <-done:
		t.Fatal("Wait returned before Resolve")
	default:
	}
	if err := b.Resolve("n1", true, "", nil); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if d := <-done; !d.OK {
		t.Error("Wait returned OK=false, want true")
	}
	if d, ok := b.Decision("n1"); !ok || !d.OK {
		t.Errorf("decision = %+v, want ok=true", d)
	}
}

func TestApprovalBrokerResolveTwice(t *testing.T) {
	b := NewApprovalBroker()
	if err := b.Resolve("n1", true, "", nil); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if err := b.Resolve("n1", false, "x", &Feedback{Category: FeedbackOther, Detail: "x"}); err == nil {
		t.Fatal("second resolve should error")
	}
}

func TestApprovalBrokerWaitCancel(t *testing.T) {
	b := NewApprovalBroker()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := b.Wait(ctx, "n1"); done <- err }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Wait should return ctx error")
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not unblock on cancel")
	}
}

func TestApprovalBrokerOnDecision(t *testing.T) {
	b := NewApprovalBroker()
	var got string
	b.OnDecision = func(nodeID string, d Decision) { got = nodeID + ":" + d.Reason }
	if err := b.Resolve("n1", false, "redo", &Feedback{Category: FeedbackFunctional, Detail: "redo"}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "n1:redo" {
		t.Errorf("OnDecision = %q, want %q", got, "n1:redo")
	}
}

func TestApprovalExecutorDelegates(t *testing.T) {
	b := NewApprovalBroker()
	base := &MockExecutor{RunAgentFn: func(_ context.Context, _ *Node, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"agent":"ran"}`), nil
	}}
	ae := NewApprovalExecutor(base, b)

	// RunAgent 委托 base。
	out, err := ae.RunAgent(context.Background(), &Node{ID: "a"}, nil)
	if err != nil || string(out) != `{"agent":"ran"}` {
		t.Fatalf("RunAgent delegate: %s %v", out, err)
	}

	// Approve 阻塞到 Resolve,并带回完整 Decision(含反馈)。
	go func() { _ = b.Resolve("ap", false, "redo", &Feedback{Category: FeedbackFunctional, Detail: "redo"}) }()
	d, err := ae.Approve(context.Background(), &Node{ID: "ap"})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if d.OK {
		t.Error("Approve should return OK=false")
	}
	if d.Feedback == nil || d.Feedback.Detail != "redo" {
		t.Fatalf("Approve must carry the rejection feedback: %+v", d)
	}
}

// PRD §13:拒绝必须带结构化反馈,且 category 只能取四个枚举之一。
func TestRejectionRequiresStructuredFeedback(t *testing.T) {
	b := NewApprovalBroker()
	if err := b.Resolve("n1", false, "no good", nil); err == nil {
		t.Fatal("rejection without feedback must be refused")
	}
	if err := b.Resolve("n1", false, "", &Feedback{Category: FeedbackFunctional}); err == nil {
		t.Fatal("feedback without detail must be refused")
	}
	if err := b.Resolve("n1", false, "", &Feedback{Detail: "缺少校验"}); err == nil {
		t.Fatal("feedback without category must be refused")
	}
	if err := b.Resolve("n1", false, "", &Feedback{Category: "随便写", Detail: "x"}); err == nil {
		t.Fatal("unknown category must be refused")
	}

	fb := &Feedback{Category: FeedbackDeviation, Location: "login.tsx:42", Expected: "跳转到首页", Detail: "点击后停在原页"}
	if err := b.Resolve("n1", false, "打回", fb); err != nil {
		t.Fatalf("valid rejection should succeed: %v", err)
	}
	got, ok := b.Decision("n1")
	if !ok || got.OK || got.Feedback == nil {
		t.Fatalf("decision missing feedback: %+v", got)
	}
	if got.Feedback.Category != FeedbackDeviation || got.Feedback.Location != "login.tsx:42" || got.Feedback.Expected != "跳转到首页" {
		t.Fatalf("feedback fields lost: %+v", got.Feedback)
	}
}

func TestApprovalNeedsNoFeedback(t *testing.T) {
	b := NewApprovalBroker()
	if err := b.Resolve("ok1", true, "看着不错", nil); err != nil {
		t.Fatalf("approval must not require feedback: %v", err)
	}
}
