package authn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmailVerificationLifecycle(t *testing.T) {
	deliveries := make(chan notificationPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer verification-provider", request.Header.Get("Authorization"))
		var payload notificationPayload
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		deliveries <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	service, principalID := newLocalService(t)
	enableEmailVerification(t, service, server.URL)
	_, err := service.db.NewUpdate().Model((*principalRow)(nil)).Set("email_verified = ?", false).Where("id = ?", principalID).Exec(t.Context())
	require.NoError(t, err)
	session, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	cookie := service.SessionCookie(session)
	require.NoError(t, service.StartNotificationWorker(t.Context()))
	t.Cleanup(func() { require.NoError(t, service.StopNotificationWorker(context.Background())) })

	require.NoError(t, service.BeginEmailVerification(t.Context(), "", cookie))
	payload := receiveNotification(t, deliveries)
	assert.Equal(t, emailVerificationNotification, payload.Type)
	assert.Equal(t, "admin@example.com", payload.Recipient)
	parsed, err := url.Parse(payload.VerificationURL)
	require.NoError(t, err)
	token := parsed.Query().Get("token")
	require.True(t, strings.HasPrefix(token, emailVerificationTokenPrefix))
	require.GreaterOrEqual(t, len(token), len(emailVerificationTokenPrefix)+43)
	requireNotificationStatus(t, service, emailVerificationNotification, "sent")

	var stored emailVerificationRow
	require.NoError(t, service.db.NewSelect().Model(&stored).Where("token_hmac = ?", service.emailVerificationHMAC(token)).Scan(t.Context()))
	assert.Equal(t, "admin@example.com", stored.Email)
	assert.NotContains(t, stored.TokenHMAC, token)
	var queued notificationOutboxRow
	require.NoError(t, service.db.NewSelect().Model(&queued).Where("kind = ?", emailVerificationNotification).Scan(t.Context()))
	assert.NotContains(t, queued.PayloadCiphertext, token)
	assert.Equal(t, "sent", queued.Status)

	require.NoError(t, service.CompleteEmailVerification(t.Context(), token, ""))
	assert.ErrorIs(t, service.CompleteEmailVerification(t.Context(), token, ""), authnext.ErrInvalidEmailVerification)
	claims, err := service.Authenticate(t.Context(), "", cookie)
	require.NoError(t, err)
	assert.True(t, claims.EmailVerified)
	assert.Equal(t, "admin@example.com", claims.Email)
	auditService := auditmod.NewService(service.db)
	for _, eventType := range []string{"email_verification_requested", "email_verification_completed"} {
		events, total, auditErr := auditService.List(t.Context(), auditmod.Filter{TenantID: authnAuditTenant, EventType: eventType, Offset: 0, Limit: 50})
		require.NoError(t, auditErr)
		require.Equal(t, int64(1), total)
		assert.Equal(t, principalID, events[0].PrincipalID)
	}
	integrity, err := auditService.Verify(t.Context(), authnAuditTenant)
	require.NoError(t, err)
	assert.True(t, integrity.Valid)
}

func TestEmailVerificationStateGuards(t *testing.T) {
	t.Run("authentication is required", func(t *testing.T) {
		service, _ := newLocalService(t)
		enableEmailVerification(t, service, "https://notify.example.test/events")
		assert.ErrorIs(t, service.BeginEmailVerification(t.Context(), "", ""), authnext.ErrInvalidSession)
		serviceToken, _, err := service.IssueSubjectToken("notification-worker", "api", "", "Worker", "")
		require.NoError(t, err)
		assert.ErrorIs(t, service.BeginEmailVerification(t.Context(), "Bearer "+serviceToken, ""), authnext.ErrInvalidSession)
	})

	t.Run("already verified is idempotent", func(t *testing.T) {
		service, _ := newLocalService(t)
		enableEmailVerification(t, service, "https://notify.example.test/events")
		session, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		require.NoError(t, service.BeginEmailVerification(t.Context(), "", service.SessionCookie(session)))
		count, err := service.db.NewSelect().Model((*emailVerificationRow)(nil)).Count(t.Context())
		require.NoError(t, err)
		assert.Zero(t, count)
	})

	t.Run("primary email is required", func(t *testing.T) {
		service, principalID := newLocalService(t)
		enableEmailVerification(t, service, "https://notify.example.test/events")
		_, err := service.db.NewUpdate().Model((*principalRow)(nil)).Set("email = ''").Set("email_verified = ?", false).Where("id = ?", principalID).Exec(t.Context())
		require.NoError(t, err)
		session, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		assert.ErrorIs(t, service.BeginEmailVerification(t.Context(), "", service.SessionCookie(session)), authnext.ErrEmailRequired)
	})

	t.Run("email snapshot and expiration are enforced", func(t *testing.T) {
		service, principalID := newLocalService(t)
		enableEmailVerification(t, service, "https://notify.example.test/events")
		_, err := service.db.NewUpdate().Model((*principalRow)(nil)).Set("email_verified = ?", false).Where("id = ?", principalID).Exec(t.Context())
		require.NoError(t, err)
		session, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		cookie := service.SessionCookie(session)
		require.NoError(t, service.BeginEmailVerification(t.Context(), "", cookie))
		token := emailVerificationTokenFromOutbox(t, service)
		_, err = service.db.NewUpdate().Model((*principalRow)(nil)).Set("email = 'changed@example.com'").Where("id = ?", principalID).Exec(t.Context())
		require.NoError(t, err)
		assert.ErrorIs(t, service.CompleteEmailVerification(t.Context(), token, ""), authnext.ErrInvalidEmailVerification)

		_, err = service.db.NewUpdate().Model((*principalRow)(nil)).Set("email = 'admin@example.com'").Where("id = ?", principalID).Exec(t.Context())
		require.NoError(t, err)
		require.NoError(t, service.BeginEmailVerification(t.Context(), "", cookie))
		token = emailVerificationTokenFromOutbox(t, service)
		active, err := service.db.NewSelect().Model((*emailVerificationRow)(nil)).Where("principal_id = ? AND consumed_at = 0", principalID).Count(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 1, active)
		_, err = service.db.NewUpdate().Model((*emailVerificationRow)(nil)).Set("expires_at = ?", service.now().Add(-time.Second).UnixMilli()).Where("token_hmac = ?", service.emailVerificationHMAC(token)).Exec(t.Context())
		require.NoError(t, err)
		assert.ErrorIs(t, service.CompleteEmailVerification(t.Context(), token, ""), authnext.ErrInvalidEmailVerification)
	})

	t.Run("storage failures close the flow", func(t *testing.T) {
		service, principalID := newLocalService(t)
		enableEmailVerification(t, service, "https://notify.example.test/events")
		_, err := service.db.NewUpdate().Model((*principalRow)(nil)).Set("email_verified = ?", false).Where("id = ?", principalID).Exec(t.Context())
		require.NoError(t, err)
		session, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		_, err = service.db.NewDropTable().Model((*emailVerificationRow)(nil)).Exec(t.Context())
		require.NoError(t, err)
		assert.ErrorContains(t, service.BeginEmailVerification(t.Context(), "", service.SessionCookie(session)), "begin email verification")
	})

	t.Run("request credential failure rolls back", func(t *testing.T) {
		service, token := preparedEmailVerification(t)
		_, err := service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_verification_insert BEFORE INSERT ON iam_email_verification_tokens BEGIN SELECT RAISE(ABORT, 'verification insert denied'); END`)
		require.NoError(t, err)
		session, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		assert.ErrorContains(t, service.BeginEmailVerification(t.Context(), "", service.SessionCookie(session)), "begin email verification")
		assertVerificationActive(t, service, token)
	})

	t.Run("request notification failure rolls back", func(t *testing.T) {
		service, token := preparedEmailVerification(t)
		_, err := service.db.NewDropTable().Model((*notificationOutboxRow)(nil)).Exec(t.Context())
		require.NoError(t, err)
		session, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		assert.ErrorContains(t, service.BeginEmailVerification(t.Context(), "", service.SessionCookie(session)), "begin email verification")
		assertVerificationActive(t, service, token)
	})

	t.Run("request audit failure rolls back", func(t *testing.T) {
		service, token := preparedEmailVerification(t)
		_, err := service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_verification_request_audit BEFORE INSERT ON iam_audit_events BEGIN SELECT RAISE(ABORT, 'verification audit denied'); END`)
		require.NoError(t, err)
		session, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		assert.ErrorContains(t, service.BeginEmailVerification(t.Context(), "", service.SessionCookie(session)), "begin email verification")
		assertVerificationActive(t, service, token)
	})

	t.Run("completion storage failures roll back", func(t *testing.T) {
		service, token := preparedEmailVerification(t)
		_, err := service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_email_verified BEFORE UPDATE OF email_verified ON iam_principals BEGIN SELECT RAISE(ABORT, 'verification denied'); END`)
		require.NoError(t, err)
		assert.ErrorContains(t, service.CompleteEmailVerification(t.Context(), token, ""), "complete email verification")
		var consumedAt int64
		require.NoError(t, service.db.NewSelect().Model((*emailVerificationRow)(nil)).Column("consumed_at").Where("token_hmac = ?", service.emailVerificationHMAC(token)).Scan(t.Context(), &consumedAt))
		assert.Zero(t, consumedAt)
	})

	t.Run("missing credential storage fails closed", func(t *testing.T) {
		service, token := preparedEmailVerification(t)
		_, err := service.db.NewDropTable().Model((*emailVerificationRow)(nil)).Exec(t.Context())
		require.NoError(t, err)
		assert.ErrorContains(t, service.CompleteEmailVerification(t.Context(), token, ""), "load email verification credential")
	})

	t.Run("audit failure rolls back verification", func(t *testing.T) {
		service, token := preparedEmailVerification(t)
		_, err := service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_verification_audit BEFORE INSERT ON iam_audit_events BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
		require.NoError(t, err)
		assert.ErrorContains(t, service.CompleteEmailVerification(t.Context(), token, ""), "complete email verification")
		var verified bool
		require.NoError(t, service.db.NewSelect().Model((*principalRow)(nil)).Column("email_verified").Where("id = ?", principalIDForVerificationToken(t, service, token)).Scan(t.Context(), &verified))
		assert.False(t, verified)
	})
}

func TestEmailVerificationConcurrentSingleUse(t *testing.T) {
	service, principalID := newLocalService(t)
	enableEmailVerification(t, service, "https://notify.example.test/events")
	_, err := service.db.NewUpdate().Model((*principalRow)(nil)).Set("email_verified = ?", false).Where("id = ?", principalID).Exec(t.Context())
	require.NoError(t, err)
	session, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	require.NoError(t, service.BeginEmailVerification(t.Context(), "", service.SessionCookie(session)))
	token := emailVerificationTokenFromOutbox(t, service)

	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- service.CompleteEmailVerification(t.Context(), token, "")
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	successes, rejected := 0, 0
	for result := range results {
		switch {
		case result == nil:
			successes++
		case errors.Is(result, authnext.ErrInvalidEmailVerification):
			rejected++
		default:
			t.Fatalf("unexpected concurrent verification result: %v", result)
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, rejected)
}

func TestEmailVerificationConfiguration(t *testing.T) {
	service, _ := newLocalService(t)
	assert.False(t, service.EmailVerificationEnabled())
	assert.ErrorIs(t, service.BeginEmailVerification(t.Context(), "", ""), authnext.ErrEmailVerificationDisabled)
	assert.ErrorIs(t, service.CompleteEmailVerification(t.Context(), "token", ""), authnext.ErrEmailVerificationDisabled)
	require.NoError(t, service.StartNotificationWorker(t.Context()))
	require.NoError(t, service.StopNotificationWorker(t.Context()))

	service.cfg.EmailVerification = authnext.EmailVerificationConfig{Enabled: true, TokenTTL: time.Minute, VerifyURL: "https://app.example/verify-email"}
	assert.ErrorContains(t, service.configureEmailVerification(), "security limits")
	service.cfg.EmailVerification.TokenTTL = time.Hour
	service.cfg.EmailVerification.VerifyURL = "http://app.example/verify-email"
	assert.ErrorContains(t, service.configureEmailVerification(), "must use HTTPS")
	service.cfg.EmailVerification.VerifyURL = "https://app.example/verify-email#fragment"
	assert.ErrorContains(t, service.configureEmailVerification(), "without credentials or fragment")
}

func enableEmailVerification(t *testing.T, service *WebService, notificationURL string) {
	t.Helper()
	service.cfg.EmailVerification = authnext.EmailVerificationConfig{
		Enabled: true, TokenTTL: time.Hour, VerifyURL: "https://app.example/verify-email",
	}
	service.cfg.Notification = authnext.NotificationConfig{
		URL: notificationURL, Authorization: "Bearer verification-provider",
		PollInterval: 10 * time.Millisecond, RequestTimeout: time.Second, MaxAttempts: 3,
	}
	require.NoError(t, service.configureNotification())
	require.NoError(t, service.configureEmailVerification())
}

func emailVerificationTokenFromOutbox(t *testing.T, service *WebService) string {
	t.Helper()
	var row notificationOutboxRow
	require.NoError(t, service.db.NewSelect().Model(&row).Where("kind = ?", emailVerificationNotification).Order("created_at DESC", "id DESC").Limit(1).Scan(t.Context()))
	plain, err := service.decryptAuthnData("notification:v1", row.ID, row.PayloadCiphertext)
	require.NoError(t, err)
	var payload notificationPayload
	require.NoError(t, json.Unmarshal(plain, &payload))
	parsed, err := url.Parse(payload.VerificationURL)
	require.NoError(t, err)
	token := parsed.Query().Get("token")
	require.True(t, strings.HasPrefix(token, emailVerificationTokenPrefix))
	return token
}

func preparedEmailVerification(t *testing.T) (*WebService, string) {
	t.Helper()
	service, principalID := newLocalService(t)
	enableEmailVerification(t, service, "https://notify.example.test/events")
	_, err := service.db.NewUpdate().Model((*principalRow)(nil)).Set("email_verified = ?", false).Where("id = ?", principalID).Exec(t.Context())
	require.NoError(t, err)
	session, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	require.NoError(t, service.BeginEmailVerification(t.Context(), "", service.SessionCookie(session)))
	return service, emailVerificationTokenFromOutbox(t, service)
}

func principalIDForVerificationToken(t *testing.T, service *WebService, token string) string {
	t.Helper()
	var principalID string
	require.NoError(t, service.db.NewSelect().Model((*emailVerificationRow)(nil)).Column("principal_id").Where("token_hmac = ?", service.emailVerificationHMAC(token)).Scan(t.Context(), &principalID))
	return principalID
}

func assertVerificationActive(t *testing.T, service *WebService, token string) {
	t.Helper()
	var consumedAt int64
	require.NoError(t, service.db.NewSelect().Model((*emailVerificationRow)(nil)).Column("consumed_at").Where("token_hmac = ?", service.emailVerificationHMAC(token)).Scan(t.Context(), &consumedAt))
	assert.Zero(t, consumedAt)
}
