package authn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/passwordx"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestRegistrationLifecycle(t *testing.T) {
	deliveries := make(chan notificationPayload, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var payload notificationPayload
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		deliveries <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	service, _ := newLocalService(t)
	enableRegistration(t, service, server.URL)
	require.NoError(t, service.StartNotificationWorker(t.Context()))
	t.Cleanup(func() { require.NoError(t, service.StopNotificationWorker(context.Background())) })

	const password = "correct registration password"
	require.NoError(t, service.Register(t.Context(), " USER@example.com ", password, "Registered User"))
	require.NoError(t, service.Register(t.Context(), "user@example.com", password, "Duplicate"))

	payload := receiveNotification(t, deliveries)
	assert.Equal(t, emailVerificationNotification, payload.Type)
	assert.Equal(t, "user@example.com", payload.Recipient)
	parsed, err := url.Parse(payload.VerificationURL)
	require.NoError(t, err)
	token := parsed.Query().Get("token")
	require.NotEmpty(t, token)

	var principal principalRow
	require.NoError(t, service.db.NewSelect().Model(&principal).Where("login_name = ?", "user@example.com").Scan(t.Context()))
	assert.True(t, principal.ActivationRequired)
	assert.False(t, principal.EmailVerified)
	assert.Equal(t, "Registered User", principal.DisplayName)
	var credential credentialRow
	require.NoError(t, service.db.NewSelect().Model(&credential).Where("principal_id = ?", principal.ID).Scan(t.Context()))
	valid, err := passwordx.Verify(credential.PasswordHash, password)
	require.NoError(t, err)
	assert.True(t, valid)
	assert.NotEqual(t, password, credential.PasswordHash)
	assert.Zero(t, registrationRowCount(t, service.db, "iam_tenant_members", "user_subject = ?", principal.ID))
	assert.Equal(t, 1, registrationRowCount(t, service.db, "iam_notification_outbox", "recipient = ?", principal.Email))
	assert.ErrorIs(t, func() error {
		_, _, loginErr := service.Login(t.Context(), principal.LoginName, password, "")
		return loginErr
	}(), authnext.ErrInvalidCredentials)

	require.NoError(t, service.CompleteEmailVerification(t.Context(), token))
	require.NoError(t, service.db.NewSelect().Model(&principal).Where("id = ?", principal.ID).Scan(t.Context()))
	assert.False(t, principal.ActivationRequired)
	assert.True(t, principal.EmailVerified)
	_, _, err = service.Login(t.Context(), principal.LoginName, password, "")
	require.NoError(t, err)

	events, total, err := auditmod.NewService(service.db).List(t.Context(), auditmod.Filter{
		TenantID: authnAuditTenant, EventType: "principal_registration_requested", Offset: 0, Limit: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, events, 1)
	assert.Equal(t, principal.ID, events[0].PrincipalID)
	integrity, err := auditmod.NewService(service.db).Verify(t.Context(), authnAuditTenant)
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
}

func TestRegistrationValidationAndConfiguration(t *testing.T) {
	assert.Equal(t, authnext.Capabilities{}, (*WebService)(nil).Capabilities())
	service, _ := newLocalService(t)
	assert.False(t, service.Capabilities().Registration)
	assert.ErrorIs(t, service.Register(t.Context(), "user@example.com", "correct registration password", "User"), authnext.ErrRegistrationDisabled)

	enableRegistration(t, service, "https://notify.example.test/events")
	assert.True(t, service.Capabilities().Registration)
	assert.ErrorIs(t, service.Register(t.Context(), "not-an-email", "correct registration password", "User"), authnext.ErrInvalidRegistration)
	assert.ErrorIs(t, service.Register(t.Context(), "user@example.com", "short", "User"), authnext.ErrInvalidRegistration)

	service.cfg.EmailVerification.VerifyURL = "://invalid"
	assert.Error(t, service.Register(t.Context(), "user@example.com", "correct registration password", "User"))

	service.registrationCreator = nil
	assert.ErrorContains(t, service.configureRegistration(), "identity creator")
	service.cfg.Registration.Enabled = false
	require.NoError(t, service.configureRegistration())
}

func TestRegistrationWriteFailuresRollback(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		prepare func(*testing.T, *WebService)
	}{
		{
			name: "credential", email: "credential@example.com",
			prepare: func(t *testing.T, service *WebService) {
				_, err := service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_registration_credential BEFORE INSERT ON iam_credentials BEGIN SELECT RAISE(ABORT, 'credential denied'); END`)
				require.NoError(t, err)
			},
		},
		{
			name: "verification credential", email: "verification@example.com",
			prepare: func(t *testing.T, service *WebService) {
				_, err := service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_registration_verification BEFORE INSERT ON iam_email_verification_tokens BEGIN SELECT RAISE(ABORT, 'verification denied'); END`)
				require.NoError(t, err)
			},
		},
		{
			name: "notification", email: "notification@example.com",
			prepare: func(t *testing.T, service *WebService) {
				_, err := service.db.NewDropTable().Model((*notificationOutboxRow)(nil)).Exec(t.Context())
				require.NoError(t, err)
			},
		},
		{
			name: "audit", email: "audit@example.com",
			prepare: func(t *testing.T, service *WebService) {
				_, err := service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_registration_audit BEFORE INSERT ON iam_audit_events BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
				require.NoError(t, err)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _ := newLocalService(t)
			enableRegistration(t, service, "https://notify.example.test/events")
			test.prepare(t, service)
			assert.Error(t, service.Register(t.Context(), test.email, "correct registration password", "User"))
			assert.Zero(t, registrationRowCount(t, service.db, "iam_principals", "email = ?", test.email))
			assert.Zero(t, registrationRowCount(t, service.db, "iam_email_verification_tokens", "email = ?", test.email))
			if test.name != "notification" {
				assert.Zero(t, registrationRowCount(t, service.db, "iam_notification_outbox", "recipient = ?", test.email))
			}
		})
	}
}

func enableRegistration(t *testing.T, service *WebService, notificationURL string) {
	t.Helper()
	enableEmailVerification(t, service, notificationURL)
	service.cfg.Registration.Enabled = true
	service.registrationCreator = func(ctx context.Context, db bun.IDB, email, passwordHash, displayName string, now time.Time) (string, error) {
		id, err := identity.CreatePendingPrincipal(ctx, db, email, passwordHash, displayName, now)
		switch {
		case errors.Is(err, identity.ErrLoginConflict):
			return "", authnext.ErrRegistrationConflict
		case errors.Is(err, identity.ErrInvalid):
			return "", authnext.ErrInvalidRegistration
		default:
			return id, err
		}
	}
	require.NoError(t, service.configureRegistration())
}

func registrationRowCount(t *testing.T, db *bun.DB, table, condition string, args ...any) int {
	t.Helper()
	count, err := db.NewSelect().Table(table).Where(condition, args...).Count(t.Context())
	require.NoError(t, err)
	return count
}
