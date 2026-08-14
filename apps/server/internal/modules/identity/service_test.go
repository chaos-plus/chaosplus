package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/auditx"
	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
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

func TestPrincipalLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	require.NoError(t, organization.Migrate(context.Background(), db))
	service := newIdentityService(db)
	principal, err := service.Create(context.Background(), testID("tenant-a"), " Alice ", "correct horse battery staple", "Alice", "ALICE@example.com")
	require.NoError(t, err)
	assert.Equal(t, "alice", principal.LoginName)
	assert.Equal(t, "alice@example.com", principal.Email)
	assert.False(t, principal.EmailVerified)
	_, err = service.Create(context.Background(), testID("tenant-a"), "alice", "another secure password", "Alice", "")
	assert.ErrorIs(t, err, ErrLoginConflict)
	items, total, err := service.List(context.Background(), testID("tenant-a"), "ali", 50, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, items, 1)
	principal, err = service.Get(context.Background(), testID("tenant-a"), principal.ID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", principal.DisplayName)

	other, err := service.Create(context.Background(), testID("tenant-b"), "bob", "another secure password", "Bob", "")
	require.NoError(t, err)
	_, err = service.Get(context.Background(), testID("tenant-a"), other.ID)
	assert.ErrorIs(t, err, ErrNotFound)
	items, total, err = service.List(context.Background(), testID("tenant-a"), "", 50, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, principal.ID, items[0].ID)

	_, err = db.NewUpdate().Table("iam_principals").Set("email_verified = ?", true).Where("id = ?", principal.ID).Exec(t.Context())
	require.NoError(t, err)
	unchangedEmail := "ALICE@example.com"
	principal, err = service.Update(t.Context(), testID("tenant-a"), principal.ID, nil, &unchangedEmail)
	require.NoError(t, err)
	assert.True(t, principal.EmailVerified)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_sessions (id_hash, principal_id, created_at, last_seen_at, expires_at, absolute_expires_at, revoked_at, ip_address, user_agent) VALUES (?, ?, 1, 1, 9999999999999, 9999999999999, 0, '', '')`, "email-session", principal.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_refresh_tokens (id_hash, family_id, principal_id, client_id, scope, created_at, expires_at, used_at, revoked_at) VALUES (?, 'email-family', ?, 'client', '', 1, 9999999999999, 0, 0)`, "email-refresh", principal.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_password_recovery_tokens (token_hmac, principal_id, created_at, expires_at, consumed_at) VALUES (?, ?, 1, 9999999999999, 0)`, strings.Repeat("a", 64), principal.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_email_verification_tokens (token_hmac, principal_id, email, created_at, expires_at, consumed_at) VALUES (?, ?, 'alice@example.com', 1, 9999999999999, 0)`, strings.Repeat("b", 64), principal.ID)
	require.NoError(t, err)
	var credentialVersionBefore int64
	require.NoError(t, db.NewSelect().Table("iam_credentials").Column("credential_version").Where("principal_id = ?", principal.ID).Scan(t.Context(), &credentialVersionBefore))

	displayName, email := "Alice Updated", "UPDATED@example.com"
	principal, err = service.Update(t.Context(), testID("tenant-a"), principal.ID, &displayName, &email)
	require.NoError(t, err)
	assert.Equal(t, displayName, principal.DisplayName)
	assert.Equal(t, "updated@example.com", principal.Email)
	assert.False(t, principal.EmailVerified)
	var emailSessionRevoked, emailRefreshRevoked, recoveryConsumed, verificationConsumed, credentialVersionAfter int64
	require.NoError(t, db.NewSelect().Table("iam_sessions").Column("revoked_at").Where("id_hash = 'email-session'").Scan(t.Context(), &emailSessionRevoked))
	require.NoError(t, db.NewSelect().Table("iam_refresh_tokens").Column("revoked_at").Where("id_hash = 'email-refresh'").Scan(t.Context(), &emailRefreshRevoked))
	require.NoError(t, db.NewSelect().Table("iam_password_recovery_tokens").Column("consumed_at").Where("token_hmac = ?", strings.Repeat("a", 64)).Scan(t.Context(), &recoveryConsumed))
	require.NoError(t, db.NewSelect().Table("iam_email_verification_tokens").Column("consumed_at").Where("token_hmac = ?", strings.Repeat("b", 64)).Scan(t.Context(), &verificationConsumed))
	require.NoError(t, db.NewSelect().Table("iam_credentials").Column("credential_version").Where("principal_id = ?", principal.ID).Scan(t.Context(), &credentialVersionAfter))
	assert.Positive(t, emailSessionRevoked)
	assert.Positive(t, emailRefreshRevoked)
	assert.Positive(t, recoveryConsumed)
	assert.Positive(t, verificationConsumed)
	assert.Equal(t, credentialVersionBefore+1, credentialVersionAfter)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_sessions (id_hash, principal_id, created_at, last_seen_at, expires_at, absolute_expires_at, revoked_at, ip_address, user_agent) VALUES (?, ?, 1, 1, 9999999999999, 9999999999999, 0, '', '')`, "session", principal.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_refresh_tokens (id_hash, family_id, principal_id, client_id, scope, created_at, expires_at, used_at, revoked_at) VALUES (?, 'family', ?, 'client', '', 1, 9999999999999, 0, 0)`, "refresh", principal.ID)
	require.NoError(t, err)
	principal, err = service.SetStatus(t.Context(), testID("tenant-a"), principal.ID, "disabled")
	require.NoError(t, err)
	assert.Equal(t, "disabled", principal.Status)
	var sessionRevoked, refreshRevoked int64
	require.NoError(t, db.NewSelect().Table("iam_sessions").Column("revoked_at").Where("id_hash = 'session'").Scan(t.Context(), &sessionRevoked))
	require.NoError(t, db.NewSelect().Table("iam_refresh_tokens").Column("revoked_at").Where("id_hash = 'refresh'").Scan(t.Context(), &refreshRevoked))
	assert.Positive(t, sessionRevoked)
	assert.Positive(t, refreshRevoked)
	principal, err = service.SetStatus(t.Context(), testID("tenant-a"), principal.ID, "active")
	require.NoError(t, err)
	assert.Equal(t, "active", principal.Status)
}

