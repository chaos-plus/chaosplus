package governance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccessReviewLifecycle(t *testing.T) {
	fixture := newGovernanceFixture(t)
	directCreatedAt := addDirectReviewGrant(t, fixture, "requester", governanceNow.Add(-time.Hour))
	temporary := createApprovedReviewGrant(t, fixture, "other")

	review, err := fixture.service.CreateReview(t.Context(), testID("tenant-a"), testID("approver"), CreateAccessReview{
		Name: " Quarterly privileged access ", DueAt: governanceNow.Add(7 * 24 * time.Hour),
	})
	require.NoError(t, err)
	assert.Equal(t, ReviewStatusOpen, review.Status)
	assert.Equal(t, "Quarterly privileged access", review.Name)
	assert.Equal(t, 2, review.Total)
	assert.Equal(t, 2, review.Pending)
	direct := reviewItemByType(t, review, ReviewGrantPermanent)
	temporaryItem := reviewItemByType(t, review, ReviewGrantTemporary)
	assert.Equal(t, directCreatedAt.UnixMilli(), direct.GrantCreatedAt.UnixMilli())
	assert.Equal(t, temporary.ID, temporaryItem.GrantID)
	require.NotNil(t, temporaryItem.GrantExpiresAt)

	_, err = fixture.service.DecideReviewItem(t.Context(), testID("tenant-a"), review.ID, direct.ID, testID("requester"), ReviewDecisionKeep, "self")
	assert.ErrorIs(t, err, ErrReviewSelfDecision)
	_, err = fixture.service.CompleteReview(t.Context(), testID("tenant-a"), review.ID, testID("approver"))
	assert.ErrorIs(t, err, ErrReviewIncomplete)

	kept, err := fixture.service.DecideReviewItem(t.Context(), testID("tenant-a"), review.ID, direct.ID, testID("other"), ReviewDecisionKeep, "still required")
	require.NoError(t, err)
	assert.Equal(t, ReviewDecisionKeep, kept.Decision)
	revoked, err := fixture.service.DecideReviewItem(t.Context(), testID("tenant-a"), review.ID, temporaryItem.ID, testID("approver"), ReviewDecisionRevoke, "incident ended")
	require.NoError(t, err)
	assert.Equal(t, ReviewDecisionRevoke, revoked.Decision)

	completed, err := fixture.service.CompleteReview(t.Context(), testID("tenant-a"), review.ID, testID("approver"))
	require.NoError(t, err)
	assert.Equal(t, ReviewStatusCompleted, completed.Status)
	assert.Equal(t, 2, completed.Total)
	assert.Equal(t, 0, completed.Pending)
	assert.Equal(t, 1, completed.Kept)
	assert.Equal(t, 1, completed.Revoked)
	require.NotNil(t, completed.CompletedAt)

	allowed, err := iam.NewAuthorizer(fixture.db).Check(t.Context(), testID("tenant-a"), "store_view", testID("other"))
	require.NoError(t, err)
	assert.False(t, allowed)
	allowed, err = iam.NewAuthorizer(fixture.db).Check(t.Context(), testID("tenant-a"), "store_view", testID("requester"))
	require.NoError(t, err)
	assert.True(t, allowed)
	revision, err := policyx.Current(t.Context(), fixture.db, testID("tenant-a"))
	require.NoError(t, err)
	assert.Equal(t, int64(2), revision, "approval and review revocation each advance policy once")

	listed, err := fixture.service.ListReviews(t.Context(), testID("tenant-a"))
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.NotNil(t, listed[0].Items)
	assert.Empty(t, listed[0].Items)
	assert.Equal(t, completed.Total, listed[0].Total)
	_, err = fixture.service.GetReview(t.Context(), testID("tenant-b"), review.ID)
	assert.ErrorIs(t, err, ErrReviewNotFound)

	integrity, err := auditmod.NewService(fixture.db, newTestIDGenerator()).Verify(t.Context(), testID("tenant-a"))
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
}

