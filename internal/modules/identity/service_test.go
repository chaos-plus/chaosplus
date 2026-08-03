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
	principal, err := service.Create(context.Background(), "tenant-a", " Alice ", "correct horse battery staple", "Alice", "ALICE@example.com")
	require.NoError(t, err)
	assert.Equal(t, "alice", principal.LoginName)
	assert.Equal(t, "alice@example.com", principal.Email)
	assert.False(t, principal.EmailVerified)
	_, err = service.Create(context.Background(), "tenant-a", "alice", "another secure password", "Alice", "")
	assert.ErrorIs(t, err, ErrLoginConflict)
	items, total, err := service.List(context.Background(), "tenant-a", "ali", 50, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, items, 1)
	principal, err = service.Get(context.Background(), "tenant-a", principal.ID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", principal.DisplayName)

	other, err := service.Create(context.Background(), "tenant-b", "bob", "another secure password", "Bob", "")
	require.NoError(t, err)
	_, err = service.Get(context.Background(), "tenant-a", other.ID)
	assert.ErrorIs(t, err, ErrNotFound)
	items, total, err = service.List(context.Background(), "tenant-a", "", 50, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, principal.ID, items[0].ID)

	_, err = db.NewUpdate().Table("iam_principals").Set("email_verified = ?", true).Where("id = ?", principal.ID).Exec(t.Context())
	require.NoError(t, err)
	unchangedEmail := "ALICE@example.com"
	principal, err = service.Update(t.Context(), "tenant-a", principal.ID, nil, &unchangedEmail)
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
	principal, err = service.Update(t.Context(), "tenant-a", principal.ID, &displayName, &email)
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
	principal, err = service.SetStatus(t.Context(), "tenant-a", principal.ID, "disabled")
	require.NoError(t, err)
	assert.Equal(t, "disabled", principal.Status)
	var sessionRevoked, refreshRevoked int64
	require.NoError(t, db.NewSelect().Table("iam_sessions").Column("revoked_at").Where("id_hash = 'session'").Scan(t.Context(), &sessionRevoked))
	require.NoError(t, db.NewSelect().Table("iam_refresh_tokens").Column("revoked_at").Where("id_hash = 'refresh'").Scan(t.Context(), &refreshRevoked))
	assert.Positive(t, sessionRevoked)
	assert.Positive(t, refreshRevoked)
	principal, err = service.SetStatus(t.Context(), "tenant-a", principal.ID, "active")
	require.NoError(t, err)
	assert.Equal(t, "active", principal.Status)
}

