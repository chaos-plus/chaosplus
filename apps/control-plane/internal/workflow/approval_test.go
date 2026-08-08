package workflow

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestApprovalBrokerWaitResolve(t *testing.T) {
	b := NewApprovalBroker()
	done := make(chan bool, 1)
	go func() { ok, _ := b.Wait(context.Background(), "n1"); done <- ok }()
	select {
	case <-done:
		t.Fatal("Wait returned before Resolve")
	default:
	}
	if err := b.Resolve("n1", true, ""); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if ok := <-done; !ok {
		t.Error("Wait returned false, want true")
	}
	if d, ok := b.Decision("n1"); !ok || !d.OK {
		t.Errorf("decision = %+v, want ok=true", d)
	}
}

func TestApprovalBrokerResolveTwice(t *testing.T) {
	b := NewApprovalBroker()
	if err := b.Resolve("n1", true, ""); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if err := b.Resolve("n1", false, "x"); err == nil {
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
	if err := b.Resolve("n1", false, "redo"); err != nil {
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

	// Approve 阻塞到 Resolve。
	go func() { _ = b.Resolve("ap", false, "redo") }()
	ok, err := ae.Approve(context.Background(), &Node{ID: "ap"})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if ok {
		t.Error("Approve should return false")
	}
}