func TestAccessReviewCancellationExpiryAndValidation(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		fixture := newGovernanceFixture(t)
		addDirectReviewGrant(t, fixture, "requester", governanceNow)
		review, err := fixture.service.CreateReview(t.Context(), testID("tenant-a"), testID("approver"), CreateAccessReview{Name: "Cancel review", DueAt: governanceNow.Add(time.Hour)})
		require.NoError(t, err)
		cancelled, err := fixture.service.CancelReview(t.Context(), testID("tenant-a"), review.ID, testID("approver"))
		require.NoError(t, err)
		assert.Equal(t, ReviewStatusCancelled, cancelled.Status)
		_, err = fixture.service.DecideReviewItem(t.Context(), testID("tenant-a"), review.ID, review.Items[0].ID, testID("other"), ReviewDecisionKeep, "")
		assert.ErrorIs(t, err, ErrReviewStateConflict)
		_, err = fixture.service.CancelReview(t.Context(), testID("tenant-a"), review.ID, testID("approver"))
		assert.ErrorIs(t, err, ErrReviewStateConflict)
	})

	t.Run("expiry", func(t *testing.T) {
		fixture := newGovernanceFixture(t)
		addDirectReviewGrant(t, fixture, "requester", governanceNow)
		review, err := fixture.service.CreateReview(t.Context(), testID("tenant-a"), testID("approver"), CreateAccessReview{Name: "Expiring review", DueAt: governanceNow.Add(time.Hour)})
		require.NoError(t, err)
		fixture.service.now = func() time.Time { return governanceNow.Add(2 * time.Hour) }
		loaded, err := fixture.service.GetReview(t.Context(), testID("tenant-a"), review.ID)
		require.NoError(t, err)
		assert.Equal(t, ReviewStatusExpired, loaded.Status)
		_, err = fixture.service.DecideReviewItem(t.Context(), testID("tenant-a"), review.ID, review.Items[0].ID, testID("other"), ReviewDecisionKeep, "")
		assert.ErrorIs(t, err, ErrReviewExpired)
		_, err = fixture.service.CompleteReview(t.Context(), testID("tenant-a"), review.ID, testID("approver"))
		assert.ErrorIs(t, err, ErrReviewExpired)
	})

	t.Run("empty and invalid", func(t *testing.T) {
		fixture := newGovernanceFixture(t)
		_, err := fixture.service.CreateReview(t.Context(), testID("tenant-a"), testID("approver"), CreateAccessReview{Name: "Empty review", DueAt: governanceNow.Add(time.Hour)})
		assert.ErrorIs(t, err, ErrReviewEmpty)
		_, err = fixture.service.CreateReview(t.Context(), 0, testID("approver"), CreateAccessReview{Name: "x", DueAt: governanceNow})
		assert.ErrorIs(t, err, ErrInvalidReview)
		_, err = fixture.service.ListReviews(t.Context(), 0)
		assert.ErrorIs(t, err, ErrInvalidReview)
		_, err = fixture.service.GetReview(t.Context(), testID("tenant-a"), 0)
		assert.ErrorIs(t, err, ErrInvalidReview)
		_, err = fixture.service.DecideReviewItem(t.Context(), testID("tenant-a"), testID("review"), testID("item"), testID("approver"), "unknown", "")
		assert.ErrorIs(t, err, ErrInvalidReview)
		_, err = fixture.service.finishReview(t.Context(), testID("tenant-a"), testID("review"), testID("approver"), "unknown")
		assert.ErrorIs(t, err, ErrInvalidReview)
	})
}