func TestCreateInvitedPrincipalUsesCallerTransaction(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, testID("tenant")))
	service := newIdentityService(db)

	err = db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		id, err := service.CreateInvitedPrincipal(ctx, tx, testID("tenant"), " Invited ", "correct horse battery staple", "Invited User", "USER@example.com")
		require.NoError(t, err)
		var principal principalRow
		require.NoError(t, tx.NewSelect().Model(&principal).Where("id = ?", id).Scan(ctx))
		assert.Equal(t, "invited", principal.LoginName)
		assert.Equal(t, "user@example.com", principal.Email)
		assert.True(t, principal.EmailVerified)
		return errors.New("rollback")
	})
	assert.Error(t, err)
	assertIdentityCreateStateEmpty(t, db)

	_, err = service.CreateInvitedPrincipal(t.Context(), db, testID("tenant"), "invalid", "short", "", "not-an-email")
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER deny_invited_principal BEFORE INSERT ON iam_principals
		WHEN NEW.login_name = 'storage-failure' BEGIN SELECT RAISE(ABORT, 'principal denied'); END`)
	require.NoError(t, err)
	err = db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, createErr := service.CreateInvitedPrincipal(ctx, tx, testID("tenant"), "storage-failure", "correct horse battery staple", "User", "storage@example.com")
		return createErr
	})
	assert.ErrorContains(t, err, "insert invited principal")
	assertIdentityCreateStateEmpty(t, db)
}

func TestCreatePendingPrincipalUsesCallerTransactionWithoutMembership(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	nextID := newTestIDGenerator()

	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	err = db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		id, createErr := CreatePendingPrincipal(ctx, tx, " USER@example.com ", "argon2id-hash", "Registered User", now, nextID)
		require.NoError(t, createErr)
		var principal principalRow
		require.NoError(t, tx.NewSelect().Model(&principal).Where("id = ?", id).Scan(ctx))
		assert.Equal(t, "user@example.com", principal.LoginName)
		assert.Equal(t, "user@example.com", principal.Email)
		assert.True(t, principal.ActivationRequired)
		assert.False(t, principal.EmailVerified)
		assert.Equal(t, now.UnixMilli(), principal.CreatedAt)
		var credential credentialRow
		require.NoError(t, tx.NewSelect().Model(&credential).Where("principal_id = ?", id).Scan(ctx))
		assert.Equal(t, "argon2id-hash", credential.PasswordHash)
		assert.Zero(t, registrationMembershipCount(t, tx, id))
		return errors.New("rollback")
	})
	assert.Error(t, err)
	assert.Zero(t, identityTableCount(t, db, "iam_principals"))
	assert.Zero(t, identityTableCount(t, db, "iam_credentials"))

	id, err := CreatePendingPrincipal(t.Context(), db, "user@example.com", "argon2id-hash", "", now, nextID)
	require.NoError(t, err)
	var principal principalRow
	require.NoError(t, db.NewSelect().Model(&principal).Where("id = ?", id).Scan(t.Context()))
	assert.Equal(t, "user@example.com", principal.DisplayName)
	assert.Zero(t, registrationMembershipCount(t, db, id))
	_, err = CreatePendingPrincipal(t.Context(), db, "user@example.com", "another-hash", "User", now, nextID)
	assert.ErrorIs(t, err, ErrLoginConflict)
	_, err = CreatePendingPrincipal(t.Context(), db, "not-an-email", "hash", "User", now, nextID)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = CreatePendingPrincipal(t.Context(), nil, "other@example.com", "hash", "User", now, nextID)
	assert.ErrorIs(t, err, ErrInvalid)

	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER deny_pending_principal BEFORE INSERT ON iam_principals
		WHEN NEW.email = 'principal-failure@example.com' BEGIN SELECT RAISE(ABORT, 'principal denied'); END`)
	require.NoError(t, err)
	err = db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, createErr := CreatePendingPrincipal(ctx, tx, "principal-failure@example.com", "hash", "User", now, nextID)
		return createErr
	})
	assert.ErrorContains(t, err, "insert pending principal")

	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER deny_pending_credential BEFORE INSERT ON iam_credentials
		BEGIN SELECT RAISE(ABORT, 'credential denied'); END`)
	require.NoError(t, err)
	err = db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, createErr := CreatePendingPrincipal(ctx, tx, "credential-failure@example.com", "hash", "User", now, nextID)
		return createErr
	})
	assert.ErrorContains(t, err, "insert pending credential")
	assert.Zero(t, identityTableCount(t, db, "iam_tenant_members"))
}

func registrationMembershipCount(t *testing.T, db bun.IDB, principalID guid.ID) int {
	t.Helper()
	count, err := db.NewSelect().Table("iam_tenant_members").Where("principal_id = ?", principalID).Count(t.Context())
	require.NoError(t, err)
	return count
}

func identityTableCount(t *testing.T, db bun.IDB, table string) int {
	t.Helper()
	count, err := db.NewSelect().Table(table).Count(t.Context())
	require.NoError(t, err)
	return count
}

func TestPrincipalLifecycleAppendsSafeVerifiableAudit(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	service := newIdentityService(db)
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "administrator", PrincipalID: testID("administrator")})

	principal, err := service.Create(ctx, testID("tenant"), "alice", "correct horse battery staple", "Alice", "alice@example.test")
	require.NoError(t, err)
	displayName, email := "Alice Updated", "updated@example.test"
	_, err = service.Update(ctx, testID("tenant"), principal.ID, &displayName, &email)
	require.NoError(t, err)
	_, err = service.SetStatus(ctx, testID("tenant"), principal.ID, "disabled")
	require.NoError(t, err)
	_, err = service.SetStatus(ctx, testID("tenant"), principal.ID, "active")
	require.NoError(t, err)

	events, total, err := auditmod.NewService(db, newTestIDGenerator()).List(ctx, auditmod.Filter{TenantID: testID("tenant"), Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(4), total)
	require.Len(t, events, 4)
	eventTypes := make([]string, 0, len(events))
	for _, event := range events {
		eventTypes = append(eventTypes, event.EventType)
		assert.Equal(t, testID("administrator"), event.PrincipalID)
		assert.Equal(t, "principal", event.TargetType)
		assert.Equal(t, principal.ID, event.TargetID)
		detail := string(event.Detail)
		assert.NotContains(t, detail, "alice@example.test")
		assert.NotContains(t, detail, "updated@example.test")
		assert.NotContains(t, detail, "correct horse battery staple")
	}
	assert.ElementsMatch(t, []string{"principal_created", "principal_updated", "principal_disabled", "principal_restored"}, eventTypes)
	integrity, err := auditmod.NewService(db, newTestIDGenerator()).Verify(ctx, testID("tenant"))
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
	assert.Equal(t, int64(4), integrity.VerifiedEvents)
	revision, err := policyx.Current(ctx, db, testID("tenant"))
	require.NoError(t, err)
	assert.Equal(t, int64(1), revision)
}

func TestPrincipalValidationAndDatabaseFailures(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	require.NoError(t, iam.Migrate(context.Background(), db))
	require.NoError(t, organization.Migrate(context.Background(), db))
	service := newIdentityService(db)

	for _, input := range []struct {
		tenant                          guid.ID
		login, password, display, email string
	}{
		{login: "alice", password: "correct horse battery staple"},
		{tenant: testID("tenant"), password: "correct horse battery staple"},
		{tenant: testID("tenant"), login: "alice", password: "short"},
		{tenant: testID("tenant"), login: "alice", password: "correct horse battery staple", display: strings.Repeat("x", 129)},
		{tenant: testID("tenant"), login: "alice", password: "correct horse battery staple", email: strings.Repeat("x", 321)},
	} {
		_, err := service.Create(t.Context(), input.tenant, input.login, input.password, input.display, input.email)
		assert.ErrorIs(t, err, ErrInvalid)
	}
	_, _, err = service.List(t.Context(), 0, "", 50, 0)
	assert.ErrorIs(t, err, ErrInvalid)
	_, _, err = service.List(t.Context(), testID("tenant"), "", 0, 0)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.Get(t.Context(), testID("tenant"), 0)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.Update(t.Context(), testID("tenant"), testID("missing"), nil, nil)
	assert.ErrorIs(t, err, ErrInvalid)
	missingDisplay := "Missing"
	_, err = service.Update(t.Context(), testID("tenant"), testID("missing"), &missingDisplay, nil)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = service.Update(t.Context(), 0, testID("missing"), &missingDisplay, nil)
	assert.ErrorIs(t, err, ErrInvalid)
	principal, err := service.Create(t.Context(), testID("tenant"), "valid", "correct horse battery staple", "Valid", "")
	require.NoError(t, err)
	_, err = service.Update(t.Context(), testID("tenant"), principal.ID, nil, nil)
	assert.ErrorIs(t, err, ErrInvalid)
	blank := " "
	_, err = service.Update(t.Context(), testID("tenant"), principal.ID, &blank, nil)
	assert.ErrorIs(t, err, ErrInvalid)
	longEmail := strings.Repeat("x", 321)
	_, err = service.Update(t.Context(), testID("tenant"), principal.ID, nil, &longEmail)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.SetStatus(t.Context(), testID("tenant"), principal.ID, "locked")
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.SetStatus(t.Context(), testID("tenant"), testID("missing"), "disabled")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = service.SetStatus(t.Context(), 0, principal.ID, "disabled")
	assert.ErrorIs(t, err, ErrInvalid)

	require.NoError(t, db.Close())
	_, _, err = service.List(t.Context(), testID("tenant"), "", 50, 0)
	assert.Error(t, err)
	_, err = service.Get(t.Context(), testID("tenant"), testID("principal"))
	assert.Error(t, err)
	_, err = service.Update(t.Context(), testID("tenant"), principal.ID, &blank, nil)
	assert.Error(t, err)
	_, err = service.SetStatus(t.Context(), testID("tenant"), principal.ID, "disabled")
	assert.Error(t, err)
	assert.Panics(t, func() {
		NewService(nil, newIdentityAuditAppender(db), iam.NewAdministratorGuard(), newTestIDGenerator())
	})
	assert.Panics(t, func() { NewService(db, nil, iam.NewAdministratorGuard(), newTestIDGenerator()) })
	assert.Panics(t, func() { NewService(db, newIdentityAuditAppender(db), nil, newTestIDGenerator()) })
	assert.True(t, isUnique(errors.New("duplicate key value")))
	assert.False(t, isUnique(errors.New("connection closed")))
}

func TestPrincipalCreationTransactionFailures(t *testing.T) {
	for name, trigger := range map[string]string{
		"principal":  `CREATE TRIGGER deny_principal_insert BEFORE INSERT ON iam_principals BEGIN SELECT RAISE(ABORT, 'principal denied'); END`,
		"credential": `CREATE TRIGGER deny_credential_insert BEFORE INSERT ON iam_credentials BEGIN SELECT RAISE(ABORT, 'credential denied'); END`,
		"membership": `CREATE TRIGGER deny_membership_insert BEFORE INSERT ON iam_tenant_members BEGIN SELECT RAISE(ABORT, 'membership denied'); END`,
		"audit":      `CREATE TRIGGER deny_audit_insert BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'principal_created' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`,
	} {
		t.Run(name, func(t *testing.T) {
			db, err := bunxtest.Memory()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, iam.Migrate(t.Context(), db))
			require.NoError(t, organization.Migrate(t.Context(), db))
			_, err = db.ExecContext(t.Context(), trigger)
			require.NoError(t, err)
			_, err = newIdentityService(db).Create(t.Context(), testID("tenant"), "alice", "correct horse battery staple", "", "")
			assert.ErrorContains(t, err, "denied")
			assertIdentityCreateStateEmpty(t, db)
		})
	}
}

func TestPrincipalCreationRollsBackWhenPolicyRevisionFails(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, dropTable(t.Context(), db, "iam_policy_revisions"))

	_, err = newIdentityService(db).Create(t.Context(), testID("tenant"), "alice", "correct horse battery staple", "", "")
	require.ErrorContains(t, err, "advance IAM policy revision")
	assertIdentityCreateStateEmpty(t, db)
}

func TestPrincipalUpdateRollsBackAllSecurityStateWhenAuditFails(t *testing.T) {
	db, service, principal := newIdentityFixture(t)
	_, err := db.NewUpdate().Table("iam_principals").Set("email_verified = ?", true).Where("id = ?", principal.ID).Exec(t.Context())
	require.NoError(t, err)
	insertIdentitySecurityState(t, db, guidString(principal.ID), "update")
	rejectIdentityAuditEvent(t, db, "principal_updated")

	displayName, email := "Changed", "changed@example.test"
	_, err = service.Update(t.Context(), testID("tenant"), principal.ID, &displayName, &email)
	require.ErrorContains(t, err, "append audit event")

	current, err := service.Get(t.Context(), testID("tenant"), principal.ID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", current.DisplayName)
	assert.Equal(t, "alice@example.test", current.Email)
	assert.True(t, current.EmailVerified)
	var memberDisplay, memberEmail string
	require.NoError(t, db.NewSelect().Table("iam_tenant_members").Column("display_name", "email").Where("tenant_id = ? AND principal_id = ?", testID("tenant"), principal.ID).Scan(t.Context(), &memberDisplay, &memberEmail))
	assert.Equal(t, "Alice", memberDisplay)
	assert.Equal(t, "alice@example.test", memberEmail)
	assertIdentitySecurityState(t, db, guidString(principal.ID), "update", 1, 0)
	assertIdentityAuditBaseline(t, db, 1)
}

func TestPrincipalDisableRollsBackRevocationWhenAuditFails(t *testing.T) {
	db, service, principal := newIdentityFixture(t)
	insertIdentitySecurityState(t, db, guidString(principal.ID), "disable")
	rejectIdentityAuditEvent(t, db, "principal_disabled")

	_, err := service.SetStatus(t.Context(), testID("tenant"), principal.ID, "disabled")
	require.ErrorContains(t, err, "append audit event")
	current, err := service.Get(t.Context(), testID("tenant"), principal.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", current.Status)
	assertIdentitySecurityState(t, db, guidString(principal.ID), "disable", 1, 0)
	assertIdentityAuditBaseline(t, db, 1)
}

func TestPrincipalRestoreRollsBackStatusWhenAuditFails(t *testing.T) {
	db, service, principal := newIdentityFixture(t)
	_, err := service.SetStatus(t.Context(), testID("tenant"), principal.ID, "disabled")
	require.NoError(t, err)
	rejectIdentityAuditEvent(t, db, "principal_restored")

	_, err = service.SetStatus(t.Context(), testID("tenant"), principal.ID, "active")
	require.ErrorContains(t, err, "append audit event")
	current, err := service.Get(t.Context(), testID("tenant"), principal.ID)
	require.NoError(t, err)
	assert.Equal(t, "disabled", current.Status)
	assertIdentityAuditBaseline(t, db, 2)
}

func TestPrincipalDisablePreservesEveryTenantAdministrator(t *testing.T) {
	db, service, principal := newIdentityFixture(t)
	repository := iam.NewRepository(db, newTestIDGenerator())
	roles := make(map[string]string, 2)
	for _, tenantID := range []string{"tenant", "tenant-b"} {
		if tenantID != "tenant" {
			_, err := repository.PutMember(t.Context(), iam.TenantMember{TenantID: testID(tenantID), PrincipalID: principal.ID, DisplayName: principal.DisplayName, Status: iam.MemberActive})
			require.NoError(t, err)
		}
		roleID := "administrator-" + tenantID
		now := time.Now().UTC().UnixMilli()
		_, err := db.ExecContext(t.Context(), `INSERT INTO iam_roles (tenant_id,id,name,description,created_at,updated_at) VALUES (?,?,?,'',?,?)`, testID(tenantID), testID(roleID), "Administrator", now, now)
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `INSERT INTO iam_role_permissions (tenant_id,role_id,permission_code,created_at) VALUES (?,?, 'tenant_administer',?)`, testID(tenantID), testID(roleID), now)
		require.NoError(t, err)
		_, err = repository.AddMember(t.Context(), testID(tenantID), testID(roleID), principal.ID)
		require.NoError(t, err)
		roles[tenantID] = roleID
	}

	_, err := service.SetStatus(t.Context(), testID("tenant"), principal.ID, "disabled")
	assert.ErrorIs(t, err, iam.ErrLastTenantAdministrator)
	current, err := service.Get(t.Context(), testID("tenant"), principal.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", current.Status)

	backup, err := service.Create(t.Context(), testID("tenant"), "backup-admin", "backup administrator password", "Backup Admin", "")
	require.NoError(t, err)
	_, err = repository.PutMember(t.Context(), iam.TenantMember{TenantID: testID("tenant-b"), PrincipalID: backup.ID, DisplayName: backup.DisplayName, Status: iam.MemberActive})
	require.NoError(t, err)
	for tenantID, roleID := range roles {
		_, err = repository.AddMember(t.Context(), testID(tenantID), testID(roleID), backup.ID)
		require.NoError(t, err)
	}

	current, err = service.SetStatus(t.Context(), testID("tenant"), principal.ID, "disabled")
	require.NoError(t, err)
	assert.Equal(t, "disabled", current.Status)
}

// TestIdentityServicePropagatesDatabaseFailures drops real tables so the driver
// produces genuine errors, exercising the failure branches the happy-path tests
// never reach.
func TestIdentityServicePropagatesDatabaseFailures(t *testing.T) {
	t.Run("principals", func(t *testing.T) {
		db, service, principal := newIdentityFixture(t)
		_, err := db.ExecContext(t.Context(), "DROP TABLE iam_principals")
		require.NoError(t, err)

		_, _, listErr := service.List(t.Context(), testID("tenant"), "", 10, 0)
		assert.Error(t, listErr)
		_, getErr := service.Get(t.Context(), testID("tenant"), principal.ID)
		assert.Error(t, getErr)
		displayName := "Renamed"
		_, updateErr := service.Update(t.Context(), testID("tenant"), principal.ID, &displayName, nil)
		assert.Error(t, updateErr)
		_, statusErr := service.SetStatus(t.Context(), testID("tenant"), principal.ID, "disabled")
		assert.Error(t, statusErr)
		_, createErr := service.Create(t.Context(), testID("tenant"), "broken", "correct horse battery staple", "Broken", "broken@example.test")
		assert.Error(t, createErr)
		_, _, ensureErr := service.EnsureExternalPrincipal(t.Context(), db, testID("tenant"), "external@example.test", "External", time.Now().UTC())
		assert.Error(t, ensureErr)
	})

	t.Run("service accounts", func(t *testing.T) {
		db, service, _ := newIdentityFixture(t)
		_, err := db.ExecContext(t.Context(), "DROP TABLE iam_service_accounts")
		require.NoError(t, err)

		_, _, listErr := service.ListServiceAccounts(t.Context(), testID("tenant"), "", 10, 0)
		assert.Error(t, listErr)
		_, getErr := service.GetServiceAccount(t.Context(), testID("tenant"), testID("missing"))
		assert.Error(t, getErr)
		_, createErr := service.CreateServiceAccount(t.Context(), testID("tenant"), "broken", "Broken", "", nil)
		assert.Error(t, createErr)
		_, replaceErr := service.ReplaceServiceAccount(t.Context(), testID("tenant"), testID("missing"), "Broken", "", "active", nil, 1)
		assert.Error(t, replaceErr)
		assert.Error(t, service.DeleteServiceAccount(t.Context(), testID("tenant"), testID("missing"), 1))
	})
}

func newIdentityFixture(t *testing.T) (*bun.DB, *Service, Principal) {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	service := newIdentityService(db)
	principal, err := service.Create(t.Context(), testID("tenant"), "alice", "correct horse battery staple", "Alice", "alice@example.test")
	require.NoError(t, err)
	return db, service, principal
}

func insertIdentitySecurityState(t *testing.T, db *bun.DB, principalID, prefix string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `INSERT INTO iam_sessions (id_hash, principal_id, created_at, last_seen_at, expires_at, absolute_expires_at, revoked_at, ip_address, user_agent) VALUES (?, ?, 1, 1, 9999999999999, 9999999999999, 0, '', '')`, prefix+"-session", principalID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_refresh_tokens (id_hash, family_id, principal_id, client_id, scope, created_at, expires_at, used_at, revoked_at) VALUES (?, ?, ?, 'client', '', 1, 9999999999999, 0, 0)`, prefix+"-refresh", prefix+"-family", principalID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_password_recovery_tokens (token_hmac, principal_id, created_at, expires_at, consumed_at) VALUES (?, ?, 1, 9999999999999, 0)`, strings.Repeat(prefix[:1], 64), principalID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_email_verification_tokens (token_hmac, principal_id, email, created_at, expires_at, consumed_at) VALUES (?, ?, 'alice@example.test', 1, 9999999999999, 0)`, strings.Repeat(prefix[len(prefix)-1:], 64), principalID)
	require.NoError(t, err)
}

