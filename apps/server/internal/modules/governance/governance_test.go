package governance

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

var governanceNow = time.Now().UTC().Add(-time.Minute).Truncate(time.Second)

func TestAccessRequestApprovalAndRevocation(t *testing.T) {
	fixture := newGovernanceFixture(t)

	roles, err := fixture.service.RequestableRoles(t.Context(), testID("tenant-a"))
	require.NoError(t, err)
	require.Len(t, roles, 1)
	assert.Equal(t, "Operator", roles[0].Name)

	request, err := fixture.service.Create(t.Context(), testID("tenant-a"), testID("requester"), CreateAccessRequest{
		RoleID: testID("role-a"), Reason: " Need temporary store access ", AccessExpiresAt: governanceNow.Add(24 * time.Hour),
	})
	require.NoError(t, err)
	assert.Equal(t, StatusPending, request.Status)
	assert.Equal(t, "Operator", request.RoleName)
	assert.Equal(t, "Need temporary store access", request.Reason)

	_, err = fixture.service.Approve(t.Context(), testID("tenant-a"), request.ID, testID("requester"), "self")
	assert.ErrorIs(t, err, ErrSelfApproval)
	_, err = fixture.service.Approve(t.Context(), testID("tenant-b"), request.ID, testID("approver"), "wrong tenant")
	assert.ErrorIs(t, err, ErrNotFound)

	_, err = fixture.db.ExecContext(t.Context(), "UPDATE iam_roles SET name = 'Renamed' WHERE tenant_id = 'tenant-a' AND id = 'role-a'")
	require.NoError(t, err)
	listed, err := fixture.service.List(t.Context(), testID("tenant-a"), testID("requester"))
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, "Operator", listed[0].RoleName, "request snapshots must remain immutable")

	approved, err := fixture.service.Approve(t.Context(), testID("tenant-a"), request.ID, testID("approver"), "approved for incident")
	require.NoError(t, err)
	assert.Equal(t, StatusApproved, approved.Status)
	assert.Equal(t, testID("approver"), approved.DecidedBy)
	allowed, err := iam.NewAuthorizer(fixture.db).Check(t.Context(), testID("tenant-a"), "store_view", testID("requester"))
	require.NoError(t, err)
	assert.True(t, allowed)

	revoked, err := fixture.service.Revoke(t.Context(), testID("tenant-a"), request.ID, testID("approver"), "incident closed")
	require.NoError(t, err)
	assert.Equal(t, StatusRevoked, revoked.Status)
	allowed, err = iam.NewAuthorizer(fixture.db).Check(t.Context(), testID("tenant-a"), "store_view", testID("requester"))
	require.NoError(t, err)
	assert.False(t, allowed)
	revision, err := policyx.Current(t.Context(), fixture.db, testID("tenant-a"))
	require.NoError(t, err)
	assert.Equal(t, int64(2), revision)

	integrity, err := auditmod.NewService(fixture.db, newTestIDGenerator()).Verify(t.Context(), testID("tenant-a"))
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
	assert.Equal(t, int64(3), integrity.VerifiedEvents)
}

func TestAccessRequestRejectWithdrawExpiryAndValidation(t *testing.T) {
	fixture := newGovernanceFixture(t)

	for _, input := range []CreateAccessRequest{
		{},
		{RoleID: testID("role-a"), Reason: "no", AccessExpiresAt: governanceNow.Add(time.Hour)},
		{RoleID: testID("role-a"), Reason: "valid reason", AccessExpiresAt: governanceNow},
		{RoleID: testID("role-a"), Reason: "valid reason", AccessExpiresAt: governanceNow.Add(maxAccessDuration + time.Second)},
	} {
		_, err := fixture.service.Create(t.Context(), testID("tenant-a"), testID("requester"), input)
		assert.ErrorIs(t, err, ErrInvalid)
	}
	_, err := fixture.service.Create(t.Context(), testID("tenant-a"), testID("disabled"), validGovernanceRequest())
	assert.ErrorIs(t, err, ErrRequesterInactive)
	missing := validGovernanceRequest()
	missing.RoleID = testID("missing")
	_, err = fixture.service.Create(t.Context(), testID("tenant-a"), testID("requester"), missing)
	assert.ErrorIs(t, err, ErrRoleNotFound)

	rejected := mustCreateAccessRequest(t, fixture)
	result, err := fixture.service.Reject(t.Context(), testID("tenant-a"), rejected.ID, testID("approver"), "not required")
	require.NoError(t, err)
	assert.Equal(t, StatusRejected, result.Status)
	_, err = fixture.service.Reject(t.Context(), testID("tenant-a"), rejected.ID, testID("approver"), "again")
	assert.ErrorIs(t, err, ErrStateConflict)

	withdrawn := mustCreateAccessRequest(t, fixture)
	_, err = fixture.service.Revoke(t.Context(), testID("tenant-a"), withdrawn.ID, testID("approver"), "not approved")
	assert.ErrorIs(t, err, ErrStateConflict)
	_, err = fixture.service.Withdraw(t.Context(), testID("tenant-a"), withdrawn.ID, testID("other"), "not mine")
	assert.ErrorIs(t, err, ErrRequesterOnly)
	result, err = fixture.service.Withdraw(t.Context(), testID("tenant-a"), withdrawn.ID, testID("requester"), "no longer needed")
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, result.Status)

	expired := mustCreateAccessRequest(t, fixture)
	fixture.service.now = func() time.Time { return governanceNow.Add(requestTTL + time.Second) }
	items, err := fixture.service.List(t.Context(), testID("tenant-a"), 0)
	require.NoError(t, err)
	assert.Contains(t, accessRequestStatuses(items), StatusExpired)
	_, err = fixture.service.Approve(t.Context(), testID("tenant-a"), expired.ID, testID("approver"), "too late")
	assert.ErrorIs(t, err, ErrExpired)

	fixture.service.now = func() time.Time { return governanceNow }
	_, err = fixture.db.ExecContext(t.Context(), "INSERT INTO iam_role_members (tenant_id, role_id, principal_id, created_at) VALUES (?, ?, ?, 1)", testID("tenant-a"), testID("role-a"), testID("requester"))
	require.NoError(t, err)
	_, err = fixture.service.Create(t.Context(), testID("tenant-a"), testID("requester"), validGovernanceRequest())
	assert.ErrorIs(t, err, ErrAlreadyGranted)
	_, err = fixture.service.RequestableRoles(t.Context(), 0)
	assert.Error(t, err)
	_, err = fixture.service.List(t.Context(), 0, 0)
	assert.Error(t, err)
}

