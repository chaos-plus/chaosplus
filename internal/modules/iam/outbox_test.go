package iam

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chaos-plus/chaosplus/internal/core/extension/spicedbx"
)

type recordingRelationshipWriter struct {
	mu      sync.Mutex
	updates []spicedbx.RelationshipUpdate
	err     error
}

func (w *recordingRelationshipWriter) WriteRelationshipUpdates(_ context.Context, updates []spicedbx.RelationshipUpdate) (spicedbx.ZedToken, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.updates = append(w.updates, updates...)
	if w.err != nil {
		return "", w.err
	}
	return "zed-token", nil
}

func TestOutboxCoalescesChangesWhileProcessing(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	role := createTestRole(t, repo, "t1", "role")
	_, err := repo.GrantPermission(ctx, "t1", role.ID, "store_view")
	require.NoError(t, err)

	claimed, err := repo.ClaimOutbox(ctx, "worker", OutboxConfig{})
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, spicedbx.RelationshipTouch, claimed[0].Operation)
	assert.Equal(t, int64(1), claimed[0].Version)

	_, err = repo.RevokePermission(ctx, "t1", role.ID, "store_view")
	require.NoError(t, err)
	require.NoError(t, repo.CompleteOutbox(ctx, claimed[0], "worker", "old-token"))
	status, _, err := repo.OutboxStatus(ctx, "t1", permissionRelationship("t1", role.ID, "store_view"))
	require.NoError(t, err)
	assert.Equal(t, OutboxPending, status)

	claimed, err = repo.ClaimOutbox(ctx, "worker-2", OutboxConfig{})
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, spicedbx.RelationshipDelete, claimed[0].Operation)
	assert.Equal(t, int64(2), claimed[0].Version)
	require.NoError(t, repo.CompleteOutbox(ctx, claimed[0], "worker-2", "new-token"))
	status, token, err := repo.OutboxStatus(ctx, "t1", claimed[0].Relationship)
	require.NoError(t, err)
	assert.Equal(t, OutboxDelivered, status)
	assert.Equal(t, spicedbx.ZedToken("new-token"), token)
}

func TestOutboxClaimHasSingleOwnerAndRecoversStaleLock(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	role := createTestRole(t, repo, "t1", "role")
	_, err := repo.AddMember(ctx, "t1", role.ID, "u1")
	require.NoError(t, err)

	first, err := repo.ClaimOutbox(ctx, "worker-1", OutboxConfig{})
	require.NoError(t, err)
	require.Len(t, first, 1)
	second, err := repo.ClaimOutbox(ctx, "worker-2", OutboxConfig{})
	require.NoError(t, err)
	assert.Empty(t, second)

	repo.now = func() time.Time { return time.UnixMilli(1_700_000_031_000).UTC() }
	recovered, err := repo.ClaimOutbox(ctx, "worker-2", OutboxConfig{LockTimeout: 30 * time.Second})
	require.NoError(t, err)
	require.Len(t, recovered, 1)
	assert.Equal(t, first[0].ID, recovered[0].ID)
}