func TestAccessReviewRevocationIsAtomicAndConcurrent(t *testing.T) {
	t.Run("audit failure rolls back grant and decision", func(t *testing.T) {
		fixture := newGovernanceFixture(t)
		addDirectReviewGrant(t, fixture, "requester", governanceNow)
		review, err := fixture.service.CreateReview(t.Context(), testID("tenant-a"), testID("approver"), CreateAccessReview{Name: "Atomic review", DueAt: governanceNow.Add(time.Hour)})
		require.NoError(t, err)
		_, err = fixture.db.ExecContext(t.Context(), `CREATE TRIGGER reject_review_revoke BEFORE INSERT ON iam_audit_events
			WHEN NEW.event_type = 'access_review_item_revoked' BEGIN SELECT RAISE(ABORT, 'forced review audit failure'); END`)
		require.NoError(t, err)

		_, err = fixture.service.DecideReviewItem(t.Context(), testID("tenant-a"), review.ID, review.Items[0].ID, testID("other"), ReviewDecisionRevoke, "remove")
		assert.ErrorContains(t, err, "forced review audit failure")
		loaded, loadErr := fixture.service.GetReview(t.Context(), testID("tenant-a"), review.ID)
		require.NoError(t, loadErr)
		assert.Equal(t, ReviewDecisionPending, loaded.Items[0].Decision)
		assertDirectReviewGrant(t, fixture, "requester", true)
		revision, revisionErr := policyx.Current(t.Context(), fixture.db, testID("tenant-a"))
		require.NoError(t, revisionErr)
		assert.Zero(t, revision)
	})

	t.Run("one concurrent item decision", func(t *testing.T) {
		fixture := newGovernanceFixture(t)
		addDirectReviewGrant(t, fixture, "requester", governanceNow)
		review, err := fixture.service.CreateReview(t.Context(), testID("tenant-a"), testID("approver"), CreateAccessReview{Name: "Concurrent review", DueAt: governanceNow.Add(time.Hour)})
		require.NoError(t, err)
		start := make(chan struct{})
		results := make(chan error, 2)
		var workers sync.WaitGroup
		for _, actor := range []string{"approver", "other"} {
			workers.Add(1)
			go func(actor string) {
				defer workers.Done()
				<-start
				_, decideErr := fixture.service.DecideReviewItem(context.Background(), testID("tenant-a"), review.ID, review.Items[0].ID, testID(actor), ReviewDecisionKeep, "concurrent")
				results <- decideErr
			}(actor)
		}
		close(start)
		workers.Wait()
		close(results)
		succeeded, conflicted := 0, 0
		for result := range results {
			switch {
			case result == nil:
				succeeded++
			case errors.Is(result, ErrReviewStateConflict):
				conflicted++
			default:
				t.Fatalf("unexpected review decision error: %v", result)
			}
		}
		assert.Equal(t, 1, succeeded)
		assert.Equal(t, 1, conflicted)
	})
}

func TestAccessReviewProtectsLastAdministratorAndSnapshot(t *testing.T) {
	fixture := newGovernanceFixture(t)
	now := governanceNow.UnixMilli()
	_, err := fixture.db.ExecContext(t.Context(), `INSERT INTO iam_principals
		(id,login_name,email,display_name,status,created_at,updated_at,disabled_at)
		VALUES (?,?,?,?,?,?,?,0)`, testID("admin"), "admin", "admin@example.test", "Admin", "active", now, now)
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), `INSERT INTO iam_tenant_members
		(tenant_id,principal_id,display_name,email,status,created_at,updated_at,disabled_at)
		VALUES (?,?,?,?,?,?,?,0)`, testID("tenant-a"), testID("admin"), "Admin", "admin@example.test", "active", now, now)
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), `INSERT INTO iam_role_permissions
		(tenant_id,role_id,permission_code,created_at) VALUES (?,?,?,?)`, testID("tenant-a"), testID("role-a"), "tenant_administer", now)
	require.NoError(t, err)
	addDirectReviewGrant(t, fixture, "admin", governanceNow)

	review, err := fixture.service.CreateReview(t.Context(), testID("tenant-a"), testID("approver"), CreateAccessReview{Name: "Administrator review", DueAt: governanceNow.Add(time.Hour)})
	require.NoError(t, err)
	_, err = fixture.service.DecideReviewItem(t.Context(), testID("tenant-a"), review.ID, review.Items[0].ID, testID("approver"), ReviewDecisionRevoke, "remove")
	assert.ErrorIs(t, err, ErrReviewLastAdministrator)
	assertDirectReviewGrant(t, fixture, "admin", true)

	_, err = fixture.db.NewDelete().Table("iam_role_members").Where("tenant_id = ? AND role_id = ? AND principal_id = ?", testID("tenant-a"), testID("role-a"), testID("admin")).Exec(t.Context())
	require.NoError(t, err)
	newCreatedAt := governanceNow.Add(time.Minute).UnixMilli()
	_, err = fixture.db.ExecContext(t.Context(), `INSERT INTO iam_role_members (tenant_id,role_id,principal_id,created_at)
		VALUES (?,?,?,?)`, testID("tenant-a"), testID("role-a"), testID("admin"), newCreatedAt)
	require.NoError(t, err)
	item, err := fixture.service.DecideReviewItem(t.Context(), testID("tenant-a"), review.ID, review.Items[0].ID, testID("approver"), ReviewDecisionRevoke, "stale snapshot")
	require.NoError(t, err)
	assert.Equal(t, ReviewDecisionRevoke, item.Decision)
	assertDirectReviewGrant(t, fixture, "admin", true)
}

