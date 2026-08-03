package authn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPasswordRecoveryLifecycleAndNotificationDelivery(t *testing.T) {
	deliveries := make(chan notificationPayload, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "Bearer recovery-provider", r.Header.Get("Authorization"))
		require.NotEmpty(t, r.Header.Get("Idempotency-Key"))
		var payload notificationPayload
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, r.Header.Get("Idempotency-Key"), payload.ID)
		deliveries <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	service, principalID := newLocalService(t)
	enableRecovery(t, service, server.URL)
	_, err := service.db.NewUpdate().Model((*principalRow)(nil)).Set("email_verified = ?", false).Where("id = ?", principalID).Exec(t.Context())
	require.NoError(t, err)
	require.NoError(t, service.BeginPasswordRecovery(t.Context(), "admin"))
	require.NoError(t, service.BeginPasswordRecovery(t.Context(), "admin@example.com"))
	count, err := service.db.NewSelect().Model((*passwordRecoveryRow)(nil)).Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
	_, err = EnsureBootstrapPrincipal(t.Context(), service.db, BootstrapPrincipal{
		LoginName: "admin", Password: "correct horse battery staple", DisplayName: "Administrator", Email: "admin@example.com",
	})
	require.NoError(t, err)
	var credentialVersionBeforeRecovery int64
	require.NoError(t, service.db.NewSelect().Model((*credentialRow)(nil)).Column("credential_version").Where("principal_id = ?", principalID).Scan(t.Context(), &credentialVersionBeforeRecovery))
	workerCtx := t.Context()
	require.NoError(t, service.StartNotificationWorker(workerCtx))
	t.Cleanup(func() { require.NoError(t, service.StopNotificationWorker(context.Background())) })

	session, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	oldCookie := service.SessionCookie(session)
	oldAccess, _, err := service.IssueAccessToken(t.Context(), principalID, "api", "openid")
	require.NoError(t, err)
	_, err = service.db.ExecContext(t.Context(), `INSERT INTO iam_refresh_tokens
 (id_hash, family_id, principal_id, client_id, scope, created_at, expires_at, used_at, revoked_at)
 VALUES ('recovery-refresh', 'recovery-family', ?, 'client', '', ?, ?, 0, 0)`, principalID, time.Now().UnixMilli(), time.Now().Add(time.Hour).UnixMilli())
	require.NoError(t, err)

	require.NoError(t, service.BeginPasswordRecovery(t.Context(), "missing@example.com"))
	count, err = service.db.NewSelect().Model((*passwordRecoveryRow)(nil)).Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
	require.NoError(t, service.BeginPasswordRecovery(t.Context(), " ADMIN "))

	first := receiveNotification(t, deliveries)
	require.Equal(t, passwordRecoveryNotification, first.Type)
	require.Equal(t, "admin@example.com", first.Recipient)
	parsed, err := url.Parse(first.RecoveryURL)
	require.NoError(t, err)
	recoveryToken := parsed.Query().Get("token")
	require.True(t, strings.HasPrefix(recoveryToken, recoveryTokenPrefix))
	require.GreaterOrEqual(t, len(recoveryToken), len(recoveryTokenPrefix)+43)

	var stored passwordRecoveryRow
	require.NoError(t, service.db.NewSelect().Model(&stored).Scan(t.Context()))
	assert.Len(t, stored.TokenHMAC, 64)
	assert.NotContains(t, stored.TokenHMAC, recoveryToken)
	var queued notificationOutboxRow
	requireNotificationStatus(t, service, passwordRecoveryNotification, "sent")
	require.NoError(t, service.db.NewSelect().Model(&queued).Where("kind = ?", passwordRecoveryNotification).Scan(t.Context()))
	assert.NotContains(t, queued.PayloadCiphertext, recoveryToken)
	assert.Equal(t, "sent", queued.Status)

	assert.ErrorIs(t, service.CompletePasswordRecovery(t.Context(), recoveryToken, "correct horse battery staple"), authnext.ErrPasswordReused)
	require.NoError(t, service.CompletePasswordRecovery(t.Context(), recoveryToken, "new correct horse battery staple"))
	assert.ErrorIs(t, service.CompletePasswordRecovery(t.Context(), recoveryToken, "another correct horse battery staple"), authnext.ErrInvalidRecovery)
	_, err = service.Authenticate(t.Context(), "", oldCookie)
	assert.ErrorIs(t, err, ErrInvalidSession)
	_, err = service.Authenticate(t.Context(), "Bearer "+oldAccess, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
	_, _, err = service.Login(t.Context(), "admin", "correct horse battery staple", "")
	assert.ErrorIs(t, err, authnext.ErrInvalidCredentials)
	newSession, _, err := service.Login(t.Context(), "admin", "new correct horse battery staple", "")
	require.NoError(t, err)

	var credential credentialRow
	require.NoError(t, service.db.NewSelect().Model(&credential).Where("principal_id = ?", principalID).Scan(t.Context()))
	assert.Equal(t, credentialVersionBeforeRecovery+1, credential.CredentialVersion)
	assert.Greater(t, credential.RecoveryCooldownUntil, service.now().UTC().UnixMilli())
	var refreshRevoked int64
	require.NoError(t, service.db.NewSelect().Table("iam_refresh_tokens").Column("revoked_at").Where("id_hash = 'recovery-refresh'").Scan(t.Context(), &refreshRevoked))
	assert.NotZero(t, refreshRevoked)

	newCookie := service.SessionCookie(newSession)
	_, err = service.BeginTOTPEnrollment(t.Context(), "", newCookie, "new correct horse battery staple")
	assert.ErrorIs(t, err, authnext.ErrRecoveryCooldown)
	_, err = service.BeginPasskeyRegistration(t.Context(), "", newCookie, "new correct horse battery staple")
	assert.ErrorIs(t, err, authnext.ErrRecoveryCooldown)

	second := receiveNotification(t, deliveries)
	assert.Equal(t, passwordChangedNotification, second.Type)
	auditService := auditmod.NewService(service.db)
	events, total, err := auditService.List(t.Context(), auditmod.Filter{TenantID: authnAuditTenant, EventType: "password_recovery_completed", Offset: 0, Limit: 50})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	assert.Equal(t, principalID, events[0].PrincipalID)
	integrity, err := auditService.Verify(t.Context(), authnAuditTenant)
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
}

