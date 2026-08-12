package identity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	"github.com/chaos-plus/chaosplus/internal/core/extension/policyx"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestProvisionedPrincipalLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, testID("tenant")))
	service := newIdentityService(db)

	input := ProvisionedPrincipalInput{
		LoginName: " SCIM.User ", DisplayName: "", Email: "SCIM.USER@example.test",
		PasswordHash: "provisioned-password-hash", Active: true,
	}
	principal, err := service.CreateProvisionedTo(t.Context(), db, testID("tenant"), input)
	require.NoError(t, err)
	assert.Equal(t, "scim.user", principal.LoginName)
	assert.Equal(t, "scim.user", principal.DisplayName)
	assert.Equal(t, "scim.user@example.test", principal.Email)
	inactive, err := service.CreateProvisionedTo(t.Context(), db, testID("tenant"), ProvisionedPrincipalInput{
		LoginName: "inactive", PasswordHash: "provisioned-password-hash", Active: false,
	})
	require.NoError(t, err)
	var inactiveStatus string
	require.NoError(t, db.NewSelect().Table("iam_tenant_members").Column("status").Where("tenant_id = ? AND principal_id = ?", testID("tenant"), inactive.ID).Scan(t.Context(), &inactiveStatus))
	assert.Equal(t, "disabled", inactiveStatus)

	_, err = service.CreateProvisionedTo(t.Context(), db, testID("tenant"), input)
	assert.ErrorIs(t, err, ErrLoginConflict)
	_, err = service.ReplaceProvisionedTo(t.Context(), db, testID("tenant"), principal.ID, ProvisionedPrincipalInput{
		LoginName: inactive.LoginName, DisplayName: principal.DisplayName, Email: principal.Email, Active: true,
	})
	assert.ErrorIs(t, err, ErrLoginConflict)
	_, err = service.CreateProvisionedTo(t.Context(), nil, testID("tenant"), input)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.CreateProvisionedTo(t.Context(), db, 0, input)
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.CreateProvisionedTo(t.Context(), db, testID("tenant"), ProvisionedPrincipalInput{LoginName: "bad", Email: "not-an-email", PasswordHash: "hash"})
	assert.ErrorIs(t, err, ErrInvalid)

	for _, statement := range []string{
		`INSERT INTO iam_sessions (id_hash, principal_id, created_at, last_seen_at, expires_at, absolute_expires_at, revoked_at, ip_address, user_agent) VALUES ('scim-session', ?, 1, 1, 9999999999999, 9999999999999, 0, '', '')`,
		`INSERT INTO iam_refresh_tokens (id_hash, family_id, principal_id, client_id, scope, created_at, expires_at, used_at, revoked_at) VALUES ('scim-refresh', 'family', ?, 'client', '', 1, 9999999999999, 0, 0)`,
	} {
		_, err = db.ExecContext(t.Context(), statement, principal.ID)
		require.NoError(t, err)
	}
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_password_recovery_tokens (token_hmac, principal_id, created_at, expires_at, consumed_at) VALUES (?, ?, 1, 9999999999999, 0)`, strings.Repeat("c", 64), principal.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_email_verification_tokens (token_hmac, principal_id, email, created_at, expires_at, consumed_at) VALUES (?, ?, ?, 1, 9999999999999, 0)`, strings.Repeat("d", 64), principal.ID, principal.Email)
	require.NoError(t, err)
	_, err = db.NewUpdate().Table("iam_principals").Set("email_verified = ?", true).Where("id = ?", principal.ID).Exec(t.Context())
	require.NoError(t, err)

	revisionBefore, err := policyx.Current(t.Context(), db, testID("tenant"))
	require.NoError(t, err)
	updated, err := service.ReplaceProvisionedTo(t.Context(), db, testID("tenant"), principal.ID, ProvisionedPrincipalInput{
		LoginName: "scim.renamed", DisplayName: "SCIM Renamed", Email: "renamed@example.test", Active: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "scim.renamed", updated.LoginName)
	assert.False(t, updated.EmailVerified)
	revisionAfter, err := policyx.Current(t.Context(), db, testID("tenant"))
	require.NoError(t, err)
	assert.Greater(t, revisionAfter, revisionBefore)

	for table, column := range map[string]string{
		"iam_sessions": "revoked_at", "iam_refresh_tokens": "revoked_at",
		"iam_password_recovery_tokens": "consumed_at", "iam_email_verification_tokens": "consumed_at",
	} {
		var value int64
		require.NoError(t, db.NewSelect().Table(table).Column(column).Where("principal_id = ?", principal.ID).Scan(t.Context(), &value))
		assert.Positive(t, value, table)
	}

	updated, err = service.ReplaceProvisionedTo(t.Context(), db, testID("tenant"), principal.ID, ProvisionedPrincipalInput{
		LoginName: updated.LoginName, DisplayName: updated.DisplayName, Email: updated.Email, Active: false,
	})
	require.NoError(t, err)
	var memberStatus string
	require.NoError(t, db.NewSelect().Table("iam_tenant_members").Column("status").Where("tenant_id = ? AND principal_id = ?", testID("tenant"), principal.ID).Scan(t.Context(), &memberStatus))
	assert.Equal(t, "disabled", memberStatus)

	_, err = service.ReplaceProvisionedTo(t.Context(), db, testID("tenant"), principal.ID, ProvisionedPrincipalInput{
		LoginName: updated.LoginName, DisplayName: updated.DisplayName, Email: updated.Email, Active: true,
	})
	require.NoError(t, err)
	_, err = db.NewUpdate().Table("iam_principals").Set("status = 'disabled'").Where("id = ?", principal.ID).Exec(t.Context())
	require.NoError(t, err)
	_, err = service.ReplaceProvisionedTo(t.Context(), db, testID("tenant"), principal.ID, ProvisionedPrincipalInput{
		LoginName: updated.LoginName, DisplayName: updated.DisplayName, Email: updated.Email, Active: true,
	})
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.ReplaceProvisionedTo(t.Context(), nil, testID("tenant"), principal.ID, ProvisionedPrincipalInput{LoginName: "valid"})
	assert.ErrorIs(t, err, ErrInvalid)
	_, err = service.ReplaceProvisionedTo(t.Context(), db, testID("tenant"), testID("missing"), ProvisionedPrincipalInput{LoginName: "valid"})
	assert.ErrorIs(t, err, ErrNotFound)

	rollbackErr := db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, createErr := service.CreateProvisionedTo(ctx, tx, testID("tenant"), ProvisionedPrincipalInput{
			LoginName: "rolled-back", PasswordHash: "hash", Active: true,
		})
		require.NoError(t, createErr)
		return errors.New("rollback")
	})
	require.Error(t, rollbackErr)
	count, err := db.NewSelect().Table("iam_principals").Where("login_name = ?", "rolled-back").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestProvisionedPrincipalWriteFailuresRollback(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, testID("tenant")))
	service := newIdentityService(db)

	for _, test := range []struct {
		name    string
		trigger string
	}{
		{"principal", `CREATE TRIGGER reject_provisioned_principal BEFORE INSERT ON iam_principals WHEN NEW.login_name = 'fail-principal' BEGIN SELECT RAISE(ABORT, 'principal denied'); END`},
		{"credential", `CREATE TRIGGER reject_provisioned_credential BEFORE INSERT ON iam_credentials WHEN NEW.password_hash = 'fail-credential' BEGIN SELECT RAISE(ABORT, 'credential denied'); END`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, triggerErr := db.ExecContext(t.Context(), test.trigger)
			require.NoError(t, triggerErr)
			err := db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
				_, err := service.CreateProvisionedTo(ctx, tx, testID("tenant"), ProvisionedPrincipalInput{
					LoginName: "fail-" + test.name, PasswordHash: "fail-" + test.name, Active: true,
				})
				return err
			})
			require.Error(t, err)
			count, countErr := db.NewSelect().Table("iam_principals").Where("login_name = ?", "fail-"+test.name).Count(t.Context())
			require.NoError(t, countErr)
			assert.Zero(t, count)
		})
	}
}