func TestAccessReviewRejectsInvalidStateAndRealStorageFailures(t *testing.T) {
	t.Run("inactive owner", func(t *testing.T) {
		fixture := newGovernanceFixture(t)
		addDirectReviewGrant(t, fixture, "requester", governanceNow)
		_, err := fixture.service.CreateReview(t.Context(), testID("tenant-a"), testID("inactive"), CreateAccessReview{
			Name: "Inactive owner review", DueAt: governanceNow.Add(time.Hour),
		})
		assert.ErrorIs(t, err, ErrRequesterInactive)
	})

	t.Run("missing item", func(t *testing.T) {
		fixture := newGovernanceFixture(t)
		addDirectReviewGrant(t, fixture, "requester", governanceNow)
		review, err := fixture.service.CreateReview(t.Context(), testID("tenant-a"), testID("approver"), CreateAccessReview{
			Name: "Missing item review", DueAt: governanceNow.Add(time.Hour),
		})
		require.NoError(t, err)
		_, err = fixture.service.DecideReviewItem(t.Context(), testID("tenant-a"), review.ID, testID("missing"), testID("other"), ReviewDecisionKeep, "")
		assert.ErrorIs(t, err, ErrReviewItemNotFound)
	})

	t.Run("review table unavailable", func(t *testing.T) {
		fixture := newGovernanceFixture(t)
		_, err := fixture.db.ExecContext(t.Context(), "DROP TABLE iam_access_reviews")
		require.NoError(t, err)
		_, err = fixture.service.ListReviews(t.Context(), testID("tenant-a"))
		assert.ErrorContains(t, err, "list access reviews")
	})

	t.Run("review lookup unavailable", func(t *testing.T) {
		fixture := newGovernanceFixture(t)
		_, err := fixture.db.ExecContext(t.Context(), "DROP TABLE iam_access_reviews")
		require.NoError(t, err)
		_, err = fixture.service.GetReview(t.Context(), testID("tenant-a"), testID("missing"))
		assert.ErrorContains(t, err, "get access review")
	})

	t.Run("grant source unavailable", func(t *testing.T) {
		fixture := newGovernanceFixture(t)
		_, err := fixture.db.ExecContext(t.Context(), "DROP TABLE iam_role_members")
		require.NoError(t, err)
		_, err = fixture.service.CreateReview(t.Context(), testID("tenant-a"), testID("approver"), CreateAccessReview{
			Name: "Grant source failure", DueAt: governanceNow.Add(time.Hour),
		})
		assert.ErrorContains(t, err, "list reviewable role grants")
	})

	t.Run("review count table unavailable", func(t *testing.T) {
		fixture := newGovernanceFixture(t)
		addDirectReviewGrant(t, fixture, "requester", governanceNow)
		_, err := fixture.service.CreateReview(t.Context(), testID("tenant-a"), testID("approver"), CreateAccessReview{
			Name: "Count failure review", DueAt: governanceNow.Add(time.Hour),
		})
		require.NoError(t, err)
		_, err = fixture.db.ExecContext(t.Context(), "DROP TABLE iam_access_review_items")
		require.NoError(t, err)
		_, err = fixture.service.ListReviews(t.Context(), testID("tenant-a"))
		assert.ErrorContains(t, err, "count access review items")
	})

	t.Run("review item table unavailable", func(t *testing.T) {
		fixture := newGovernanceFixture(t)
		addDirectReviewGrant(t, fixture, "requester", governanceNow)
		review, err := fixture.service.CreateReview(t.Context(), testID("tenant-a"), testID("approver"), CreateAccessReview{
			Name: "Item failure review", DueAt: governanceNow.Add(time.Hour),
		})
		require.NoError(t, err)
		_, err = fixture.db.ExecContext(t.Context(), "DROP TABLE iam_access_review_items")
		require.NoError(t, err)
		_, err = fixture.service.GetReview(t.Context(), testID("tenant-a"), review.ID)
		assert.ErrorContains(t, err, "list access review items")
	})
}

