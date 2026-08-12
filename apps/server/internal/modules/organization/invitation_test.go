package organization

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

type invitationFixture struct {
	db       *bun.DB
	service  *InvitationService
	identity *identity.Service
}

func TestInvitationLifecycle(t *testing.T) {
	fixture := newInvitationFixture(t)
	started := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	fixture.service.now = func() time.Time { return started }

	item, token, err := fixture.service.Create(t.Context(), testID("tenant-a"), CreateInvitation{
		Email: " USER@Example.com ", DepartmentID: testID("department-a"), RoleIDs: []guid.ID{testID("role-b"), testID("role-a"), testID("role-a")}, TTL: 24 * time.Hour,
	})
	require.NoError(t, err)
	assert.Equal(t, "user@example.com", item.Email)
	assert.Equal(t, []guid.ID{testID("role-a"), testID("role-b")}, item.RoleIDs)
	assert.Equal(t, InvitationPending, item.Status)

	var stored invitationRow
	require.NoError(t, fixture.db.NewSelect().Model(&stored).Where("tenant_id = ? AND id = ?", testID("tenant-a"), item.ID).Scan(t.Context()))
	assert.NotEqual(t, token, stored.TokenHMAC)
	assert.NotContains(t, stored.TokenHMAC, token)
	assert.NotContains(t, token, stored.TokenHMAC)

	items, err := fixture.service.List(t.Context(), testID("tenant-a"))
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, item.ID, items[0].ID)

	resent, replacement, err := fixture.service.Resend(t.Context(), testID("tenant-a"), item.ID, 48*time.Hour)
	require.NoError(t, err)
	assert.NotEqual(t, token, replacement)
	assert.Equal(t, started.Add(48*time.Hour), resent.ExpiresAt)
	_, err = fixture.service.Accept(t.Context(), AcceptInvitation{Token: token, LoginName: "invited", Password: "correct horse battery staple"})
	assert.ErrorIs(t, err, ErrInvitationCredential)

	accepted, err := fixture.service.Accept(t.Context(), AcceptInvitation{Token: replacement, LoginName: " Invited ", Password: "correct horse battery staple", DisplayName: "Invited User"})
	require.NoError(t, err)
	assert.False(t, accepted.AlreadyAccepted)
	assert.Equal(t, "user@example.com", accepted.Email)

	repeated, err := fixture.service.Accept(t.Context(), AcceptInvitation{Token: replacement, LoginName: "ignored", Password: "different secure password"})
	require.NoError(t, err)
	assert.True(t, repeated.AlreadyAccepted)
	assert.Equal(t, accepted.PrincipalID, repeated.PrincipalID)

	principal, err := fixture.identity.Get(t.Context(), testID("tenant-a"), accepted.PrincipalID)
	require.NoError(t, err)
	assert.Equal(t, "invited", principal.LoginName)
	assert.Equal(t, "user@example.com", principal.Email)
	assert.True(t, principal.EmailVerified)
	var departmentID guid.ID
	require.NoError(t, fixture.db.NewSelect().Table("iam_member_departments").Column("department_id").Where("tenant_id = ? AND principal_id = ?", testID("tenant-a"), accepted.PrincipalID).Scan(t.Context(), &departmentID))
	assert.Equal(t, testID("department-a"), departmentID)
	roleCount, err := fixture.db.NewSelect().Table("iam_role_members").Where("tenant_id = ? AND principal_id = ?", testID("tenant-a"), accepted.PrincipalID).Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, roleCount)
	revision, err := policyx.Current(t.Context(), fixture.db, testID("tenant-a"))
	require.NoError(t, err)
	assert.Equal(t, int64(1), revision)

	events, total, err := auditmod.NewService(fixture.db, newTestIDGenerator()).List(t.Context(), auditmod.Filter{TenantID: testID("tenant-a"), Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	for _, event := range events {
		assert.NotContains(t, string(event.Detail), token)
		assert.NotContains(t, string(event.Detail), replacement)
	}
	integrity, err := auditmod.NewService(fixture.db, newTestIDGenerator()).Verify(t.Context(), testID("tenant-a"))
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
}

func TestInvitationRevokeExpireAndBindingFailures(t *testing.T) {
	fixture := newInvitationFixture(t)
	started := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	fixture.service.now = func() time.Time { return started }

	revoked, revokedToken, err := fixture.service.Create(t.Context(), testID("tenant-a"), CreateInvitation{Email: "revoked@example.com"})
	require.NoError(t, err)
	assert.NotNil(t, revoked.RoleIDs)
	assert.Empty(t, revoked.RoleIDs)
	require.NoError(t, fixture.service.Revoke(t.Context(), testID("tenant-a"), revoked.ID))
	_, err = fixture.service.Accept(t.Context(), AcceptInvitation{Token: revokedToken, LoginName: "revoked", Password: "correct horse battery staple"})
	assert.ErrorIs(t, err, ErrInvitationCredential)
	assert.ErrorIs(t, fixture.service.Revoke(t.Context(), testID("tenant-a"), revoked.ID), ErrInvitationState)

	expired, expiredToken, err := fixture.service.Create(t.Context(), testID("tenant-a"), CreateInvitation{Email: "expired@example.com", TTL: time.Hour})
	require.NoError(t, err)
	fixture.service.now = func() time.Time { return started.Add(time.Hour) }
	items, err := fixture.service.List(t.Context(), testID("tenant-a"))
	require.NoError(t, err)
	assert.Equal(t, InvitationExpired, invitationStatus(items, expired.ID))
	_, err = fixture.service.Accept(t.Context(), AcceptInvitation{Token: expiredToken, LoginName: "expired", Password: "correct horse battery staple"})
	assert.ErrorIs(t, err, ErrInvitationExpired)

	fixture.service.now = func() time.Time { return started }
	missing, missingToken, err := fixture.service.Create(t.Context(), testID("tenant-a"), CreateInvitation{Email: "missing@example.com", DepartmentID: testID("department-a")})
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), "DELETE FROM iam_department_closure WHERE tenant_id = ? AND descendant_id = ?", testID("tenant-a"), testID("department-a"))
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), "DELETE FROM iam_departments WHERE tenant_id = ? AND id = ?", testID("tenant-a"), testID("department-a"))
	require.NoError(t, err)
	_, err = fixture.service.Accept(t.Context(), AcceptInvitation{Token: missingToken, LoginName: "missing", Password: "correct horse battery staple"})
	assert.ErrorIs(t, err, ErrInvitationBindingMissing)
	assert.Equal(t, InvitationPending, invitationStatus(mustInvitations(t, fixture.service, testID("tenant-a")), missing.ID))

	_, _, err = fixture.service.Create(t.Context(), testID("tenant-a"), CreateInvitation{Email: "role@example.com", RoleIDs: []guid.ID{testID("unknown-role")}})
	assert.ErrorIs(t, err, ErrInvitationBindingMissing)
	_, err = fixture.db.NewUpdate().Table("iam_tenants").Set("status = ?", TenantSuspended).Where("id = ?", testID("tenant-a")).Exec(t.Context())
	require.NoError(t, err)
	_, _, err = fixture.service.Create(t.Context(), testID("tenant-a"), CreateInvitation{Email: "suspended@example.com"})
	assert.ErrorIs(t, err, ErrInvitationBindingInactive)
}

