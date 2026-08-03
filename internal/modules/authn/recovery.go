package authn

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/passwordx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secretx"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/uptrace/bun"
)

const (
	passwordRecoveryNotification = "password_recovery"
	passwordChangedNotification  = "password_changed"
	recoveryTokenPrefix          = "cpr1_"
	authnAuditTenant             = "_system"
)

type passwordRecoveryRow struct {
	bun.BaseModel `bun:"table:iam_password_recovery_tokens"`
	TokenHMAC     string `bun:"token_hmac,pk"`
	PrincipalID   string
	CreatedAt     int64
	ExpiresAt     int64
	ConsumedAt    int64
}

type notificationOutboxRow struct {
	bun.BaseModel     `bun:"table:iam_notification_outbox"`
	ID                string `bun:"id,pk"`
	Kind              string
	Recipient         string
	PayloadCiphertext string
	Status            string
	Attempts          int
	AvailableAt       int64
	LockedAt          int64
	SentAt            int64
	LastError         string
	CreatedAt         int64
	UpdatedAt         int64
}

type notificationPayload struct {
	ID              string    `json:"id"`
	Type            string    `json:"type"`
	Recipient       string    `json:"recipient"`
	RecoveryURL     string    `json:"recovery_url,omitempty"`
	VerificationURL string    `json:"verification_url,omitempty"`
	OccurredAt      time.Time `json:"occurred_at"`
	ExpiresAt       time.Time `json:"expires_at,omitempty"`
}

func (s *WebService) configureNotification() error {
	if !s.NotificationEnabled() {
		return nil
	}
	if !s.web.Enabled || s.cfg.Notification.PollInterval > time.Minute ||
		s.cfg.Notification.RequestTimeout > time.Minute || s.cfg.Notification.MaxAttempts > 20 {
		return errors.New("authn notification configuration exceeds security limits")
	}
	if err := validateAuthnURL("notification.url", s.cfg.Notification.URL); err != nil {
		return err
	}
	authorization, err := secretx.Resolve(
		"authn.notification.authorization",
		s.cfg.Notification.Authorization,
		s.cfg.Notification.AuthorizationFile,
		4096,
	)
	if err != nil {
		return err
	}
	s.notificationAuthorization = authorization
	s.notificationClient = &http.Client{Timeout: s.cfg.Notification.RequestTimeout}
	return nil
}

func (s *WebService) configureRecovery() error {
	if !s.cfg.Recovery.Enabled {
		return nil
	}
	if !s.web.Enabled || s.cfg.Recovery.TokenTTL < 5*time.Minute || s.cfg.Recovery.TokenTTL > time.Hour ||
		s.cfg.Recovery.Cooldown < time.Hour || s.cfg.Recovery.Cooldown > 7*24*time.Hour {
		return errors.New("authn recovery configuration exceeds security limits")
	}
	return validateAuthnURL("recovery.reset_url", s.cfg.Recovery.ResetURL)
}

func validateAuthnURL(name, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("authn %s must be an absolute URL without credentials or fragment", name)
	}
	if parsed.Scheme == "https" {
		return nil
	}
	host := parsed.Hostname()
	if parsed.Scheme == "http" && (strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()) {
		return nil
	}
	return fmt.Errorf("authn %s must use HTTPS except on loopback", name)
}

func (s *WebService) NotificationEnabled() bool {
	return s != nil && s.cfg.Enabled && s.web.Enabled && s.db != nil &&
		(s.cfg.Recovery.Enabled || s.cfg.EmailVerification.Enabled)
}

func (s *WebService) RecoveryEnabled() bool {
	return s != nil && s.cfg.Enabled && s.web.Enabled && s.cfg.Recovery.Enabled && s.db != nil
}