func TestAccessReviewCoversDerivedGrants(t *testing.T) {
	fixture := newGovernanceFixture(t)
	now := governanceNow.UnixMilli()

	_, err := fixture.db.ExecContext(t.Context(), `INSERT INTO iam_groups
		(tenant_id,id,name,name_key,group_type,description,status,sort_order,version,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`, testID("tenant-a"), testID("group-a"), "Group A", "group a", "static", "", "active", 0, 1, now, now)
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), `INSERT INTO iam_group_members
		(tenant_id,group_id,principal_id,starts_at,ends_at,created_at,updated_at)
		VALUES (?,?,?,0,0,?,?)`, testID("tenant-a"), testID("group-a"), testID("requester"), now, now)
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), `INSERT INTO iam_group_role_bindings
		(tenant_id,role_id,group_id,created_at) VALUES (?,?,?,?)`, testID("tenant-a"), testID("role-a"), testID("group-a"), now)
	require.NoError(t, err)

	_, err = fixture.db.ExecContext(t.Context(), `INSERT INTO iam_positions
		(tenant_id,id,code,name,status,sort_order,version,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, testID("tenant-a"), testID("position-a"), "pa", "Position A", "active", 0, 1, now, now)
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), `INSERT INTO iam_position_members
		(tenant_id,position_id,principal_id,starts_at,ends_at,created_at,updated_at)
		VALUES (?,?,?,0,0,?,?)`, testID("tenant-a"), testID("position-a"), testID("other"), now, now)
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), `INSERT INTO iam_position_role_bindings
		(tenant_id,role_id,position_id,created_at) VALUES (?,?,?,?)`, testID("tenant-a"), testID("role-a"), testID("position-a"), now)
	require.NoError(t, err)

	_, err = fixture.db.ExecContext(t.Context(), `INSERT INTO iam_entities
		(tenant_id,id,parent_id,type,name,status,metadata,created_at,updated_at)
		VALUES (?,?,NULL,?,?,?,'{}',?,?)`, testID("tenant-a"), testID("entity-a"), "store", "Store A", "active", now, now)
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), `INSERT INTO iam_role_bindings
		(tenant_id,role_id,principal_id,scope_type,scope_id,effect,expires_at,created_at)
		VALUES (?,?,?,?,?,?,0,?)`, testID("tenant-a"), testID("role-a"), testID("requester"), "entity", testID("entity-a"), "allow", now)
	require.NoError(t, err)

	rule := `{"version":1,"match":"all","conditions":[{"field":"member.subject","operator":"in","values":["` + wireID("requester") + `"]}]}`
	_, err = fixture.db.ExecContext(t.Context(), `INSERT INTO iam_groups
		(tenant_id,id,name,name_key,group_type,rule_json,description,status,sort_order,version,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, testID("tenant-a"), testID("dynamic-a"), "Dynamic A", "dynamic a", "dynamic", rule, "", "active", 0, 1, now, now)
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), `INSERT INTO iam_group_role_bindings
		(tenant_id,role_id,group_id,created_at) VALUES (?,?,?,?)`, testID("tenant-a"), testID("role-a"), testID("dynamic-a"), now)
	require.NoError(t, err)

	review, err := fixture.service.CreateReview(t.Context(), testID("tenant-a"), testID("approver"), CreateAccessReview{
		Name: "Derived access review", DueAt: governanceNow.Add(24 * time.Hour),
	})
	require.NoError(t, err)
	assert.Equal(t, 4, review.Total)
	groupItem := reviewItemByType(t, review, ReviewGrantGroup)
	positionItem := reviewItemByType(t, review, ReviewGrantPosition)
	entityItem := reviewItemByType(t, review, ReviewGrantEntity)
	dynamicItem := reviewItemByType(t, review, ReviewGrantDynamicGroup)
	assert.Equal(t, testID("group-a"), groupItem.GrantID)
	assert.Equal(t, testID("position-a"), positionItem.GrantID)
	assert.Equal(t, testID("entity-a"), entityItem.GrantID)
	assert.Equal(t, testID("dynamic-a"), dynamicItem.GrantID)

	_, err = fixture.service.DecideReviewItem(t.Context(), testID("tenant-a"), review.ID, groupItem.ID, testID("approver"), ReviewDecisionRevoke, "not needed")
	require.NoError(t, err)
	assertReviewRowCount(t, fixture, "iam_group_members", 0)
	_, err = fixture.service.DecideReviewItem(t.Context(), testID("tenant-a"), review.ID, positionItem.ID, testID("approver"), ReviewDecisionRevoke, "role change")
	require.NoError(t, err)
	assertReviewRowCount(t, fixture, "iam_position_members", 0)
	_, err = fixture.service.DecideReviewItem(t.Context(), testID("tenant-a"), review.ID, entityItem.ID, testID("approver"), ReviewDecisionRevoke, "scoped access ended")
	require.NoError(t, err)
	assertReviewRowCount(t, fixture, "iam_role_bindings", 0)
	_, err = fixture.service.DecideReviewItem(t.Context(), testID("tenant-a"), review.ID, dynamicItem.ID, testID("approver"), ReviewDecisionRevoke, "not revocable")
	assert.ErrorIs(t, err, ErrReviewDynamicDerived)

	allowed, err := iam.NewAuthorizer(fixture.db).Check(t.Context(), testID("tenant-a"), "store_view", testID("other"))
	require.NoError(t, err)
	assert.False(t, allowed, "position-derived access is severed by the review")
	allowed, err = iam.NewAuthorizer(fixture.db).Check(t.Context(), testID("tenant-a"), "store_view", testID("requester"))
	require.NoError(t, err)
	assert.True(t, allowed, "rule-derived access persists until the group rule or binding changes")
}