func assertIdentitySecurityState(t *testing.T, db *bun.DB, principalID, prefix string, credentialVersion, changedAt int64) {
	t.Helper()
	var actualVersion, sessionRevoked, refreshRevoked, recoveryConsumed, verificationConsumed int64
	require.NoError(t, db.NewSelect().Table("iam_credentials").Column("credential_version").Where("principal_id = ?", principalID).Scan(t.Context(), &actualVersion))
	require.NoError(t, db.NewSelect().Table("iam_sessions").Column("revoked_at").Where("id_hash = ?", prefix+"-session").Scan(t.Context(), &sessionRevoked))
	require.NoError(t, db.NewSelect().Table("iam_refresh_tokens").Column("revoked_at").Where("id_hash = ?", prefix+"-refresh").Scan(t.Context(), &refreshRevoked))
	require.NoError(t, db.NewSelect().Table("iam_password_recovery_tokens").Column("consumed_at").Where("principal_id = ?", principalID).Scan(t.Context(), &recoveryConsumed))
	require.NoError(t, db.NewSelect().Table("iam_email_verification_tokens").Column("consumed_at").Where("principal_id = ?", principalID).Scan(t.Context(), &verificationConsumed))
	assert.Equal(t, credentialVersion, actualVersion)
	assert.Equal(t, changedAt, sessionRevoked)
	assert.Equal(t, changedAt, refreshRevoked)
	assert.Equal(t, changedAt, recoveryConsumed)
	assert.Equal(t, changedAt, verificationConsumed)
}

