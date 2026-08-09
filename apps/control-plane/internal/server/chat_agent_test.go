package server

import (
	"testing"

	"github.com/chaos-plus/chaosplus/apps/control-plane/internal/workflow"
)

func TestStoreTypeForMapsEventsToPRDTypes(t *testing.T) {
	cases := []struct {
		name string
		ev   RunEvent
		want string
	}{
		{"run failed", RunEvent{Status: workflow.StatusFailed}, "RUN_FAILED"},
		{"run generic", RunEvent{Status: workflow.StatusRunning}, "RUN_EVENT"},
		{"review approved", RunEvent{NodeID: "ap", Review: &ReviewInfo{Approved: true}}, "REVIEW_APPROVED"},
		{"review rejected", RunEvent{NodeID: "ap", Review: &ReviewInfo{Approved: false}}, "REVIEW_REJECTED"},
		{"review requested", RunEvent{NodeID: "ap", Status: workflow.StatusWaitingApproval}, "REVIEW_REQUESTED"},
		{"node completed", RunEvent{NodeID: "n1", Status: workflow.StatusCompleted}, "NODE_completed"},
		{"node retrying", RunEvent{NodeID: "n1", Status: workflow.StatusRetrying}, "NODE_RETRY_SCHEDULED"},
	}
	for _, c := range cases {
		if got := storeTypeFor(c.ev); got != c.want {
			t.Errorf("%s: storeTypeFor = %q, want %q", c.name, got, c.want)
		}
	}
}