func TestPasswordRecoveryExpiryStorageAndDeliveryFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	service, _ := newLocalService(t)
	enableRecovery(t, service, server.URL)

	assert.ErrorIs(t, service.BeginPasswordRecovery(t.Context(), ""), authnext.ErrInvalidRecovery)
	assert.ErrorIs(t, service.CompletePasswordRecovery(t.Context(), "invalid", "new correct horse battery staple"), authnext.ErrInvalidRecovery)
	require.NoError(t, service.BeginPasswordRecovery(t.Context(), "admin@example.com"))
	recoveryToken := recoveryTokenFromOutbox(t, service)
	var notification notificationOutboxRow
	require.NoError(t, service.db.NewSelect().Model(&notification).Scan(t.Context()))
	err := service.deliverNotification(t.Context(), notification.ID)
	require.ErrorContains(t, err, "status 503")
	require.NoError(t, service.db.NewSelect().Model(&notification).Where("id = ?", notification.ID).Scan(t.Context()))
	assert.Equal(t, "pending", notification.Status)
	assert.Equal(t, 1, notification.Attempts)

	var recovery passwordRecoveryRow
	require.NoError(t, service.db.NewSelect().Model(&recovery).Scan(t.Context()))
	_, err = service.db.NewUpdate().Model((*passwordRecoveryRow)(nil)).Set("expires_at = ?", service.now().Add(-time.Second).UnixMilli()).Where("token_hmac = ?", recovery.TokenHMAC).Exec(t.Context())
	require.NoError(t, err)
	assert.ErrorIs(t, service.CompletePasswordRecovery(t.Context(), recoveryToken, "new correct horse battery staple"), authnext.ErrInvalidRecovery)

	_, err = service.db.ExecContext(t.Context(), "DROP TABLE iam_notification_outbox")
	require.NoError(t, err)
	err = service.BeginPasswordRecovery(t.Context(), "admin@example.com")
	assert.ErrorContains(t, err, "store password recovery request")
}