func assertIdentityCreateStateEmpty(t *testing.T, db *bun.DB) {
	t.Helper()
	for _, table := range []string{"iam_principals", "iam_credentials", "iam_tenant_members", "iam_audit_events", "iam_audit_heads"} {
		var count int
		require.NoError(t, db.NewSelect().Table(table).ColumnExpr("COUNT(*)").Scan(t.Context(), &count), table)
		assert.Zero(t, count, table)
	}
	exists, err := tableExists(t.Context(), db, "iam_policy_revisions")
	require.NoError(t, err)
	if exists {
		revision, revisionErr := policyx.Current(t.Context(), db, testID("tenant"))
		require.NoError(t, revisionErr)
		assert.Zero(t, revision)
	}
}

func assertIdentityAuditBaseline(t *testing.T, db *bun.DB, expectedEvents int64) {
	t.Helper()
	events, total, err := auditmod.NewService(db, newTestIDGenerator()).List(t.Context(), auditmod.Filter{TenantID: testID("tenant"), Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, expectedEvents, total)
	assert.Len(t, events, int(expectedEvents))
	revision, err := policyx.Current(t.Context(), db, testID("tenant"))
	require.NoError(t, err)
	assert.Equal(t, int64(1), revision)
}

func rejectIdentityAuditEvent(t *testing.T, db *bun.DB, eventType string) {
	t.Helper()
	query := fmt.Sprintf(`CREATE TRIGGER reject_identity_audit BEFORE INSERT ON iam_audit_events
		WHEN NEW.event_type = '%s' BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END`, strings.ReplaceAll(eventType, "'", "''"))
	_, err := db.ExecContext(t.Context(), query)
	require.NoError(t, err)
}

func dropTable(ctx context.Context, db *bun.DB, table string) error {
	_, err := db.ExecContext(ctx, "DROP TABLE "+table)
	return err
}

func tableExists(ctx context.Context, db *bun.DB, table string) (bool, error) {
	var count int
	err := db.NewSelect().Table("sqlite_master").ColumnExpr("COUNT(*)").Where("type = 'table' AND name = ?", table).Scan(ctx, &count)
	return count == 1, err
}

func newIdentityService(db *bun.DB) *Service {
	return NewService(db, newIdentityAuditAppender(db), iam.NewAdministratorGuard(), newTestIDGenerator())
}

func newIdentityAuditAppender(db *bun.DB) auditx.Appender {
	service := auditmod.NewService(db, newTestIDGenerator())
	return func(ctx context.Context, executor bun.IDB, event auditx.Event) error {
		_, err := service.AppendTo(ctx, executor, auditmod.EventInput{
			TenantID: event.TenantID, PrincipalID: event.PrincipalID,
			EventType: event.EventType, TargetType: event.TargetType, TargetID: event.TargetID,
			Outcome: "success", Detail: event.Detail,
		})
		return err
	}
}

func TestEnsureExternalPrincipalJIT(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	require.NoError(t, organization.Migrate(context.Background(), db))
	service := newIdentityService(db)
	ctx := context.Background()
	now := time.Now().UTC()

	id, created, err := service.EnsureExternalPrincipal(ctx, db, testID("tenant-a"), "EXT@example.com", "External User", now)
	require.NoError(t, err)
	assert.True(t, created)
	assert.NotEmpty(t, id)
	count, err := db.NewSelect().Table("iam_principals").Where("id = ?", id).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	count, err = db.NewSelect().Table("iam_tenant_members").Where("tenant_id = ? AND principal_id = ?", testID("tenant-a"), id).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	same, created, err := service.EnsureExternalPrincipal(ctx, db, testID("tenant-a"), "ext@example.com", "External User", now)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, id, same)

	local, err := service.Create(ctx, testID("tenant-a"), "local", "correct horse battery staple", "Local", "local@example.com")
	require.NoError(t, err)
	same, created, err = service.EnsureExternalPrincipal(ctx, db, testID("tenant-b"), "local@example.com", "Local", now)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, local.ID, same)
	count, err = db.NewSelect().Table("iam_tenant_members").Where("tenant_id = ? AND principal_id = ?", testID("tenant-b"), local.ID).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	disabled, err := service.Create(ctx, testID("tenant-a"), "ghost", "correct horse battery staple", "Ghost", "ghost@example.com")
	require.NoError(t, err)
	_, err = db.NewUpdate().Table("iam_principals").Set("status = ?", "disabled").Where("id = ?", disabled.ID).Exec(ctx)
	require.NoError(t, err)
	_, _, err = service.EnsureExternalPrincipal(ctx, db, testID("tenant-b"), "ghost@example.com", "Ghost", now)
	assert.ErrorIs(t, err, ErrPrincipalInactive)

	_, _, err = service.EnsureExternalPrincipal(ctx, db, 0, "ext@example.com", "X", now)
	assert.ErrorIs(t, err, ErrInvalid)
	_, _, err = service.EnsureExternalPrincipal(ctx, db, testID("tenant-a"), "not-an-email", "X", now)
	assert.ErrorIs(t, err, ErrInvalid)
	_, _, err = service.EnsureExternalPrincipal(ctx, db, testID("tenant-a"), "ext@example.com", strings.Repeat("x", 129), now)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestCreateInvitedPrincipalValidation(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	require.NoError(t, organization.Migrate(context.Background(), db))
	service := newIdentityService(db)
	ctx := context.Background()

	_, err = service.CreateInvitedPrincipal(ctx, db, testID("tenant-a"), "bob", "correct horse battery staple", "Bob", "not-an-email")
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.CreateInvitedPrincipal(ctx, db, testID("tenant-a"), "bob", "short", "Bob", "bob@example.com")
	assert.ErrorIs(t, err, ErrInvalid)
	id, err := service.CreateInvitedPrincipal(ctx, db, testID("tenant-a"), "bob", "correct horse battery staple", "Bob", "bob@example.com")
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	_, err = service.CreateInvitedPrincipal(ctx, db, testID("tenant-a"), "bob", "correct horse battery staple", "Bob", "bob@example.com")
	assert.ErrorIs(t, err, ErrLoginConflict)
}