func TestApprovalIsAtomicUnderFailureAndConcurrency(t *testing.T) {
	t.Run("audit rollback", func(t *testing.T) {
		fixture := newGovernanceFixture(t)
		request := mustCreateAccessRequest(t, fixture)
		_, err := fixture.db.ExecContext(t.Context(), `CREATE TRIGGER reject_governance_approval BEFORE INSERT ON iam_audit_events
			WHEN NEW.event_type = 'access_request_approved' BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END`)
		require.NoError(t, err)

		_, err = fixture.service.Approve(t.Context(), testID("tenant-a"), request.ID, testID("approver"), "approve")
		assert.ErrorContains(t, err, "forced audit failure")
		assertGovernanceRequestState(t, fixture.db, guidString(request.ID), StatusPending, StatusPending, 0)
		revision, revisionErr := policyx.Current(t.Context(), fixture.db, testID("tenant-a"))
		require.NoError(t, revisionErr)
		assert.Zero(t, revision)
	})

	t.Run("one concurrent decision", func(t *testing.T) {
		fixture := newGovernanceFixture(t)
		request := mustCreateAccessRequest(t, fixture)
		start := make(chan struct{})
		errorsSeen := make(chan error, 2)
		var wait sync.WaitGroup
		for _, actor := range []string{"approver", "other"} {
			wait.Add(1)
			go func(actor string) {
				defer wait.Done()
				<-start
				_, err := fixture.service.Approve(context.Background(), testID("tenant-a"), request.ID, testID(actor), "concurrent")
				errorsSeen <- err
			}(actor)
		}
		close(start)
		wait.Wait()
		close(errorsSeen)
		successes, conflicts := 0, 0
		for err := range errorsSeen {
			if err == nil {
				successes++
			} else if errors.Is(err, ErrStateConflict) {
				conflicts++
			} else {
				t.Fatalf("unexpected concurrent approval error: %v", err)
			}
		}
		assert.Equal(t, 1, successes)
		assert.Equal(t, 1, conflicts)
		assertGovernanceRequestState(t, fixture.db, guidString(request.ID), StatusApproved, StatusApproved, 1)
	})
}

type governanceFixture struct {
	db      *bun.DB
	service *Service
}

func newGovernanceFixture(t *testing.T) governanceFixture {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		require.NoError(t, organization.EnsureTenant(t.Context(), db, testID(tenant)))
	}
	now := governanceNow.UnixMilli()
	for _, member := range []struct{ tenant, subject, status string }{
		{"tenant-a", "requester", "active"}, {"tenant-a", "approver", "active"}, {"tenant-a", "other", "active"},
		{"tenant-a", "disabled", "disabled"}, {"tenant-b", "requester", "active"}, {"tenant-b", "approver", "active"},
	} {
		_, err = db.ExecContext(t.Context(), `INSERT INTO iam_tenant_members
			(tenant_id, principal_id, display_name, email, status, created_at, updated_at, disabled_at)
			VALUES (?, ?, ?, '', ?, ?, ?, 0)`, testID(member.tenant), testID(member.subject), member.subject, member.status, now, now)
		require.NoError(t, err)
	}
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_roles
		(tenant_id, id, name, description, created_at, updated_at) VALUES
		(?, ?, 'Operator', 'Temporary operator access', ?, ?),
		(?, ?, 'Other', '', ?, ?)`, testID("tenant-a"), testID("role-a"), now, now, testID("tenant-b"), testID("role-b"), now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_role_permissions
		(tenant_id, role_id, permission_code, created_at) VALUES (?, ?, 'store_view', ?)`, testID("tenant-a"), testID("role-a"), now)
	require.NoError(t, err)

	var sequence atomic.Int64
	service := NewService(db, governanceAuditAppender(db), governanceRoleGrantStore(), func() (guid.ID, error) {
		return guid.ID(sequence.Add(1)), nil
	})
	service.now = func() time.Time { return governanceNow }
	return governanceFixture{db: db, service: service}
}