// BeginPasswordRecovery always returns the same success result for missing,
// disabled, and notification-ineligible accounts to prevent enumeration.
func (s *WebService) BeginPasswordRecovery(ctx context.Context, identifier string) error {
	if !s.RecoveryEnabled() {
		return authnext.ErrRecoveryDisabled
	}
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	if identifier == "" || len(identifier) > 320 {
		return authnext.ErrInvalidRecovery
	}
	var principal principalRow
	err := s.db.NewSelect().Model(&principal).
		Where("status = 'active' AND email_verified = ? AND (LOWER(login_name) = ? OR LOWER(email) = ?)", true, identifier, identifier).
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) || err == nil && principal.Email == "" {
		return nil
	}
	if err != nil {
		return fmt.Errorf("begin password recovery: %w", err)
	}

	secret, err := randomToken(32)
	if err != nil {
		return fmt.Errorf("generate password recovery credential: %w", err)
	}
	token := recoveryTokenPrefix + secret
	now := s.now().UTC()
	expires := now.Add(s.cfg.Recovery.TokenTTL)
	row := passwordRecoveryRow{TokenHMAC: s.passwordRecoveryHMAC(token), PrincipalID: principal.ID, CreatedAt: now.UnixMilli(), ExpiresAt: expires.UnixMilli()}
	resetURL, err := credentialURL(s.cfg.Recovery.ResetURL, token)
	if err != nil {
		return err
	}
	notification := notificationPayload{Type: passwordRecoveryNotification, Recipient: principal.Email, RecoveryURL: resetURL, OccurredAt: now, ExpiresAt: expires}

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewUpdate().Model((*passwordRecoveryRow)(nil)).Set("consumed_at = ?", now.UnixMilli()).
			Where("principal_id = ? AND consumed_at = 0", principal.ID).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
			return err
		}
		return s.enqueueNotification(ctx, tx, notification)
	})
	if err != nil {
		return fmt.Errorf("store password recovery request: %w", err)
	}
	return nil
}

func credentialURL(base, token string) (string, error) {
	target, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("build password recovery URL: %w", err)
	}
	query := target.Query()
	query.Set("token", token)
	target.RawQuery = query.Encode()
	return target.String(), nil
}

func (s *WebService) CompletePasswordRecovery(ctx context.Context, token, newPassword string) error {
	if !s.RecoveryEnabled() {
		return authnext.ErrRecoveryDisabled
	}
	if !strings.HasPrefix(token, recoveryTokenPrefix) || len(token) > 128 || len(newPassword) < 12 || len(newPassword) > 1024 {
		return authnext.ErrInvalidRecovery
	}
	now := s.now().UTC()
	computedHMAC := s.passwordRecoveryHMAC(token)
	var recovery passwordRecoveryRow
	if err := s.db.NewSelect().Model(&recovery).Where("token_hmac = ? AND consumed_at = 0 AND expires_at > ?", computedHMAC, now.UnixMilli()).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authnext.ErrInvalidRecovery
		}
		return fmt.Errorf("load password recovery credential: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(recovery.TokenHMAC), []byte(computedHMAC)) != 1 {
		return authnext.ErrInvalidRecovery
	}
	var principal principalRow
	if err := s.db.NewSelect().Model(&principal).Where("id = ? AND status = 'active' AND email_verified = ?", recovery.PrincipalID, true).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authnext.ErrInvalidRecovery
		}
		return fmt.Errorf("load recovery principal: %w", err)
	}
	var credential credentialRow
	if err := s.db.NewSelect().Model(&credential).Where("principal_id = ?", recovery.PrincipalID).Scan(ctx); err != nil {
		return fmt.Errorf("load recovery credential: %w", err)
	}
	var history []passwordHistoryRow
	if err := s.db.NewSelect().Model(&history).Where("principal_id = ?", recovery.PrincipalID).Order("created_at DESC").Limit(5).Scan(ctx); err != nil {
		return fmt.Errorf("load recovery password history: %w", err)
	}
	for _, encoded := range append([]string{credential.PasswordHash}, passwordHashes(history)...) {
		reused, err := passwordx.Verify(encoded, newPassword)
		if err != nil {
			return fmt.Errorf("verify recovery password history: %w", err)
		}
		if reused {
			return authnext.ErrPasswordReused
		}
	}
	newHash, err := passwordx.Hash(newPassword)
	if err != nil {
		return err
	}
	historyID, err := randomToken(18)
	if err != nil {
		return err
	}
	notification := notificationPayload{Type: passwordChangedNotification, Recipient: principal.Email, OccurredAt: now}
	auditService := auditmod.NewService(s.db)

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*passwordRecoveryRow)(nil)).Set("consumed_at = ?", now.UnixMilli()).
			Where("token_hmac = ? AND consumed_at = 0 AND expires_at > ?", computedHMAC, now.UnixMilli()).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return authnext.ErrInvalidRecovery
		}
		oldPassword := passwordHistoryRow{ID: historyID, PrincipalID: recovery.PrincipalID, PasswordHash: credential.PasswordHash, CreatedAt: now.UnixMilli()}
		if _, err := tx.NewInsert().Model(&oldPassword).Exec(ctx); err != nil {
			return err
		}
		if err := trimPasswordHistory(ctx, tx, recovery.PrincipalID); err != nil {
			return err
		}
		result, err = tx.NewUpdate().Model((*credentialRow)(nil)).
			Set("password_hash = ?", newHash).Set("password_changed_at = ?", now.UnixMilli()).Set("updated_at = ?", now.UnixMilli()).
			Set("failed_attempts = 0").Set("locked_until = 0").Set("credential_version = credential_version + 1").
			Set("recovery_cooldown_until = ?", now.Add(s.cfg.Recovery.Cooldown).UnixMilli()).
			Where("principal_id = ? AND password_hash = ?", recovery.PrincipalID, credential.PasswordHash).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return authnext.ErrInvalidRecovery
		}
		if _, err := tx.NewUpdate().Model((*sessionRow)(nil)).Set("revoked_at = ?", now.UnixMilli()).Where("principal_id = ? AND revoked_at = 0", recovery.PrincipalID).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Table("iam_refresh_tokens").Set("revoked_at = ?", now.UnixMilli()).Where("principal_id = ? AND revoked_at = 0", recovery.PrincipalID).Exec(ctx); err != nil {
			return err
		}
		if principal.Email != "" {
			if err := s.enqueueNotification(ctx, tx, notification); err != nil {
				return err
			}
		}
		_, err = auditService.AppendTo(ctx, tx, auditmod.EventInput{
			TenantID: authnAuditTenant, PrincipalID: recovery.PrincipalID,
			EventType: "password_recovery_completed", TargetType: "principal", TargetID: recovery.PrincipalID,
			Outcome: "success", Detail: map[string]any{"sessions_revoked": true, "credential_version_incremented": true},
		})
		return err
	})
	if err != nil {
		if errors.Is(err, authnext.ErrInvalidRecovery) {
			return err
		}
		return fmt.Errorf("complete password recovery: %w", err)
	}
	return nil
}