func TestPasswordRecoveryFailsClosedOnCredentialStorageCorruption(t *testing.T) {
	t.Run("closed database", func(t *testing.T) {
		service, _ := newLocalService(t)
		enableRecovery(t, service, "http://127.0.0.1:1/notifications")
		require.NoError(t, service.db.Close())
		assert.ErrorContains(t, service.BeginPasswordRecovery(t.Context(), "admin@example.com"), "begin password recovery")
	})

	t.Run("missing credential", func(t *testing.T) {
		service, principalID := newLocalService(t)
		enableRecovery(t, service, "http://127.0.0.1:1/notifications")
		require.NoError(t, service.BeginPasswordRecovery(t.Context(), "admin@example.com"))
		token := recoveryTokenFromOutbox(t, service)
		_, err := service.db.NewDelete().Model((*credentialRow)(nil)).Where("principal_id = ?", principalID).Exec(t.Context())
		require.NoError(t, err)
		assert.ErrorContains(t, service.CompletePasswordRecovery(t.Context(), token, "new correct horse battery staple"), "load recovery credential")
	})

	t.Run("corrupt password hash", func(t *testing.T) {
		service, principalID := newLocalService(t)
		enableRecovery(t, service, "http://127.0.0.1:1/notifications")
		require.NoError(t, service.BeginPasswordRecovery(t.Context(), "admin@example.com"))
		token := recoveryTokenFromOutbox(t, service)
		_, err := service.db.NewUpdate().Model((*credentialRow)(nil)).Set("password_hash = ?", "invalid").Where("principal_id = ?", principalID).Exec(t.Context())
		require.NoError(t, err)
		assert.ErrorContains(t, service.CompletePasswordRecovery(t.Context(), token, "new correct horse battery staple"), "verify recovery password history")
	})

	t.Run("unavailable password history", func(t *testing.T) {
		service, _ := newLocalService(t)
		enableRecovery(t, service, "http://127.0.0.1:1/notifications")
		require.NoError(t, service.BeginPasswordRecovery(t.Context(), "admin@example.com"))
		token := recoveryTokenFromOutbox(t, service)
		_, err := service.db.ExecContext(t.Context(), "DROP TABLE iam_password_history")
		require.NoError(t, err)
		assert.ErrorContains(t, service.CompletePasswordRecovery(t.Context(), token, "new correct horse battery staple"), "load recovery password history")
	})
}

func TestPasswordRecoveryHistoryRetentionAndInvalidURL(t *testing.T) {
	service, principalID := newLocalService(t)
	for index, id := range []string{"history-1", "history-2", "history-3", "history-4", "history-5", "history-6"} {
		row := passwordHistoryRow{ID: id, PrincipalID: principalID, PasswordHash: "hash", CreatedAt: int64(index + 1)}
		_, err := service.db.NewInsert().Model(&row).Exec(t.Context())
		require.NoError(t, err)
	}

	require.NoError(t, trimPasswordHistory(t.Context(), service.db, principalID))
	count, err := service.db.NewSelect().Model((*passwordHistoryRow)(nil)).Where("principal_id = ?", principalID).Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 5, count)

	_, err = credentialURL("https://example.com/%", "token")
	assert.ErrorContains(t, err, "build password recovery URL")
}

func TestPasswordRecoveryCredentialIsSingleUseUnderConcurrency(t *testing.T) {
	service, _ := newLocalService(t)
	enableRecovery(t, service, "http://127.0.0.1:1/notifications")
	require.NoError(t, service.BeginPasswordRecovery(t.Context(), "admin@example.com"))
	token := recoveryTokenFromOutbox(t, service)

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, password := range []string{"first new correct horse battery staple", "second new correct horse battery staple"} {
		go func(newPassword string) {
			<-start
			results <- service.CompletePasswordRecovery(t.Context(), token, newPassword)
		}(password)
	}
	close(start)
	errorsSeen := []error{<-results, <-results}
	successes, rejected := 0, 0
	for _, err := range errorsSeen {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, authnext.ErrInvalidRecovery):
			rejected++
		default:
			t.Fatalf("unexpected concurrent recovery result: %v", err)
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, rejected)

	var recovery passwordRecoveryRow
	require.NoError(t, service.db.NewSelect().Model(&recovery).Where("token_hmac = ?", service.passwordRecoveryHMAC(token)).Scan(t.Context()))
	assert.NotZero(t, recovery.ConsumedAt)
	var credentialVersion int64
	require.NoError(t, service.db.NewSelect().Model((*credentialRow)(nil)).Column("credential_version").Scan(t.Context(), &credentialVersion))
	assert.Equal(t, int64(2), credentialVersion)
}