func TestOutboxWorkerDeliversAndRetriesToDead(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	role := createTestRole(t, repo, "t1", "role")
	_, err := repo.AddMember(ctx, "t1", role.ID, "u1")
	require.NoError(t, err)

	writer := &recordingRelationshipWriter{}
	worker := NewOutboxWorker(repo, writer, OutboxConfig{}, func() (string, error) { return "worker", nil })
	require.NoError(t, worker.RunOnce(ctx))
	status, token, err := repo.OutboxStatus(ctx, "t1", memberRelationship(role.ID, "u1"))
	require.NoError(t, err)
	assert.Equal(t, OutboxDelivered, status)
	assert.Equal(t, spicedbx.ZedToken("zed-token"), token)
	require.Len(t, writer.updates, 1)

	_, err = repo.RemoveMember(ctx, "t1", role.ID, "u1")
	require.NoError(t, err)
	writer.err = errors.New("spicedb down")
	cfg := OutboxConfig{MaxAttempts: 2, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond}
	worker = NewOutboxWorker(repo, writer, cfg, func() (string, error) { return "retry", nil })
	assert.ErrorContains(t, worker.RunOnce(ctx), "spicedb down")
	repo.now = func() time.Time { return time.UnixMilli(1_700_000_000_002).UTC() }
	assert.ErrorContains(t, worker.RunOnce(ctx), "spicedb down")
	status, _, err = repo.OutboxStatus(ctx, "t1", memberRelationship(role.ID, "u1"))
	require.NoError(t, err)
	assert.Equal(t, OutboxDead, status)

	writer.err = nil
	_, err = repo.AddMember(ctx, "t1", role.ID, "u1")
	require.NoError(t, err)
	require.NoError(t, worker.RunOnce(ctx))
	status, _, err = repo.OutboxStatus(ctx, "t1", memberRelationship(role.ID, "u1"))
	require.NoError(t, err)
	assert.Equal(t, OutboxDelivered, status)
}

func TestOutboxWorkerLifecycleAndDefaults(t *testing.T) {
	repo := newIAMRepository(t)
	writer := &recordingRelationshipWriter{}
	worker := NewOutboxWorker(repo, writer, OutboxConfig{PollInterval: time.Hour}, func() (string, error) { return "worker", nil })
	require.NoError(t, worker.Start(context.Background()))
	require.NoError(t, worker.Start(context.Background()))
	worker.Wake()
	worker.Stop()
	worker.Stop()

	bad := NewOutboxWorker(repo, writer, OutboxConfig{}, func() (string, error) { return "", fmt.Errorf("no id") })
	assert.ErrorContains(t, bad.Start(context.Background()), "no id")
	assert.ErrorContains(t, bad.RunOnce(context.Background()), "no id")

	cfg := (OutboxConfig{}).withDefaults()
	assert.Equal(t, 32, cfg.BatchSize)
	assert.Equal(t, 30*time.Second, cfg.LockTimeout)
	assert.Equal(t, 2*time.Second, backoff(OutboxConfig{BaseBackoff: time.Second, MaxBackoff: 3 * time.Second}, 2))
	assert.Equal(t, 3*time.Second, backoff(OutboxConfig{BaseBackoff: time.Second, MaxBackoff: 3 * time.Second}, 4))
}

func TestOutboxConstructorsRejectMissingDependencies(t *testing.T) {
	repo := newIAMRepository(t)
	writer := &recordingRelationshipWriter{}
	assert.Panics(t, func() { NewOutboxWorker(nil, writer, OutboxConfig{}, func() (string, error) { return "1", nil }) })
	assert.Panics(t, func() { NewOutboxWorker(repo, nil, OutboxConfig{}, func() (string, error) { return "1", nil }) })
	assert.Panics(t, func() { NewOutboxWorker(repo, writer, OutboxConfig{}, nil) })
}

func TestOutboxFailureAfterDesiredStateChangesRequeuesLatestVersion(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	role := createTestRole(t, repo, "t1", "role")
	_, err := repo.AddMember(ctx, "t1", role.ID, "u1")
	require.NoError(t, err)
	claimed, err := repo.ClaimOutbox(ctx, "worker", OutboxConfig{})
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	_, err = repo.RemoveMember(ctx, "t1", role.ID, "u1")
	require.NoError(t, err)
	require.NoError(t, repo.FailOutbox(ctx, claimed[0], "worker", errors.New("old write failed"), OutboxConfig{}))
	status, _, err := repo.OutboxStatus(ctx, "t1", claimed[0].Relationship)
	require.NoError(t, err)
	assert.Equal(t, OutboxPending, status)

	assert.Len(t, truncateWorkerID(strings.Repeat("x", 100)), 60)
}