func TestInvitationLoginConflictAndAuditRollback(t *testing.T) {
	fixture := newInvitationFixture(t)
	_, err := fixture.identity.Create(t.Context(), testID("tenant-a"), "existing", "correct horse battery staple", "Existing", "existing@example.com")
	require.NoError(t, err)
	_, token, err := fixture.service.Create(t.Context(), testID("tenant-a"), CreateInvitation{Email: "new@example.com", RoleIDs: []guid.ID{testID("role-a")}})
	require.NoError(t, err)
	_, err = fixture.service.Accept(t.Context(), AcceptInvitation{Token: token, LoginName: "existing", Password: "another secure password"})
	assert.ErrorIs(t, err, ErrInvitationLoginConflict)

	rollback, rollbackToken, err := fixture.service.Create(t.Context(), testID("tenant-a"), CreateInvitation{Email: "rollback@example.com", RoleIDs: []guid.ID{testID("role-a")}})
	require.NoError(t, err)
	revisionBefore, err := policyx.Current(t.Context(), fixture.db, testID("tenant-a"))
	require.NoError(t, err)
	_, err = fixture.db.ExecContext(t.Context(), `CREATE TRIGGER deny_invitation_accept_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'invitation_accepted' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)
	_, err = fixture.service.Accept(t.Context(), AcceptInvitation{Token: rollbackToken, LoginName: "rollback", Password: "correct horse battery staple"})
	assert.ErrorContains(t, err, "audit denied")
	assert.Equal(t, InvitationPending, invitationStatus(mustInvitations(t, fixture.service, testID("tenant-a")), rollback.ID))
	count, err := fixture.db.NewSelect().Table("iam_principals").Where("login_name = ?", "rollback").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
	revisionAfter, err := policyx.Current(t.Context(), fixture.db, testID("tenant-a"))
	require.NoError(t, err)
	assert.Equal(t, revisionBefore, revisionAfter)
}

func TestInvitationConcurrentAcceptIsIdempotent(t *testing.T) {
	fixture := newInvitationFixture(t)
	_, token, err := fixture.service.Create(t.Context(), testID("tenant-a"), CreateInvitation{Email: "parallel@example.com"})
	require.NoError(t, err)

	results := make([]InvitationAcceptance, 2)
	errorsFound := make([]error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for index := range results {
		go func() {
			defer wait.Done()
			results[index], errorsFound[index] = fixture.service.Accept(context.Background(), AcceptInvitation{Token: token, LoginName: "parallel", Password: "correct horse battery staple"})
		}()
	}
	wait.Wait()
	require.NoError(t, errors.Join(errorsFound...))
	assert.Equal(t, results[0].PrincipalID, results[1].PrincipalID)
	assert.NotEqual(t, results[0].AlreadyAccepted, results[1].AlreadyAccepted)
	count, err := fixture.db.NewSelect().Table("iam_principals").Where("login_name = ?", "parallel").Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestInvitationValidation(t *testing.T) {
	fixture := newInvitationFixture(t)
	for _, input := range []CreateInvitation{
		{},
		{Email: "not-an-email"},
		{Email: "user@example.com", TTL: time.Minute},
		{Email: "user@example.com", DepartmentID: guid.ID(-1)},
		{Email: "user@example.com", RoleIDs: []guid.ID{0}},
	} {
		_, _, err := fixture.service.Create(t.Context(), testID("tenant-a"), input)
		assert.ErrorIs(t, err, ErrInvitationInvalid)
	}
	_, err := fixture.service.List(t.Context(), 0)
	assert.ErrorIs(t, err, ErrInvitationInvalid)
	assert.ErrorIs(t, fixture.service.Revoke(t.Context(), testID("tenant-a"), 0), ErrInvitationInvalid)
	_, _, err = fixture.service.Resend(t.Context(), testID("tenant-a"), testID("missing"), time.Minute)
	assert.ErrorIs(t, err, ErrInvitationInvalid)
	_, err = fixture.service.Accept(t.Context(), AcceptInvitation{Token: "invalid", LoginName: "user", Password: "correct horse battery staple"})
	assert.ErrorIs(t, err, ErrInvitationCredential)
	assert.Panics(t, func() { NewInvitationService(nil, nil, nil, nil, nil) })
}

func TestInvitationStateAndPersistenceFailures(t *testing.T) {
	fixture := newInvitationFixture(t)
	started := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	fixture.service.now = func() time.Time { return started }

	_, _, err := fixture.service.Resend(t.Context(), testID("tenant-a"), testID("missing"), time.Hour)
	assert.ErrorIs(t, err, ErrInvitationNotFound)
	assert.ErrorIs(t, fixture.service.Revoke(t.Context(), testID("tenant-a"), testID("missing")), ErrInvitationNotFound)
	_, err = fixture.service.Accept(t.Context(), AcceptInvitation{Token: "inv1_missing.", LoginName: "missing", Password: "correct horse battery staple"})
	assert.ErrorIs(t, err, ErrInvitationCredential)

	expired, _, err := fixture.service.Create(t.Context(), testID("tenant-a"), CreateInvitation{Email: "expired-revoke@example.com", TTL: time.Hour})
	require.NoError(t, err)
	fixture.service.now = func() time.Time { return started.Add(time.Hour) }
	assert.ErrorIs(t, fixture.service.Revoke(t.Context(), testID("tenant-a"), expired.ID), ErrInvitationExpired)

	fixture.service.now = func() time.Time { return started }
	revoked, _, err := fixture.service.Create(t.Context(), testID("tenant-a"), CreateInvitation{Email: "revoked-resend@example.com"})
	require.NoError(t, err)
	require.NoError(t, fixture.service.Revoke(t.Context(), testID("tenant-a"), revoked.ID))
	_, _, err = fixture.service.Resend(t.Context(), testID("tenant-a"), revoked.ID, time.Hour)
	assert.ErrorIs(t, err, ErrInvitationState)

	require.NoError(t, createSQLiteFailureTrigger(t, fixture.db, "deny_invitation_insert", "INSERT", "insert denied"))
	_, _, err = fixture.service.Create(t.Context(), testID("tenant-a"), CreateInvitation{Email: "insert-failure@example.com"})
	assert.ErrorContains(t, err, "insert denied")
	require.NoError(t, dropSQLiteTrigger(t, fixture.db, "deny_invitation_insert"))

	pending, _, err := fixture.service.Create(t.Context(), testID("tenant-a"), CreateInvitation{Email: "update-failure@example.com"})
	require.NoError(t, err)
	require.NoError(t, createSQLiteFailureTrigger(t, fixture.db, "deny_invitation_update", "UPDATE", "update denied"))
	_, _, err = fixture.service.Resend(t.Context(), testID("tenant-a"), pending.ID, time.Hour)
	assert.ErrorContains(t, err, "update denied")
	assert.ErrorContains(t, fixture.service.Revoke(t.Context(), testID("tenant-a"), pending.ID), "update denied")
	require.NoError(t, dropSQLiteTrigger(t, fixture.db, "deny_invitation_update"))
}

func createSQLiteFailureTrigger(t *testing.T, db *bun.DB, name, operation, message string) error {
	t.Helper()
	_, err := db.ExecContext(t.Context(), fmt.Sprintf("CREATE TRIGGER %s BEFORE %s ON iam_invitations BEGIN SELECT RAISE(ABORT, '%s'); END", name, operation, message))
	return err
}

func newInvitationFixture(t *testing.T) invitationFixture {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, Migrate(t.Context(), db))
	require.NoError(t, EnsureTenant(t.Context(), db, testID("tenant-a")))
	now := time.Now().UTC().UnixMilli()
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_departments (tenant_id,id,parent_id,name,name_key,status,sort_order,version,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, testID("tenant-a"), testID("department-a"), guid.ID(0), "Department A", "department a", StatusActive, 0, 1, now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_department_closure (tenant_id,ancestor_id,descendant_id,depth) VALUES (?,?,?,0)`, testID("tenant-a"), testID("department-a"), testID("department-a"))
	require.NoError(t, err)
	for _, role := range []string{"role-a", "role-b"} {
		_, err = db.ExecContext(t.Context(), `INSERT INTO iam_roles (tenant_id,id,name,description,created_at,updated_at) VALUES (?,?,?,?,?,?)`, testID("tenant-a"), testID(role), role, "", now, now)
		require.NoError(t, err)
	}
	key := base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.SeedSize))
	credentials, err := authnmod.NewWebService(authn.Config{Enabled: true, Issuer: "https://iam.example", SigningKey: key, MFA: authn.MFAConfig{EncryptionKey: key}, Web: authn.WebConfig{Enabled: true}}, db, authnmod.WithIDGenerator(newTestIDGenerator()))
	require.NoError(t, err)
	identityService := identity.NewService(db, realOrganizationAuditAppender(db), iam.NewAdministratorGuard(), newTestIDGenerator())
	createPrincipal := func(ctx context.Context, executor bun.IDB, tenantID guid.ID, loginName, password, displayName, email string) (guid.ID, error) {
		id, createErr := identityService.CreateInvitedPrincipal(ctx, executor, tenantID, loginName, password, displayName, email)
		switch {
		case errors.Is(createErr, identity.ErrLoginConflict):
			return 0, ErrInvitationLoginConflict
		case errors.Is(createErr, identity.ErrInvalid):
			return 0, ErrInvitationInvalid
		default:
			return id, createErr
		}
	}
	var sequence atomic.Int64
	service := NewInvitationService(db, realOrganizationAuditAppender(db), func() (guid.ID, error) {
		return guid.ID(sequence.Add(1)), nil
	}, credentials, createPrincipal)
	return invitationFixture{db: db, service: service, identity: identityService}
}

func invitationStatus(items []Invitation, id guid.ID) string {
	for _, item := range items {
		if item.ID == id {
			return item.Status
		}
	}
	return ""
}

func mustInvitations(t *testing.T, service *InvitationService, tenantID guid.ID) []Invitation {
	t.Helper()
	items, err := service.List(t.Context(), tenantID)
	require.NoError(t, err)
	return items
}