func trimPasswordHistory(ctx context.Context, db bun.IDB, principalID string) error {
	var historyIDs []string
	if err := db.NewSelect().Model((*passwordHistoryRow)(nil)).Column("id").Where("principal_id = ?", principalID).
		Order("created_at DESC", "id DESC").Scan(ctx, &historyIDs); err != nil {
		return err
	}
	if expired := historyIDs[min(5, len(historyIDs)):]; len(expired) > 0 {
		_, err := db.NewDelete().Model((*passwordHistoryRow)(nil)).Where("id IN (?)", bun.List(expired)).Exec(ctx)
		return err
	}
	return nil
}

func (s *WebService) passwordRecoveryHMAC(token string) string {
	mac := hmac.New(sha256.New, s.mfaPurposeKey("password-recovery:v1"))
	_, _ = mac.Write([]byte("chaosplus:password-recovery:v1\x00" + token))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *WebService) ensureRecoveryCooldownExpired(ctx context.Context, principalID string) error {
	var until int64
	if err := s.db.NewSelect().Model((*credentialRow)(nil)).Column("recovery_cooldown_until").Where("principal_id = ?", principalID).Scan(ctx, &until); err != nil {
		return fmt.Errorf("load recovery cooldown: %w", err)
	}
	if until > s.now().UTC().UnixMilli() {
		return authnext.ErrRecoveryCooldown
	}
	return nil
}

func (s *WebService) enqueueNotification(ctx context.Context, db bun.IDB, payload notificationPayload) error {
	id, err := randomToken(18)
	if err != nil {
		return err
	}
	payload.ID = id
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode notification: %w", err)
	}
	ciphertext, err := s.encryptAuthnData("notification:v1", id, encoded)
	if err != nil {
		return err
	}
	now := s.now().UTC().UnixMilli()
	row := notificationOutboxRow{
		ID: id, Kind: payload.Type, Recipient: payload.Recipient, PayloadCiphertext: ciphertext,
		Status: "pending", AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := db.NewInsert().Model(&row).Exec(ctx); err != nil {
		return fmt.Errorf("enqueue notification: %w", err)
	}
	return nil
}