func assertReviewRowCount(t *testing.T, fixture governanceFixture, table string, expected int) {
	t.Helper()
	count, err := fixture.db.NewSelect().Table(table).Where("tenant_id = 'tenant-a'").Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, expected, count)
}

func addDirectReviewGrant(t *testing.T, fixture governanceFixture, principalID string, createdAt time.Time) time.Time {
	t.Helper()
	_, err := fixture.db.ExecContext(t.Context(), `INSERT INTO iam_role_members (tenant_id,role_id,principal_id,created_at)
		VALUES (?,?,?,?)`, testID("tenant-a"), testID("role-a"), testID(principalID), createdAt.UnixMilli())
	require.NoError(t, err)
	return createdAt.UTC()
}

func createApprovedReviewGrant(t *testing.T, fixture governanceFixture, principalID string) AccessRequest {
	t.Helper()
	request, err := fixture.service.Create(t.Context(), testID("tenant-a"), testID(principalID), CreateAccessRequest{
		RoleID: testID("role-a"), Reason: "temporary review access", AccessExpiresAt: governanceNow.Add(24 * time.Hour),
	})
	require.NoError(t, err)
	actor := "approver"
	if principalID == actor {
		actor = "requester"
	}
	request, err = fixture.service.Approve(t.Context(), testID("tenant-a"), request.ID, testID(actor), "approved")
	require.NoError(t, err)
	return request
}

func reviewItemByType(t *testing.T, review AccessReview, grantType string) AccessReviewItem {
	t.Helper()
	for _, item := range review.Items {
		if item.GrantType == grantType {
			return item
		}
	}
	t.Fatalf("review item type %s not found", grantType)
	return AccessReviewItem{}
}

func assertDirectReviewGrant(t *testing.T, fixture governanceFixture, principalID string, expected bool) {
	t.Helper()
	count, err := fixture.db.NewSelect().Table("iam_role_members").Where("tenant_id = ? AND role_id = ? AND principal_id = ?", testID("tenant-a"), testID("role-a"), testID(principalID)).Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, expected, count == 1)
}