func TestPasswordRecoveryTransactionRollsBackOnCredentialFailure(t *testing.T) {
	service, _ := newLocalService(t)
	enableRecovery(t, service, "http://127.0.0.1:1/notifications")
	require.NoError(t, service.BeginPasswordRecovery(t.Context(), "admin@example.com"))
	token := recoveryTokenFromOutbox(t, service)
	_, err := service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_recovery_credential_update
BEFORE UPDATE OF credential_version ON iam_credentials
BEGIN SELECT RAISE(ABORT, 'credential version denied'); END`)
	require.NoError(t, err)

	err = service.CompletePasswordRecovery(t.Context(), token, "new correct horse battery staple")
	assert.ErrorContains(t, err, "credential version denied")
	var consumedAt int64
	require.NoError(t, service.db.NewSelect().Model((*passwordRecoveryRow)(nil)).Column("consumed_at").Where("token_hmac = ?", service.passwordRecoveryHMAC(token)).Scan(t.Context(), &consumedAt))
	assert.Zero(t, consumedAt)
	_, _, err = service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
}

func TestPasswordRecoveryRejectsChangedEmailState(t *testing.T) {
	service, principalID := newLocalService(t)
	enableRecovery(t, service, "http://127.0.0.1:1/notifications")
	require.NoError(t, service.BeginPasswordRecovery(t.Context(), "admin@example.com"))
	token := recoveryTokenFromOutbox(t, service)
	_, err := service.db.NewUpdate().Model((*principalRow)(nil)).Set("email_verified = ?", false).Where("id = ?", principalID).Exec(t.Context())
	require.NoError(t, err)
	assert.ErrorIs(t, service.CompletePasswordRecovery(t.Context(), token, "new correct horse battery staple"), authnext.ErrInvalidRecovery)
}

func TestRecoveryNotificationRetriesCorruptionAndStaleLocks(t *testing.T) {
	t.Run("max attempts", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(server.Close)
		service, _ := newLocalService(t)
		enableRecovery(t, service, server.URL)
		service.cfg.Notification.MaxAttempts = 2
		require.NoError(t, service.BeginPasswordRecovery(t.Context(), "admin@example.com"))
		var row notificationOutboxRow
		require.NoError(t, service.db.NewSelect().Model(&row).Where("kind = ?", passwordRecoveryNotification).Scan(t.Context()))
		assert.ErrorContains(t, service.deliverNotification(t.Context(), row.ID), "status 503")
		_, err := service.db.NewUpdate().Model((*notificationOutboxRow)(nil)).Set("available_at = 0").Where("id = ?", row.ID).Exec(t.Context())
		require.NoError(t, err)
		assert.ErrorContains(t, service.deliverNotification(t.Context(), row.ID), "status 503")
		require.NoError(t, service.db.NewSelect().Model(&row).Where("id = ?", row.ID).Scan(t.Context()))
		assert.Equal(t, "failed", row.Status)
		assert.Equal(t, 2, row.Attempts)
	})

	t.Run("corrupt ciphertext", func(t *testing.T) {
		service, _ := newLocalService(t)
		enableRecovery(t, service, "http://127.0.0.1:1/notifications")
		require.NoError(t, service.BeginPasswordRecovery(t.Context(), "admin@example.com"))
		var row notificationOutboxRow
		require.NoError(t, service.db.NewSelect().Model(&row).Where("kind = ?", passwordRecoveryNotification).Scan(t.Context()))
		_, err := service.db.NewUpdate().Model((*notificationOutboxRow)(nil)).Set("payload_ciphertext = ?", "v1.%").Where("id = ?", row.ID).Exec(t.Context())
		require.NoError(t, err)
		assert.ErrorIs(t, service.deliverNotification(t.Context(), row.ID), errInvalidAuthnCiphertext)
		require.NoError(t, service.db.NewSelect().Model(&row).Where("id = ?", row.ID).Scan(t.Context()))
		assert.Equal(t, "failed", row.Status)
		assert.Equal(t, 1, row.Attempts)
	})

	t.Run("stale delivery lock", func(t *testing.T) {
		var deliveries atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			deliveries.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(server.Close)
		service, _ := newLocalService(t)
		enableRecovery(t, service, server.URL)
		require.NoError(t, service.BeginPasswordRecovery(t.Context(), "admin@example.com"))
		var row notificationOutboxRow
		require.NoError(t, service.db.NewSelect().Model(&row).Where("kind = ?", passwordRecoveryNotification).Scan(t.Context()))
		_, err := service.db.NewUpdate().Model((*notificationOutboxRow)(nil)).Set("status = 'delivering'").Set("locked_at = 1").Where("id = ?", row.ID).Exec(t.Context())
		require.NoError(t, err)
		require.NoError(t, service.deliverPendingNotifications(t.Context()))
		require.NoError(t, service.db.NewSelect().Model(&row).Where("id = ?", row.ID).Scan(t.Context()))
		assert.Equal(t, "sent", row.Status)
		assert.Equal(t, int32(1), deliveries.Load())
	})
}

func TestPasswordRecoveryConfigurationAndWorkerLifecycle(t *testing.T) {
	disabled, err := NewWebService(authnext.Config{Recovery: authnext.RecoveryConfig{Enabled: true}}, nil)
	require.NoError(t, err)
	assert.False(t, disabled.RecoveryEnabled())
	assert.ErrorIs(t, disabled.BeginPasswordRecovery(t.Context(), "admin"), authnext.ErrRecoveryDisabled)

	service, _ := newLocalService(t)
	assert.False(t, service.RecoveryEnabled())
	assert.ErrorIs(t, service.BeginPasswordRecovery(t.Context(), "admin"), authnext.ErrRecoveryDisabled)
	assert.ErrorIs(t, service.CompletePasswordRecovery(t.Context(), "token", "new correct horse battery staple"), authnext.ErrRecoveryDisabled)
	require.NoError(t, service.StartNotificationWorker(t.Context()))
	require.NoError(t, service.StopNotificationWorker(t.Context()))

	service.cfg.Recovery = authnext.RecoveryConfig{
		Enabled: true, TokenTTL: 5 * time.Minute, Cooldown: time.Hour,
		ResetURL: "http://example.com/reset",
	}
	service.cfg.Notification = authnext.NotificationConfig{URL: "https://notify.example.com", PollInterval: time.Second, RequestTimeout: time.Second, MaxAttempts: 2}
	assert.ErrorContains(t, service.configureRecovery(), "must use HTTPS")
	service.cfg.Recovery.ResetURL = "https://app.example/reset#fragment"
	assert.ErrorContains(t, service.configureRecovery(), "without credentials or fragment")
	service.cfg.Recovery.ResetURL = "https://app.example/reset"
	service.cfg.Notification.URL = "invalid"
	assert.ErrorContains(t, service.configureNotification(), "absolute URL")

	authorizationFile := filepath.Join(t.TempDir(), "notification-authorization")
	require.NoError(t, os.WriteFile(authorizationFile, []byte("Bearer from-file\r\n"), 0o600))
	service.cfg.Notification.URL = "https://notify.example.com"
	service.cfg.Notification.Authorization = ""
	service.cfg.Notification.AuthorizationFile = authorizationFile
	require.NoError(t, service.configureNotification())
	assert.Equal(t, "Bearer from-file", service.notificationAuthorization)
	service.cfg.Notification.Authorization = "Bearer inline"
	assert.ErrorContains(t, service.configureNotification(), "mutually exclusive")
	service.cfg.Notification.Authorization = ""
	service.cfg.Notification.AuthorizationFile = filepath.Join(t.TempDir(), "missing")
	assert.ErrorContains(t, service.configureNotification(), "read authn.notification.authorization_file")
}

func enableRecovery(t *testing.T, service *WebService, notificationURL string) {
	t.Helper()
	service.cfg.Recovery = authnext.RecoveryConfig{
		Enabled: true, TokenTTL: 15 * time.Minute, Cooldown: 24 * time.Hour,
		ResetURL: "https://app.example/recover",
	}
	service.cfg.Notification = authnext.NotificationConfig{
		URL: notificationURL, Authorization: "Bearer recovery-provider",
		PollInterval: 10 * time.Millisecond, RequestTimeout: time.Second, MaxAttempts: 3,
	}
	require.NoError(t, service.configureNotification())
	require.NoError(t, service.configureRecovery())
}

func receiveNotification(t *testing.T, deliveries <-chan notificationPayload) notificationPayload {
	t.Helper()
	select {
	case payload := <-deliveries:
		return payload
	case <-time.After(3 * time.Second):
		t.Fatal("notification was not delivered")
		return notificationPayload{}
	}
}

func requireNotificationStatus(t *testing.T, service *WebService, kind, status string) {
	t.Helper()
	require.Eventually(t, func() bool {
		var current string
		return service.db.NewSelect().Model((*notificationOutboxRow)(nil)).Column("status").Where("kind = ?", kind).Scan(t.Context(), &current) == nil && current == status
	}, 2*time.Second, 10*time.Millisecond)
}

func recoveryTokenFromOutbox(t *testing.T, service *WebService) string {
	t.Helper()
	var row notificationOutboxRow
	require.NoError(t, service.db.NewSelect().Model(&row).Where("kind = ?", passwordRecoveryNotification).Order("created_at DESC", "id DESC").Limit(1).Scan(t.Context()))
	plain, err := service.decryptAuthnData("notification:v1", row.ID, row.PayloadCiphertext)
	require.NoError(t, err)
	var payload notificationPayload
	require.NoError(t, json.Unmarshal(plain, &payload))
	parsed, err := url.Parse(payload.RecoveryURL)
	require.NoError(t, err)
	token := parsed.Query().Get("token")
	require.True(t, strings.HasPrefix(token, recoveryTokenPrefix))
	return token
}