func TestLateOldOperationRequeuesNewerDeliveredOpposite(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	role := createTestRole(t, repo, "t1", "role")
	_, err := repo.GrantPermission(ctx, "t1", role.ID, "store_view")
	require.NoError(t, err)
	oldClaim, err := repo.ClaimOutbox(ctx, "old-worker", OutboxConfig{})
	require.NoError(t, err)
	require.Len(t, oldClaim, 1)

	_, err = repo.RevokePermission(ctx, "t1", role.ID, "store_view")
	require.NoError(t, err)
	repo.now = func() time.Time { return time.UnixMilli(1_700_000_031_000).UTC() }
	newClaim, err := repo.ClaimOutbox(ctx, "new-worker", OutboxConfig{LockTimeout: 30 * time.Second})
	require.NoError(t, err)
	require.Len(t, newClaim, 1)
	require.NoError(t, repo.CompleteOutbox(ctx, newClaim[0], "new-worker", "delete-token"))

	// The old TOUCH completed remotely after the newer DELETE. Its completion
	// must requeue the latest DELETE instead of leaving the DB falsely delivered.
	require.NoError(t, repo.CompleteOutbox(ctx, oldClaim[0], "old-worker", "late-touch-token"))
	status, _, err := repo.OutboxStatus(ctx, "t1", newClaim[0].Relationship)
	require.NoError(t, err)
	assert.Equal(t, OutboxPending, status)
	compensation, err := repo.ClaimOutbox(ctx, "compensating-worker", OutboxConfig{})
	require.NoError(t, err)
	require.Len(t, compensation, 1)
	assert.Equal(t, spicedbx.RelationshipDelete, compensation[0].Operation)
}

func TestUncertainLateFailureRequeuesNewerDeliveredOpposite(t *testing.T) {
	repo := newIAMRepository(t)
	ctx := context.Background()
	role := createTestRole(t, repo, "t1", "role")
	_, err := repo.AddMember(ctx, "t1", role.ID, "u1")
	require.NoError(t, err)
	oldClaim, err := repo.ClaimOutbox(ctx, "old-worker", OutboxConfig{})
	require.NoError(t, err)

	_, err = repo.RemoveMember(ctx, "t1", role.ID, "u1")
	require.NoError(t, err)
	repo.now = func() time.Time { return time.UnixMilli(1_700_000_031_000).UTC() }
	newClaim, err := repo.ClaimOutbox(ctx, "new-worker", OutboxConfig{})
	require.NoError(t, err)
	require.NoError(t, repo.CompleteOutbox(ctx, newClaim[0], "new-worker", "delete-token"))

	// A timeout can report failure even if the old TOUCH was applied remotely.
	require.NoError(t, repo.FailOutbox(ctx, oldClaim[0], "old-worker", context.DeadlineExceeded, OutboxConfig{}))
	status, _, err := repo.OutboxStatus(ctx, "t1", newClaim[0].Relationship)
	require.NoError(t, err)
	assert.Equal(t, OutboxPending, status)
}

type deadlineWriter struct{}

func (deadlineWriter) WriteRelationshipUpdates(ctx context.Context, _ []spicedbx.RelationshipUpdate) (spicedbx.ZedToken, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

func TestOutboxWorkerBoundsRemoteDelivery(t *testing.T) {
	repo := newIAMRepository(t)
	role := createTestRole(t, repo, "t1", "role")
	_, err := repo.AddMember(context.Background(), "t1", role.ID, "u1")
	require.NoError(t, err)
	worker := NewOutboxWorker(repo, deadlineWriter{}, OutboxConfig{DeliveryTimeout: time.Millisecond, LockTimeout: time.Second}, func() (string, error) { return "deadline", nil })
	started := time.Now()
	assert.ErrorIs(t, worker.RunOnce(context.Background()), context.DeadlineExceeded)
	assert.Less(t, time.Since(started), time.Second)
}