func TestCreateInvitedPrincipalUsesCallerTransaction(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, "tenant"))
	service := newIdentityService(db)

	err = db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		id, err := service.CreateInvitedPrincipal(ctx, tx, "tenant", " Invited ", "correct horse battery staple", "Invited User", "USER@example.com")
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

	_, err = service.CreateInvitedPrincipal(t.Context(), db, "tenant", "invalid", "short", "", "not-an-email")
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER deny_invited_principal BEFORE INSERT ON iam_principals
		WHEN NEW.login_name = 'storage-failure' BEGIN SELECT RAISE(ABORT, 'principal denied'); END`)
	require.NoError(t, err)
	err = db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, createErr := service.CreateInvitedPrincipal(ctx, tx, "tenant", "storage-failure", "correct horse battery staple", "User", "storage@example.com")
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

	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	err = db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		id, createErr := CreatePendingPrincipal(ctx, tx, " USER@example.com ", "argon2id-hash", "Registered User", now)
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

	id, err := CreatePendingPrincipal(t.Context(), db, "user@example.com", "argon2id-hash", "", now)
	require.NoError(t, err)
	var principal principalRow
	require.NoError(t, db.NewSelect().Model(&principal).Where("id = ?", id).Scan(t.Context()))
	assert.Equal(t, "user@example.com", principal.DisplayName)
	assert.Zero(t, registrationMembershipCount(t, db, id))
	_, err = CreatePendingPrincipal(t.Context(), db, "user@example.com", "another-hash", "User", now)
	assert.ErrorIs(t, err, ErrLoginConflict)
	_, err = CreatePendingPrincipal(t.Context(), db, "not-an-email", "hash", "User", now)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = CreatePendingPrincipal(t.Context(), nil, "other@example.com", "hash", "User", now)
	assert.ErrorIs(t, err, ErrInvalid)

	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER deny_pending_principal BEFORE INSERT ON iam_principals
		WHEN NEW.email = 'principal-failure@example.com' BEGIN SELECT RAISE(ABORT, 'principal denied'); END`)
	require.NoError(t, err)
	err = db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, createErr := CreatePendingPrincipal(ctx, tx, "principal-failure@example.com", "hash", "User", now)
		return createErr
	})
	assert.ErrorContains(t, err, "insert pending principal")

	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER deny_pending_credential BEFORE INSERT ON iam_credentials
		BEGIN SELECT RAISE(ABORT, 'credential denied'); END`)
	require.NoError(t, err)
	err = db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, createErr := CreatePendingPrincipal(ctx, tx, "credential-failure@example.com", "hash", "User", now)
		return createErr
	})
	assert.ErrorContains(t, err, "insert pending credential")
	assert.Zero(t, identityTableCount(t, db, "iam_tenant_members"))
}

func registrationMembershipCount(t *testing.T, db bun.IDB, principalID string) int {
	t.Helper()
	count, err := db.NewSelect().Table("iam_tenant_members").Where("user_subject = ?", principalID).Count(t.Context())
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
	ctx := authnext.WithClaims(t.Context(), &authnext.Claims{Subject: "administrator"})

	principal, err := service.Create(ctx, "tenant", "alice", "correct horse battery staple", "Alice", "alice@example.test")
	require.NoError(t, err)
	displayName, email := "Alice Updated", "updated@example.test"
	_, err = service.Update(ctx, "tenant", principal.ID, &displayName, &email)
	require.NoError(t, err)
	_, err = service.SetStatus(ctx, "tenant", principal.ID, "disabled")
	require.NoError(t, err)
	_, err = service.SetStatus(ctx, "tenant", principal.ID, "active")
	require.NoError(t, err)

	events, total, err := auditmod.NewService(db).List(ctx, auditmod.Filter{TenantID: "tenant", Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(4), total)
	require.Len(t, events, 4)
	eventTypes := make([]string, 0, len(events))
	for _, event := range events {
		eventTypes = append(eventTypes, event.EventType)
		assert.Equal(t, "administrator", event.PrincipalID)
		assert.Equal(t, "principal", event.TargetType)
		assert.Equal(t, principal.ID, event.TargetID)
		detail := string(event.Detail)
		assert.NotContains(t, detail, "alice@example.test")
		assert.NotContains(t, detail, "updated@example.test")
		assert.NotContains(t, detail, "correct horse battery staple")
	}
	assert.ElementsMatch(t, []string{"principal_created", "principal_updated", "principal_disabled", "principal_restored"}, eventTypes)
	integrity, err := auditmod.NewService(db).Verify(ctx, "tenant")
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
	assert.Equal(t, int64(4), integrity.VerifiedEvents)
	revision, err := policyx.Current(ctx, db, "tenant")
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
		tenant, login, password, display, email string
	}{
		{login: "alice", password: "correct horse battery staple"},
		{tenant: "tenant", password: "correct horse battery staple"},
		{tenant: "tenant", login: "alice", password: "short"},
		{tenant: "tenant", login: "alice", password: "correct horse battery staple", display: strings.Repeat("x", 129)},
		{tenant: "tenant", login: "alice", password: "correct horse battery staple", email: strings.Repeat("x", 321)},
	} {
		_, err := service.Create(t.Context(), input.tenant, input.login, input.password, input.display, input.email)
		assert.ErrorIs(t, err, ErrInvalid)
	}
	_, _, err = service.List(t.Context(), "", "", 50, 0)
	assert.ErrorIs(t, err, ErrInvalid)
	_, _, err = service.List(t.Context(), "tenant", "", 0, 0)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.Get(t.Context(), "tenant", "")
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.Update(t.Context(), "tenant", "missing", nil, nil)
	assert.ErrorIs(t, err, ErrInvalid)
	missingDisplay := "Missing"
	_, err = service.Update(t.Context(), "tenant", "missing", &missingDisplay, nil)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = service.Update(t.Context(), "", "missing", &missingDisplay, nil)
	assert.ErrorIs(t, err, ErrInvalid)
	principal, err := service.Create(t.Context(), "tenant", "valid", "correct horse battery staple", "Valid", "")
	require.NoError(t, err)
	_, err = service.Update(t.Context(), "tenant", principal.ID, nil, nil)
	assert.ErrorIs(t, err, ErrInvalid)
	blank := " "
	_, err = service.Update(t.Context(), "tenant", principal.ID, &blank, nil)
	assert.ErrorIs(t, err, ErrInvalid)
	longEmail := strings.Repeat("x", 321)
	_, err = service.Update(t.Context(), "tenant", principal.ID, nil, &longEmail)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.SetStatus(t.Context(), "tenant", principal.ID, "locked")
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.SetStatus(t.Context(), "tenant", "missing", "disabled")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = service.SetStatus(t.Context(), "", principal.ID, "disabled")
	assert.ErrorIs(t, err, ErrInvalid)

	require.NoError(t, db.Close())
	_, _, err = service.List(t.Context(), "tenant", "", 50, 0)
	assert.Error(t, err)
	_, err = service.Get(t.Context(), "tenant", "principal")
	assert.Error(t, err)
	_, err = service.Update(t.Context(), "tenant", principal.ID, &blank, nil)
	assert.Error(t, err)
	_, err = service.SetStatus(t.Context(), "tenant", principal.ID, "disabled")
	assert.Error(t, err)
	assert.Panics(t, func() { NewService(nil, newIdentityAuditAppender(db), iam.NewAdministratorGuard()) })
	assert.Panics(t, func() { NewService(db, nil, iam.NewAdministratorGuard()) })
	assert.Panics(t, func() { NewService(db, newIdentityAuditAppender(db), nil) })
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
			_, err = newIdentityService(db).Create(t.Context(), "tenant", "alice", "correct horse battery staple", "", "")
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

	_, err = newIdentityService(db).Create(t.Context(), "tenant", "alice", "correct horse battery staple", "", "")
	require.ErrorContains(t, err, "advance IAM policy revision")
	assertIdentityCreateStateEmpty(t, db)
}

func TestPrincipalUpdateRollsBackAllSecurityStateWhenAuditFails(t *testing.T) {
	db, service, principal := newIdentityFixture(t)
	_, err := db.NewUpdate().Table("iam_principals").Set("email_verified = ?", true).Where("id = ?", principal.ID).Exec(t.Context())
	require.NoError(t, err)
	insertIdentitySecurityState(t, db, principal.ID, "update")
	rejectIdentityAuditEvent(t, db, "principal_updated")

	displayName, email := "Changed", "changed@example.test"
	_, err = service.Update(t.Context(), "tenant", principal.ID, &displayName, &email)
	require.ErrorContains(t, err, "append audit event")

	current, err := service.Get(t.Context(), "tenant", principal.ID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", current.DisplayName)
	assert.Equal(t, "alice@example.test", current.Email)
	assert.True(t, current.EmailVerified)
	var memberDisplay, memberEmail string
	require.NoError(t, db.NewSelect().Table("iam_tenant_members").Column("display_name", "email").Where("tenant_id = ? AND user_subject = ?", "tenant", principal.ID).Scan(t.Context(), &memberDisplay, &memberEmail))
	assert.Equal(t, "Alice", memberDisplay)
	assert.Equal(t, "alice@example.test", memberEmail)
	assertIdentitySecurityState(t, db, principal.ID, "update", 1, 0)
	assertIdentityAuditBaseline(t, db, 1)
}

func TestPrincipalDisableRollsBackRevocationWhenAuditFails(t *testing.T) {
	db, service, principal := newIdentityFixture(t)
	insertIdentitySecurityState(t, db, principal.ID, "disable")
	rejectIdentityAuditEvent(t, db, "principal_disabled")

	_, err := service.SetStatus(t.Context(), "tenant", principal.ID, "disabled")
	require.ErrorContains(t, err, "append audit event")
	current, err := service.Get(t.Context(), "tenant", principal.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", current.Status)
	assertIdentitySecurityState(t, db, principal.ID, "disable", 1, 0)
	assertIdentityAuditBaseline(t, db, 1)
}

func TestPrincipalRestoreRollsBackStatusWhenAuditFails(t *testing.T) {
	db, service, principal := newIdentityFixture(t)
	_, err := service.SetStatus(t.Context(), "tenant", principal.ID, "disabled")
	require.NoError(t, err)
	rejectIdentityAuditEvent(t, db, "principal_restored")

	_, err = service.SetStatus(t.Context(), "tenant", principal.ID, "active")
	require.ErrorContains(t, err, "append audit event")
	current, err := service.Get(t.Context(), "tenant", principal.ID)
	require.NoError(t, err)
	assert.Equal(t, "disabled", current.Status)
	assertIdentityAuditBaseline(t, db, 2)
}

func TestPrincipalDisablePreservesEveryTenantAdministrator(t *testing.T) {
	db, service, principal := newIdentityFixture(t)
	repository := iam.NewRepository(db, func() (string, error) { return "unused", nil })
	roles := make(map[string]string, 2)
	for _, tenantID := range []string{"tenant", "tenant-b"} {
		if tenantID != "tenant" {
			_, err := repository.PutMember(t.Context(), iam.TenantMember{TenantID: tenantID, Subject: principal.ID, DisplayName: principal.DisplayName, Status: iam.MemberActive})
			require.NoError(t, err)
		}
		roleID := "administrator-" + tenantID
		now := time.Now().UTC().UnixMilli()
		_, err := db.ExecContext(t.Context(), `INSERT INTO iam_roles (tenant_id,id,name,description,created_at,updated_at) VALUES (?,?,?,'',?,?)`, tenantID, roleID, "Administrator", now, now)
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `INSERT INTO iam_role_permissions (tenant_id,role_id,permission_code,created_at) VALUES (?,?, 'tenant_administer',?)`, tenantID, roleID, now)
		require.NoError(t, err)
		_, err = repository.AddMember(t.Context(), tenantID, roleID, principal.ID)
		require.NoError(t, err)
		roles[tenantID] = roleID
	}

	_, err := service.SetStatus(t.Context(), "tenant", principal.ID, "disabled")
	assert.ErrorIs(t, err, iam.ErrLastTenantAdministrator)
	current, err := service.Get(t.Context(), "tenant", principal.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", current.Status)

	backup, err := service.Create(t.Context(), "tenant", "backup-admin", "backup administrator password", "Backup Admin", "")
	require.NoError(t, err)
	_, err = repository.PutMember(t.Context(), iam.TenantMember{TenantID: "tenant-b", Subject: backup.ID, DisplayName: backup.DisplayName, Status: iam.MemberActive})
	require.NoError(t, err)
	for tenantID, roleID := range roles {
		_, err = repository.AddMember(t.Context(), tenantID, roleID, backup.ID)
		require.NoError(t, err)
	}

	current, err = service.SetStatus(t.Context(), "tenant", principal.ID, "disabled")
	require.NoError(t, err)
	assert.Equal(t, "disabled", current.Status)
}

func newIdentityFixture(t *testing.T) (*bun.DB, *Service, Principal) {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	service := newIdentityService(db)
	principal, err := service.Create(t.Context(), "tenant", "alice", "correct horse battery staple", "Alice", "alice@example.test")
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
		revision, revisionErr := policyx.Current(t.Context(), db, "tenant")
		require.NoError(t, revisionErr)
		assert.Zero(t, revision)
	}
}

func assertIdentityAuditBaseline(t *testing.T, db *bun.DB, expectedEvents int64) {
	t.Helper()
	events, total, err := auditmod.NewService(db).List(t.Context(), auditmod.Filter{TenantID: "tenant", Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, expectedEvents, total)
	assert.Len(t, events, int(expectedEvents))
	revision, err := policyx.Current(t.Context(), db, "tenant")
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
	return NewService(db, newIdentityAuditAppender(db), iam.NewAdministratorGuard())
}

func newIdentityAuditAppender(db *bun.DB) auditx.Appender {
	service := auditmod.NewService(db)
	return func(ctx context.Context, executor bun.IDB, event auditx.Event) error {
		_, err := service.AppendTo(ctx, executor, auditmod.EventInput{
			TenantID: event.TenantID, PrincipalID: event.PrincipalID,
			EventType: event.EventType, TargetType: event.TargetType, TargetID: event.TargetID,
			Outcome: "success", Detail: event.Detail,
		})
		return err
	}
}
