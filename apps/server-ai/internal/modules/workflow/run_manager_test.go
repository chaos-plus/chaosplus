package workflow

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func workflowTestContext() context.Context {
	return authn.WithClaims(context.Background(), &authn.Claims{TenantID: 11, EntityID: 21, PrincipalID: 31})
}

const workflowTestTimeout = 10 * time.Second

func cleanupTestRunManager(t *testing.T, manager *RunManager) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), workflowTestTimeout)
		defer cancel()
		if err := manager.Stop(ctx); err != nil {
			t.Errorf("stop run manager: %v", err)
		}
	})
}

func newTestRunManager(t *testing.T) *RunManager {
	t.Helper()
	runner := runnerEnvironmentFor(t)
	repository, _ := newTestRepository(t)
	next := guid.ID(1000)
	manager := NewRunManager(runner.natsConn, &GatewayRunnerLink{G: runner.gateway}, repository, integrationRunnerID.String(), 91, func() (guid.ID, error) {
		next++
		return next, nil
	})
	cleanupTestRunManager(t, manager)
	return manager
}

func TestRunManagerPauseResumeAndCancel(t *testing.T) {
	ctx, stop := context.WithCancel(workflowTestContext())
	defer stop()
	m := newTestRunManager(t)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	script := `if [ -f .resumed ]; then printf '{"ok":true}' > output.json; exit 0; fi
touch .resumed
sleep 10`
	def, _ := json.Marshal(map[string]any{"id": "lifecycle", "version": "1", "nodes": []map[string]any{{
		"id": "agent", "type": "agent", "agent": map[string]any{"id": "agent", "role": "automation", "executor": "script", "script": script},
	}}, "edges": []any{}})
	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: def, Workspace: t.TempDir(), ProjectID: 51})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		for _, event := range run.Events() {
			if event.NodeID == "agent" && event.Status == StatusRunning {
				return true
			}
		}
		return false
	}, workflowTestTimeout)
	if err := m.Pause(run.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		run.mu.Lock()
		done := run.done
		run.mu.Unlock()
		if done == nil {
			return false
		}
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, workflowTestTimeout)
	if err := m.Resume(run.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return run.Status() == RunCompleted }, workflowTestTimeout)

	cancelRun, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: def, Workspace: t.TempDir(), ProjectID: 51})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return cancelRun.Status() == RunRunning }, workflowTestTimeout)
	if err := m.Cancel(cancelRun.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return cancelRun.Status() == RunCancelled }, workflowTestTimeout)
}

// testDefRaw: trigger -> approval(human) -(approved)-> agent. JSON string so
// Approvers round-trips through its UnmarshalJSON ("any_human").
const testDefRaw = `{
  "id":"t","version":"1",
  "nodes":[
    {"id":"t0","type":"trigger","trigger":{"source":"manual"}},
    {"id":"ap","type":"human_approval","humanApproval":{"approvers":"any_human","timeoutMs":60000,"onTimeout":"pause","onReject":"pause"}},
    {"id":"a0","type":"agent","agent":{"id":"a0","role":"automation","executor":"script","script":"printf '{\"ok\":true}' > output.json"}}
  ],
  "edges":[{"from":"t0","to":"ap"},{"from":"ap","to":"a0","condition":"approved"}]
}`

func TestRunManagerLaunchAndApprove(t *testing.T) {
	ctx, stop := context.WithCancel(workflowTestContext())
	defer stop()

	m := newTestRunManager(t)
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: json.RawMessage(testDefRaw), Workspace: t.TempDir(), ProjectID: 51})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	// run 停在审批节点等待。
	waitFor(t, func() bool { return run.Status() == RunWaitingApproval }, workflowTestTimeout)

	// 通过 → 继续到 a0 → 完成。
	if err := m.Approve(run.ID, "ap", true, "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunCompleted }, workflowTestTimeout)

	if len(run.Events()) == 0 {
		t.Fatal("no events recorded")
	}
}

func TestRunManagerPersistsRunBeforeAcquiringLease(t *testing.T) {
	runner := runnerEnvironmentFor(t)
	ctx, stop := context.WithCancel(workflowTestContext())
	defer stop()

	repository, _ := newTestRepository(t)
	next := guid.ID(1000)
	m := NewRunManager(runner.natsConn, &GatewayRunnerLink{G: runner.gateway}, repository, integrationRunnerID.String(), 91, func() (guid.ID, error) {
		next++
		return next, nil
	})
	cleanupTestRunManager(t, m)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	run, err := m.Launch(ctx, LaunchRequest{
		WorkflowJSON: json.RawMessage(`{"id":"persisted","version":"1","nodes":[{"id":"start","type":"trigger","trigger":{"source":"manual"}}],"edges":[]}`),
		Workspace:    t.TempDir(), ProjectID: 51,
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repository.GetRunDef(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != run.ID || stored.ProjectID != 51 || stored.TenantID != run.TenantID || stored.EntityID != run.EntityID {
		t.Fatalf("stored run = %+v", stored)
	}
	switch stored.Status {
	case RunRunning, RunCompleted:
	default:
		t.Fatalf("stored run status = %q", stored.Status)
	}
}

func TestRunManagerRejectPauses(t *testing.T) {
	ctx, stop := context.WithCancel(workflowTestContext())
	defer stop()

	m := newTestRunManager(t)
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: json.RawMessage(testDefRaw), Workspace: t.TempDir(), ProjectID: 51})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunWaitingApproval }, workflowTestTimeout)

	if err := m.Approve(run.ID, "ap", false, "wrong spec", &Feedback{Category: FeedbackDeviation, Detail: "与需求不符"}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunPaused }, workflowTestTimeout)
}

func TestRunManagerRejectRoutingWithRejectedEdge(t *testing.T) {
	ctx, stop := context.WithCancel(workflowTestContext())
	defer stop()

	m := newTestRunManager(t)
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// onReject=retry + rejected 出边:拒绝走 rejected 分支而非暂停。
	defJSON, _ := json.Marshal(map[string]any{
		"id": "t", "version": "1",
		"nodes": []map[string]any{
			{"id": "t0", "type": "trigger", "trigger": map[string]any{"source": "manual"}},
			{"id": "ap", "type": "human_approval", "humanApproval": map[string]any{"approvers": "any_human", "timeoutMs": 60000, "onTimeout": "pause", "onReject": "retry"}},
			{"id": "a0", "type": "agent", "agent": map[string]any{"id": "a0", "role": "automation", "executor": "script", "script": successfulNodeScript}},
			{"id": "alt", "type": "agent", "agent": map[string]any{"id": "alt", "role": "automation", "executor": "script", "script": successfulNodeScript}},
		},
		"edges": []map[string]any{
			{"from": "t0", "to": "ap"},
			{"from": "ap", "to": "a0", "condition": "approved"},
			{"from": "ap", "to": "alt", "condition": "rejected"},
		},
	})

	run, err := m.Launch(ctx, LaunchRequest{WorkflowJSON: defJSON, Workspace: t.TempDir(), ProjectID: 51})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunWaitingApproval }, workflowTestTimeout)
	if err := m.Approve(run.ID, "ap", false, "nope", &Feedback{Category: FeedbackFunctional, Detail: "功能不对"}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	waitFor(t, func() bool { return run.Status() == RunCompleted }, workflowTestTimeout)
}

func waitFor(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