func governanceRoleGrantStore() RoleGrantStore {
	guard := iam.NewAdministratorGuard()
	return RoleGrantStore{
		Grant: iam.GrantTemporaryRole, Revoke: iam.RevokeTemporaryRole,
		RemovePermanent: func(ctx context.Context, db bun.IDB, tenantID, roleID, principalID guid.ID, createdAt int64) (bool, error) {
			changed, err := guard.RemoveRoleMember(ctx, db, "sqlite", tenantID, roleID, principalID, createdAt)
			if errors.Is(err, iam.ErrLastTenantAdministrator) {
				return false, ErrReviewLastAdministrator
			}
			return changed, err
		},
		RemoveGroupMembership:    iam.RemoveGroupMembership,
		RemovePositionMembership: iam.RemovePositionMembership,
		RemoveEntityRoleBinding:  iam.RemoveEntityRoleBinding,
	}
}

func governanceAuditAppender(db *bun.DB) auditx.Appender {
	service := auditmod.NewService(db, newTestIDGenerator())
	return func(ctx context.Context, executor bun.IDB, event auditx.Event) error {
		_, err := service.AppendTo(ctx, executor, auditmod.EventInput{
			TenantID: event.TenantID, PrincipalID: event.PrincipalID, EventType: event.EventType,
			TargetType: event.TargetType, TargetID: event.TargetID, Outcome: "success", Detail: event.Detail,
		})
		return err
	}
}

func validGovernanceRequest() CreateAccessRequest {
	return CreateAccessRequest{RoleID: testID("role-a"), Reason: "temporary operational access", AccessExpiresAt: governanceNow.Add(24 * time.Hour)}
}

func mustCreateAccessRequest(t *testing.T, fixture governanceFixture) AccessRequest {
	t.Helper()
	request, err := fixture.service.Create(t.Context(), testID("tenant-a"), testID("requester"), validGovernanceRequest())
	require.NoError(t, err)
	return request
}

func accessRequestStatuses(items []AccessRequest) []string {
	statuses := make([]string, 0, len(items))
	for _, item := range items {
		statuses = append(statuses, item.Status)
	}
	return statuses
}

func assertGovernanceRequestState(t *testing.T, db *bun.DB, requestID, requestStatus, stepStatus string, grants int) {
	t.Helper()
	var actualRequest, actualStep string
	require.NoError(t, db.NewSelect().Table("iam_access_requests").Column("status").Where("tenant_id = ? AND id = ?", testID("tenant-a"), parseGUID(requestID)).Scan(t.Context(), &actualRequest))
	require.NoError(t, db.NewSelect().Table("iam_approval_steps").Column("decision").Where("tenant_id = ? AND request_id = ?", testID("tenant-a"), parseGUID(requestID)).Scan(t.Context(), &actualStep))
	grantCount, err := db.NewSelect().Table("iam_temporary_role_grants").Where("tenant_id = ? AND id = ?", testID("tenant-a"), parseGUID(requestID)).Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, requestStatus, actualRequest)
	assert.Equal(t, stepStatus, actualStep)
	assert.Equal(t, grants, grantCount)
}

func TestServiceConstructorAndIDFailure(t *testing.T) {
	fixture := newGovernanceFixture(t)
	assert.Panics(t, func() {
		NewService(nil, governanceAuditAppender(fixture.db), RoleGrantStore{}, newTestIDGenerator())
	})
	assert.Panics(t, func() { NewService(fixture.db, nil, RoleGrantStore{}, newTestIDGenerator()) })
	assert.Panics(t, func() {
		NewService(fixture.db, governanceAuditAppender(fixture.db), RoleGrantStore{}, newTestIDGenerator())
	})
	service := NewService(fixture.db, governanceAuditAppender(fixture.db), governanceRoleGrantStore(), func() (guid.ID, error) { return 0, errors.New("entropy unavailable") })
	service.now = func() time.Time { return governanceNow }
	_, err := service.Create(t.Context(), testID("tenant-a"), testID("requester"), validGovernanceRequest())
	assert.ErrorContains(t, err, "entropy unavailable")
}