func TestEnsureExternalPrincipalBranches(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	require.NoError(t, organization.Migrate(context.Background(), db))
	service := newIdentityService(db)
	ctx := context.Background()
	now := time.Now().UTC()

	// A principal whose login name equals the lookup email is matched directly.
	direct, err := service.Create(ctx, testID("tenant-a"), "loginmatch@example.com", "correct horse battery staple", "Login Match", "other@example.com")
	require.NoError(t, err)
	matched, created, err := service.EnsureExternalPrincipal(ctx, db, testID("tenant-b"), "loginmatch@example.com", "Login Match", now)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, direct.ID, matched)

	// An existing but disabled tenant membership blocks reuse.
	local, err := service.Create(ctx, testID("tenant-a"), "local", "correct horse battery staple", "Local", "local@example.com")
	require.NoError(t, err)
	_, _, err = service.EnsureExternalPrincipal(ctx, db, testID("tenant-b"), "local@example.com", "Local", now)
	require.NoError(t, err)
	_, err = db.NewUpdate().Table("iam_tenant_members").Set("status = ?", "disabled").Where("tenant_id = ? AND principal_id = ?", testID("tenant-b"), local.ID).Exec(ctx)
	require.NoError(t, err)
	_, _, err = service.EnsureExternalPrincipal(ctx, db, testID("tenant-b"), "local@example.com", "Local", now)
	assert.ErrorIs(t, err, ErrPrincipalInactive)
}

func TestCreateInvitedPrincipalRejectsDisplayedEmail(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	require.NoError(t, organization.Migrate(context.Background(), db))
	service := newIdentityService(db)
	_, err = service.CreateInvitedPrincipal(t.Context(), db, testID("tenant-a"), "carol", "correct horse battery staple", "Carol", "Carol Example <carol@example.com>")
	assert.ErrorIs(t, err, ErrInvalid)
}