func (s *WebService) StartNotificationWorker(ctx context.Context) error {
	if !s.NotificationEnabled() {
		return nil
	}
	s.notificationMu.Lock()
	defer s.notificationMu.Unlock()
	if s.notificationCancel != nil {
		return nil
	}
	workerCtx, cancel := context.WithCancel(ctx)
	s.notificationCancel = cancel
	s.notificationDone = make(chan struct{})
	go s.runNotificationWorker(workerCtx, s.notificationDone)
	return nil
}

func (s *WebService) StopNotificationWorker(ctx context.Context) error {
	s.notificationMu.Lock()
	cancel, done := s.notificationCancel, s.notificationDone
	s.notificationCancel, s.notificationDone = nil, nil
	s.notificationMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *WebService) runNotificationWorker(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	_ = s.deliverPendingNotifications(ctx)
	ticker := time.NewTicker(s.cfg.Notification.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.deliverPendingNotifications(ctx)
		}
	}
}

func (s *WebService) deliverPendingNotifications(ctx context.Context) error {
	now := s.now().UTC().UnixMilli()
	staleBefore := now - 2*s.cfg.Notification.RequestTimeout.Milliseconds()
	_, _ = s.db.NewUpdate().Model((*notificationOutboxRow)(nil)).Set("status = 'pending'").Set("locked_at = 0").
		Where("status = 'delivering' AND locked_at < ?", staleBefore).Exec(ctx)
	var ids []string
	if err := s.db.NewSelect().Model((*notificationOutboxRow)(nil)).Column("id").
		Where("status = 'pending' AND available_at <= ?", now).Order("created_at ASC").Limit(20).Scan(ctx, &ids); err != nil {
		return fmt.Errorf("list pending notifications: %w", err)
	}
	for _, id := range ids {
		if err := s.deliverNotification(ctx, id); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

func (s *WebService) deliverNotification(ctx context.Context, id string) error {
	now := s.now().UTC().UnixMilli()
	result, err := s.db.NewUpdate().Model((*notificationOutboxRow)(nil)).
		Set("status = 'delivering'").Set("locked_at = ?", now).Set("attempts = attempts + 1").Set("updated_at = ?", now).
		Where("id = ? AND status = 'pending' AND available_at <= ?", id, now).Exec(ctx)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil
	}
	var row notificationOutboxRow
	if err := s.db.NewSelect().Model(&row).Where("id = ?", id).Scan(ctx); err != nil {
		return err
	}
	payload, err := s.decryptAuthnData("notification:v1", row.ID, row.PayloadCiphertext)
	if err == nil {
		err = s.postNotification(ctx, row.ID, payload)
	}
	if err == nil {
		_, updateErr := s.db.NewUpdate().Model((*notificationOutboxRow)(nil)).
			Set("status = 'sent'").Set("sent_at = ?", now).Set("locked_at = 0").Set("last_error = ''").Set("updated_at = ?", now).
			Where("id = ? AND status = 'delivering'", id).Exec(ctx)
		return updateErr
	}
	status := "pending"
	if row.Attempts >= s.cfg.Notification.MaxAttempts || errors.Is(err, errUnsupportedAuthnCiphertext) || errors.Is(err, errInvalidAuthnCiphertext) || errors.Is(err, errDecryptAuthenticationPayload) {
		status = "failed"
	}
	retryAt := s.now().UTC().Add(time.Duration(1<<min(row.Attempts, 8)) * s.cfg.Notification.PollInterval).UnixMilli()
	message := err.Error()
	if len(message) > 512 {
		message = message[:512]
	}
	_, updateErr := s.db.NewUpdate().Model((*notificationOutboxRow)(nil)).
		Set("status = ?", status).Set("available_at = ?", retryAt).Set("locked_at = 0").Set("last_error = ?", message).Set("updated_at = ?", now).
		Where("id = ? AND status = 'delivering'", id).Exec(ctx)
	if updateErr != nil {
		return updateErr
	}
	return err
}

func (s *WebService) postNotification(ctx context.Context, id string, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Notification.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create notification request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", id)
	if s.notificationAuthorization != "" {
		req.Header.Set("Authorization", s.notificationAuthorization)
	}
	resp, err := s.notificationClient.Do(req)
	if err != nil {
		return fmt.Errorf("deliver notification: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("deliver notification: status %d", resp.StatusCode)
	}
	return nil
}
