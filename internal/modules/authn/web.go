package authn

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/passwordx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secretx"
	"github.com/chaos-plus/chaosplus/internal/core/extension/webauthnx"
	auditmod "github.com/chaos-plus/chaosplus/internal/modules/audit"
	"github.com/uptrace/bun"
)

var (
	ErrInvalidSession  = authnext.ErrInvalidSession
	ErrSessionNotFound = authnext.ErrSessionNotFound
	ErrInvalidPassword = authnext.ErrInvalidPassword
	ErrPasswordReused  = authnext.ErrPasswordReused
	ErrCSRF            = errors.New("cross-site request rejected")
)

type principalRow struct {
	bun.BaseModel      `bun:"table:iam_principals"`
	ID                 string `bun:"id,pk"`
	LoginName          string
	Email              string
	EmailVerified      bool
	ActivationRequired bool
	DisplayName        string
	Status             string
	CreatedAt          int64
	UpdatedAt          int64
	DisabledAt         int64
}

type credentialRow struct {
	bun.BaseModel         `bun:"table:iam_credentials"`
	PrincipalID           string `bun:"principal_id,pk"`
	PasswordHash          string
	TOTPSecret            string
	MFARequired           bool
	TOTPLastUsedStep      int64
	FailedAttempts        int
	LockedUntil           int64
	PasswordChangedAt     int64
	CredentialVersion     int64
	RecoveryCooldownUntil int64
	UpdatedAt             int64
}

type passwordHistoryRow struct {
	bun.BaseModel `bun:"table:iam_password_history"`
	ID            string `bun:"id,pk"`
	PrincipalID   string
	PasswordHash  string
	CreatedAt     int64
}

type sessionRow struct {
	bun.BaseModel     `bun:"table:iam_sessions"`
	IDHash            string `bun:"id_hash,pk"`
	PrincipalID       string
	CreatedAt         int64
	AuthTime          int64
	ACR               int
	AMR               string
	LastSeenAt        int64
	ExpiresAt         int64
	AbsoluteExpiresAt int64
	RevokedAt         int64
	IPAddress         string
	UserAgent         string
}

type WebService struct {
	cfg                       authnext.Config
	web                       authnext.WebConfig
	db                        *bun.DB
	auditTrail                *auditmod.Service
	privateKey                ed25519.PrivateKey
	publicKey                 ed25519.PublicKey
	kid                       string
	mfaKey                    []byte
	passkeys                  *webauthnx.Adapter
	now                       func() time.Time
	enricher                  ClaimEnricher
	registrationCreator       RegistrationPrincipalCreator
	notificationAuthorization string
	notificationClient        *http.Client
	notificationMu            sync.Mutex
	notificationCancel        context.CancelFunc
	notificationDone          chan struct{}
}

type ClaimContext struct {
	TokenType string `json:"token_type"`
	Subject   string `json:"subject"`
	TenantID  string `json:"tenant_id,omitempty"`
	Audience  string `json:"audience"`
	Scope     string `json:"scope,omitempty"`
}

type ClaimEnricher interface {
	Enrich(context.Context, ClaimContext) (map[string]any, error)
}

type WebOption func(*WebService)

func WithClaimEnricher(enricher ClaimEnricher) WebOption {
	return func(service *WebService) { service.enricher = enricher }
}

func WithRegistrationPrincipalCreator(creator RegistrationPrincipalCreator) WebOption {
	return func(service *WebService) { service.registrationCreator = creator }
}

func NewWebService(cfg authnext.Config, db *bun.DB, options ...WebOption) (*WebService, error) {
	s := &WebService{cfg: cfg, web: cfg.Web, db: db, now: time.Now}
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	if !cfg.Enabled {
		return s, nil
	}
	if db == nil {
		return nil, errors.New("local authentication requires a writable database")
	}
	s.auditTrail = auditmod.NewService(db)
	if strings.TrimSpace(cfg.Issuer) == "" {
		return nil, errors.New("local authentication issuer is required")
	}
	s.cfg.Issuer = strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/")
	if len(s.cfg.Audience) == 0 {
		s.cfg.Audience = []string{"chaosplus-api"}
	}
	if s.cfg.AccessTokenTTL <= 0 {
		s.cfg.AccessTokenTTL = 15 * time.Minute
	}
	if s.cfg.ClockSkew <= 0 {
		s.cfg.ClockSkew = 30 * time.Second
	}
	if s.web.CookieName == "" {
		s.web.CookieName = "cp_session"
	}
	if s.web.SessionTTL <= 0 {
		s.web.SessionTTL = 8 * time.Hour
	}
	if s.web.IdleTTL <= 0 {
		s.web.IdleTTL = 30 * time.Minute
	}
	if s.web.IdleTTL > s.web.SessionTTL {
		s.web.IdleTTL = s.web.SessionTTL
	}
	if s.cfg.MFA.Issuer == "" {
		s.cfg.MFA.Issuer = "Chaosplus"
	}
	if s.cfg.MFA.EnrollmentTTL <= 0 {
		s.cfg.MFA.EnrollmentTTL = 10 * time.Minute
	}
	if s.cfg.MFA.ChallengeTTL <= 0 {
		s.cfg.MFA.ChallengeTTL = 5 * time.Minute
	}
	if s.cfg.MFA.RecoveryCodes <= 0 {
		s.cfg.MFA.RecoveryCodes = 10
	}
	if s.cfg.MFA.MaxAttempts <= 0 {
		s.cfg.MFA.MaxAttempts = 5
	}
	if s.cfg.Passkey.ChallengeTTL <= 0 {
		s.cfg.Passkey.ChallengeTTL = 5 * time.Minute
	}
	if s.cfg.Passkey.MaxCredentials <= 0 {
		s.cfg.Passkey.MaxCredentials = 10
	}
	if s.cfg.Passkey.DisplayName == "" {
		s.cfg.Passkey.DisplayName = "Chaosplus"
	}
	if s.cfg.Recovery.TokenTTL <= 0 {
		s.cfg.Recovery.TokenTTL = 15 * time.Minute
	}
	if s.cfg.Recovery.Cooldown <= 0 {
		s.cfg.Recovery.Cooldown = 24 * time.Hour
	}
	if s.cfg.EmailVerification.TokenTTL <= 0 {
		s.cfg.EmailVerification.TokenTTL = 24 * time.Hour
	}
	if s.cfg.Notification.PollInterval <= 0 {
		s.cfg.Notification.PollInterval = 5 * time.Second
	}
	if s.cfg.Notification.RequestTimeout <= 0 {
		s.cfg.Notification.RequestTimeout = 10 * time.Second
	}
	if s.cfg.Notification.MaxAttempts <= 0 {
		s.cfg.Notification.MaxAttempts = 10
	}
	if s.cfg.MFA.RecoveryCodes > 20 || s.cfg.MFA.MaxAttempts > 10 || s.cfg.MFA.EnrollmentTTL > time.Hour || s.cfg.MFA.ChallengeTTL > 15*time.Minute {
		return nil, errors.New("authn MFA configuration exceeds security limits")
	}
	if s.cfg.Passkey.Enabled && (!s.web.Enabled || s.cfg.Passkey.ChallengeTTL > 15*time.Minute || s.cfg.Passkey.MaxCredentials > 20) {
		return nil, errors.New("authn passkey configuration exceeds security limits")
	}
	if s.web.Enabled {
		encodedMFAKey, err := secretx.Resolve("authn.mfa.encryption_key", s.cfg.MFA.EncryptionKey, s.cfg.MFA.EncryptionKeyFile, 4096)
		if err != nil {
			return nil, err
		}
		s.mfaKey, err = parseMFAKey(encodedMFAKey)
		if err != nil {
			return nil, err
		}
	}
	if s.cfg.Passkey.Enabled {
		for _, origin := range s.cfg.Passkey.Origins {
			if !slices.Contains(s.web.AllowedOrigins, origin) {
				return nil, fmt.Errorf("authn passkey origin %q is not an allowed web origin", origin)
			}
		}
		var err error
		s.passkeys, err = webauthnx.New(webauthnx.Config{
			RPID: s.cfg.Passkey.RPID, DisplayName: s.cfg.Passkey.DisplayName, Origins: s.cfg.Passkey.Origins,
		})
		if err != nil {
			return nil, fmt.Errorf("configure passkeys: %w", err)
		}
	}
	if err := s.configureNotification(); err != nil {
		return nil, err
	}
	if err := s.configureRecovery(); err != nil {
		return nil, err
	}
	if err := s.configureEmailVerification(); err != nil {
		return nil, err
	}
	if err := s.configureRegistration(); err != nil {
		return nil, err
	}
	encoded, err := secretx.Resolve("authn.signing_key", cfg.SigningKey, cfg.SigningKeyFile, 4096)
	if err != nil {
		return nil, err
	}
	key, err := parseSigningKey(encoded)
	if err != nil {
		return nil, err
	}
	s.privateKey = key
	s.publicKey = key.Public().(ed25519.PublicKey)
	sum := sha256.Sum256(s.publicKey)
	s.kid = hex.EncodeToString(sum[:8])
	return s, nil
}

func (s *WebService) Enabled() bool { return s != nil && s.cfg.Enabled && s.web.Enabled }

func (s *WebService) Authenticate(ctx context.Context, authorization, cookieHeader string) (*authnext.Claims, error) {
	if strings.TrimSpace(authorization) != "" {
		claims, err := s.verifyAccessToken(authorization)
		if err != nil {
			return nil, err
		}
		if err := s.verifySubjectState(ctx, claims); err != nil {
			return nil, err
		}
		return claims, nil
	}
	if !s.Enabled() {
		return nil, authnext.ErrMissingBearer
	}
	token, err := cookieValue(cookieHeader, s.web.CookieName)
	if err != nil {
		return nil, ErrInvalidSession
	}
	now := s.now().UTC()
	var session sessionRow
	err = s.db.NewSelect().Model(&session).
		Where("id_hash = ? AND revoked_at = 0 AND expires_at > ? AND absolute_expires_at > ?", tokenHash(token), now.UnixMilli(), now.UnixMilli()).
		Scan(ctx)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: load session: %v", authnext.ErrUnavailable, err)
		}
		return nil, ErrInvalidSession
	}
	nextExpiry := now.Add(s.web.IdleTTL).UnixMilli()
	if nextExpiry > session.AbsoluteExpiresAt {
		nextExpiry = session.AbsoluteExpiresAt
	}
	_, _ = s.db.NewUpdate().Model((*sessionRow)(nil)).Set("last_seen_at = ?", now.UnixMilli()).Set("expires_at = ?", nextExpiry).Where("id_hash = ?", session.IDHash).Exec(ctx)
	claims, err := s.claims(ctx, session.PrincipalID)
	if err != nil {
		return nil, err
	}
	if session.AuthTime > 0 {
		claims.AuthTime = time.UnixMilli(session.AuthTime).UTC()
	}
	claims.ACR = session.ACR
	claims.AMR = strings.Fields(session.AMR)
	return claims, nil
}

func (s *WebService) BeginLogin(ctx context.Context, loginName, password, returnURL string) (authnext.LoginResult, error) {
	if !s.Enabled() {
		return authnext.LoginResult{}, authnext.ErrDisabled
	}
	returnURL = strings.TrimSpace(returnURL)
	if returnURL == "" {
		returnURL = s.web.PostLoginURL
	}
	if !slices.Contains(s.web.AllowedReturnURLs, returnURL) {
		return authnext.LoginResult{}, authnext.ErrReturnURL
	}
	loginName = normalizeLogin(loginName)
	var principal principalRow
	err := s.db.NewSelect().Model(&principal).Where("login_name = ?", loginName).
		Where("NOT EXISTS (SELECT 1 FROM iam_service_accounts AS service_account WHERE service_account.principal_id = principal_row.id)").Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			verifyDummy(password)
			return authnext.LoginResult{}, authnext.ErrInvalidCredentials
		}
		return authnext.LoginResult{}, fmt.Errorf("find principal: %w", err)
	}
	var credential credentialRow
	if err := s.db.NewSelect().Model(&credential).Where("principal_id = ?", principal.ID).Scan(ctx); err != nil {
		return authnext.LoginResult{}, fmt.Errorf("read credential: %w", err)
	}
	now := s.now().UTC()
	if principal.Status != "active" || principal.ActivationRequired || credential.LockedUntil > now.UnixMilli() {
		s.audit(ctx, principal.ID, "login", "denied")
		return authnext.LoginResult{}, authnext.ErrInvalidCredentials
	}
	valid, err := passwordx.Verify(credential.PasswordHash, password)
	if err != nil {
		return authnext.LoginResult{}, fmt.Errorf("verify credential: %w", err)
	}
	if !valid {
		attempts := credential.FailedAttempts + 1
		lockedUntil := int64(0)
		if attempts >= 5 {
			lockedUntil = now.Add(15 * time.Minute).UnixMilli()
			attempts = 0
		}
		_, _ = s.db.NewUpdate().Model((*credentialRow)(nil)).Set("failed_attempts = ?", attempts).Set("locked_until = ?", lockedUntil).Set("updated_at = ?", now.UnixMilli()).Where("principal_id = ?", principal.ID).Exec(ctx)
		s.audit(ctx, principal.ID, "login", "denied")
		return authnext.LoginResult{}, authnext.ErrInvalidCredentials
	}
	_, err = s.db.NewUpdate().Model((*credentialRow)(nil)).Set("failed_attempts = 0").Set("locked_until = 0").Set("updated_at = ?", now.UnixMilli()).Where("principal_id = ?", principal.ID).Exec(ctx)
	if err != nil {
		return authnext.LoginResult{}, fmt.Errorf("reset login failures: %w", err)
	}
	if credential.MFARequired {
		if credential.TOTPSecret == "" {
			return authnext.LoginResult{}, authnext.ErrAdditionalVerification
		}
		result, err := s.createLoginChallenge(ctx, principal.ID, returnURL, now)
		if err != nil {
			return authnext.LoginResult{}, err
		}
		s.audit(ctx, principal.ID, "login_password", "success")
		return result, nil
	}
	token, err := s.insertSession(ctx, s.db, principal.ID, now, authnext.Assurance{AuthTime: now, Level: 1, Methods: []string{"pwd"}})
	if err != nil {
		return authnext.LoginResult{}, err
	}
	s.audit(ctx, principal.ID, "login", "success")
	return authnext.LoginResult{Status: "authenticated", ReturnURL: returnURL, SessionID: token}, nil
}

// Login preserves the direct service contract used by non-HTTP callers. MFA
// callers must use BeginLogin and VerifyLoginMFA to complete the challenge.
func (s *WebService) Login(ctx context.Context, loginName, password, returnURL string) (string, string, error) {
	result, err := s.BeginLogin(ctx, loginName, password, returnURL)
	if err != nil {
		return "", "", err
	}
	if result.Status == "mfa_required" {
		return "", result.ReturnURL, authnext.ErrAdditionalVerification
	}
	return result.SessionID, result.ReturnURL, nil
}

func (s *WebService) ValidateCSRF(method, origin, cookieHeader, authorization string) error {
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions || method == http.MethodTrace || strings.TrimSpace(authorization) != "" {
		return nil
	}
	if _, err := cookieValue(cookieHeader, s.web.CookieName); err != nil {
		return nil
	}
	return s.ValidateLoginOrigin(origin)
}

func (s *WebService) ValidateLoginOrigin(origin string) error {
	if origin == "" || !slices.Contains(s.web.AllowedOrigins, origin) {
		return ErrCSRF
	}
	return nil
}

func (s *WebService) Logout(ctx context.Context, cookieHeader string) string {
	token, err := cookieValue(cookieHeader, s.web.CookieName)
	if err == nil && s.db != nil {
		_, _ = s.db.NewUpdate().Model((*sessionRow)(nil)).Set("revoked_at = ?", s.now().UTC().UnixMilli()).Where("id_hash = ? AND revoked_at = 0", tokenHash(token)).Exec(ctx)
	}
	return s.web.PostLogoutURL
}

func (s *WebService) ListSessions(ctx context.Context, authorization, cookieHeader string) ([]authnext.BrowserSession, error) {
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return nil, err
	}
	current := ""
	if token, cookieErr := cookieValue(cookieHeader, s.web.CookieName); cookieErr == nil {
		current = tokenHash(token)
	}
	now := s.now().UTC().UnixMilli()
	var rows []sessionRow
	if err := s.db.NewSelect().Model(&rows).
		Where("principal_id = ? AND revoked_at = 0 AND expires_at > ? AND absolute_expires_at > ?", claims.Subject, now, now).
		Order("last_seen_at DESC", "created_at DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list browser sessions: %w", err)
	}
	result := make([]authnext.BrowserSession, 0, len(rows))
	for _, row := range rows {
		result = append(result, authnext.BrowserSession{
			ID: row.IDHash, Current: row.IDHash == current,
			CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), LastSeenAt: time.UnixMilli(row.LastSeenAt).UTC(),
			ExpiresAt: time.UnixMilli(row.ExpiresAt).UTC(), AbsoluteExpiresAt: time.UnixMilli(row.AbsoluteExpiresAt).UTC(),
			IPAddress: row.IPAddress, UserAgent: row.UserAgent,
		})
	}
	return result, nil
}

func (s *WebService) RevokeSession(ctx context.Context, authorization, cookieHeader, id string) error {
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return err
	}
	if decoded, decodeErr := hex.DecodeString(id); decodeErr != nil || len(decoded) != sha256.Size {
		return ErrSessionNotFound
	}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*sessionRow)(nil)).Set("revoked_at = ?", s.now().UTC().UnixMilli()).
			Where("id_hash = ? AND principal_id = ? AND revoked_at = 0", id, claims.Subject).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrSessionNotFound
		}
		_, err = s.auditTrail.AppendTo(ctx, tx, securityAudit(claims.Subject, "session_revoke", "success"))
		return err
	})
	if err != nil {
		return fmt.Errorf("revoke browser session: %w", err)
	}
	return nil
}

func (s *WebService) LogoutAll(ctx context.Context, authorization, cookieHeader string) error {
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return err
	}
	now := s.now().UTC().UnixMilli()
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewUpdate().Model((*sessionRow)(nil)).Set("revoked_at = ?", now).Where("principal_id = ? AND revoked_at = 0", claims.Subject).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Table("iam_refresh_tokens").Set("revoked_at = ?", now).Where("principal_id = ? AND revoked_at = 0", claims.Subject).Exec(ctx); err != nil {
			return err
		}
		_, err := s.auditTrail.AppendTo(ctx, tx, securityAudit(claims.Subject, "logout_all", "success"))
		return err
	})
	if err != nil {
		return fmt.Errorf("revoke all sessions: %w", err)
	}
	return nil
}

func (s *WebService) ChangePassword(ctx context.Context, authorization, cookieHeader, currentPassword, newPassword string) error {
	if currentPassword == "" || len(newPassword) < 12 || len(newPassword) > 1024 {
		return ErrInvalidPassword
	}
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return err
	}
	var credential credentialRow
	if err := s.db.NewSelect().Model(&credential).Where("principal_id = ?", claims.Subject).Scan(ctx); err != nil {
		return fmt.Errorf("load credential: %w", err)
	}
	valid, err := passwordx.Verify(credential.PasswordHash, currentPassword)
	if err != nil {
		return fmt.Errorf("verify current password: %w", err)
	}
	if !valid {
		return authnext.ErrInvalidCredentials
	}
	var history []passwordHistoryRow
	if err := s.db.NewSelect().Model(&history).Where("principal_id = ?", claims.Subject).Order("created_at DESC").Limit(5).Scan(ctx); err != nil {
		return fmt.Errorf("load password history: %w", err)
	}
	for _, encoded := range append([]string{credential.PasswordHash}, passwordHashes(history)...) {
		reused, verifyErr := passwordx.Verify(encoded, newPassword)
		if verifyErr != nil {
			return fmt.Errorf("verify password history: %w", verifyErr)
		}
		if reused {
			return ErrPasswordReused
		}
	}
	hash, err := passwordx.Hash(newPassword)
	if err != nil {
		return err
	}
	historyID, err := randomToken(18)
	if err != nil {
		return err
	}
	now := s.now().UTC().UnixMilli()
	currentSession := ""
	if token, cookieErr := cookieValue(cookieHeader, s.web.CookieName); cookieErr == nil {
		currentSession = tokenHash(token)
	}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		row := passwordHistoryRow{ID: historyID, PrincipalID: claims.Subject, PasswordHash: credential.PasswordHash, CreatedAt: now}
		if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
			return err
		}
		var historyIDs []string
		if err := tx.NewSelect().Model((*passwordHistoryRow)(nil)).Column("id").
			Where("principal_id = ?", claims.Subject).Order("created_at DESC", "id DESC").
			Scan(ctx, &historyIDs); err != nil {
			return err
		}
		expiredHistoryIDs := historyIDs[min(5, len(historyIDs)):]
		if len(expiredHistoryIDs) > 0 {
			if _, err := tx.NewDelete().Model((*passwordHistoryRow)(nil)).Where("id IN (?)", bun.List(expiredHistoryIDs)).Exec(ctx); err != nil {
				return err
			}
		}
		result, err := tx.NewUpdate().Model((*credentialRow)(nil)).Set("password_hash = ?", hash).
			Set("password_changed_at = ?", now).Set("updated_at = ?", now).Set("failed_attempts = 0").Set("locked_until = 0").
			Where("principal_id = ? AND password_hash = ?", claims.Subject, credential.PasswordHash).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return ErrInvalidPassword
		}
		query := tx.NewUpdate().Model((*sessionRow)(nil)).Set("revoked_at = ?", now).Where("principal_id = ? AND revoked_at = 0", claims.Subject)
		if currentSession != "" {
			query = query.Where("id_hash <> ?", currentSession)
		}
		if _, err := query.Exec(ctx); err != nil {
			return err
		}
		if _, err = tx.NewUpdate().Table("iam_refresh_tokens").Set("revoked_at = ?", now).Where("principal_id = ? AND revoked_at = 0", claims.Subject).Exec(ctx); err != nil {
			return err
		}
		_, err = s.auditTrail.AppendTo(ctx, tx, securityAudit(claims.Subject, "password_change", "success"))
		return err
	})
	if err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	return nil
}

func passwordHashes(rows []passwordHistoryRow) []string {
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.PasswordHash)
	}
	return result
}

func (s *WebService) SessionCookie(token string) string {
	return (&http.Cookie{Name: s.web.CookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.web.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: int(s.web.SessionTTL.Seconds())}).String()
}

func (s *WebService) SessionCookieName() string { return s.web.CookieName }

func (s *WebService) ClearCookie() string {
	return (&http.Cookie{Name: s.web.CookieName, Path: "/", HttpOnly: true, Secure: s.web.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1}).String()
}

func (s *WebService) PostLogoutURL() string { return s.web.PostLogoutURL }

func (s *WebService) IssueAccessToken(ctx context.Context, principalID, audience, scope string) (string, int64, error) {
	return s.IssueTenantAccessToken(ctx, principalID, "", audience, scope)
}

func (s *WebService) IssueTenantAccessToken(ctx context.Context, principalID, tenantID, audience, scope string) (string, int64, error) {
	return s.IssueTenantAccessTokenWithAssurance(ctx, principalID, tenantID, audience, scope, authnext.Assurance{AuthTime: s.now().UTC(), Level: 1, Methods: []string{"pwd"}})
}

func (s *WebService) IssueTenantAccessTokenWithAssurance(ctx context.Context, principalID, tenantID, audience, scope string, assurance authnext.Assurance) (string, int64, error) {
	claims, err := s.claims(ctx, principalID)
	if err != nil {
		return "", 0, err
	}
	return s.issueSubjectToken(ctx, authnext.SubjectTypePrincipal, claims.Subject, tenantID, audience, scope, claims.CredentialVersion, claims.PreferredUsername, claims.Email, claims.EmailVerified, assurance)
}

func (s *WebService) IssueSubjectToken(subject, audience, scope, username, email string) (string, int64, error) {
	return s.issueSubjectToken(context.Background(), authnext.SubjectTypeService, subject, "", audience, scope, 0, username, email, false, authnext.Assurance{})
}

func (s *WebService) IssueTenantSubjectToken(ctx context.Context, subject, tenantID, audience, scope, username, email string) (string, int64, error) {
	return s.issueSubjectToken(ctx, authnext.SubjectTypeService, subject, tenantID, audience, scope, 0, username, email, false, authnext.Assurance{})
}

func (s *WebService) IssueTenantServiceAccountToken(ctx context.Context, subject, tenantID, audience, scope, username string, tokenVersion int64) (string, int64, error) {
	return s.issueSubjectToken(ctx, authnext.SubjectTypeServiceAccount, subject, tenantID, audience, scope, tokenVersion, username, "", false, authnext.Assurance{})
}

func (s *WebService) IssueTenantOAuthClientToken(ctx context.Context, clientID, tenantID, audience, scope, name string) (string, int64, error) {
	if err := s.verifyOAuthClientState(ctx, clientID, tenantID); err != nil {
		return "", 0, err
	}
	return s.issueSubjectToken(ctx, authnext.SubjectTypeOAuthClient, "client:"+clientID, tenantID, audience, scope, 0, name, "", false, authnext.Assurance{ClientID: clientID})
}

func (s *WebService) issueSubjectToken(ctx context.Context, subjectType, subject, tenantID, audience, scope string, credentialVersion int64, username, email string, emailVerified bool, assurance authnext.Assurance) (string, int64, error) {
	if subject == "" {
		return "", 0, authnext.ErrInvalidToken
	}
	if audience == "" {
		audience = s.cfg.Audience[0]
	}
	now := s.now().UTC()
	expires := now.Add(s.cfg.AccessTokenTTL)
	payload := map[string]any{
		"iss": s.cfg.Issuer, "sub": subject, "aud": []string{audience},
		"iat": now.Unix(), "exp": expires.Unix(), "jti": mustRandomToken(18),
		"subject_type":       subjectType,
		"preferred_username": username, "email": email, "scope": scope,
	}
	if tenantID != "" {
		payload["organization_id"] = tenantID
	}
	if subjectType == authnext.SubjectTypePrincipal || subjectType == authnext.SubjectTypeServiceAccount {
		payload["credential_version"] = credentialVersion
	}
	if subjectType == authnext.SubjectTypePrincipal {
		payload["email_verified"] = emailVerified
	}
	if !assurance.AuthTime.IsZero() {
		payload["auth_time"] = assurance.AuthTime.UTC().Unix()
	}
	if assurance.Level > 0 {
		payload["acr"] = assurance.Level
	}
	if len(assurance.Methods) > 0 {
		payload["amr"] = assurance.Methods
	}
	if assurance.ClientID != "" {
		payload["client_id"] = assurance.ClientID
	}
	if assurance.NetworkZone != "" {
		payload["network_zone"] = assurance.NetworkZone
	}
	if err := s.enrich(ctx, payload, ClaimContext{TokenType: "access_token", Subject: subject, TenantID: tenantID, Audience: audience, Scope: scope}); err != nil {
		return "", 0, err
	}
	token, err := s.sign(payload)
	return token, int64(s.cfg.AccessTokenTTL.Seconds()), err
}

func (s *WebService) IssueIDToken(ctx context.Context, principalID, clientID, nonce string) (string, error) {
	return s.IssueTenantIDToken(ctx, principalID, "", clientID, nonce)
}

func (s *WebService) IssueTenantIDToken(ctx context.Context, principalID, tenantID, clientID, nonce string) (string, error) {
	return s.IssueTenantIDTokenWithAssurance(ctx, principalID, tenantID, clientID, nonce, authnext.Assurance{AuthTime: s.now().UTC(), Level: 1, Methods: []string{"pwd"}, ClientID: clientID})
}

func (s *WebService) IssueTenantIDTokenWithAssurance(ctx context.Context, principalID, tenantID, clientID, nonce string, assurance authnext.Assurance) (string, error) {
	claims, err := s.claims(ctx, principalID)
	if err != nil {
		return "", err
	}
	now := s.now().UTC()
	authTime := int64(0)
	if !assurance.AuthTime.IsZero() {
		authTime = assurance.AuthTime.UTC().Unix()
	}
	payload := map[string]any{
		"iss": s.cfg.Issuer, "sub": claims.Subject, "aud": []string{clientID},
		"iat": now.Unix(), "exp": now.Add(s.cfg.AccessTokenTTL).Unix(), "jti": mustRandomToken(18),
		"subject_type": authnext.SubjectTypePrincipal, "credential_version": claims.CredentialVersion,
		"auth_time": authTime, "preferred_username": claims.PreferredUsername,
		"email": claims.Email, "email_verified": claims.EmailVerified,
	}
	if nonce != "" {
		payload["nonce"] = nonce
	}
	if tenantID != "" {
		payload["organization_id"] = tenantID
	}
	if assurance.Level > 0 {
		payload["acr"] = assurance.Level
	}
	if len(assurance.Methods) > 0 {
		payload["amr"] = assurance.Methods
	}
	if err := s.enrich(ctx, payload, ClaimContext{TokenType: "id_token", Subject: claims.Subject, TenantID: tenantID, Audience: clientID}); err != nil {
		return "", err
	}
	return s.sign(payload)
}

func (s *WebService) enrich(ctx context.Context, payload map[string]any, claimContext ClaimContext) error {
	if s.enricher == nil {
		return nil
	}
	claims, err := s.enricher.Enrich(ctx, claimContext)
	if err != nil {
		return fmt.Errorf("enrich token claims: %w", err)
	}
	if len(claims) > 0 {
		payload["ext"] = claims
	}
	return nil
}

func (s *WebService) JWKS() map[string]any {
	return map[string]any{"keys": []map[string]any{{
		"kty": "OKP", "crv": "Ed25519", "use": "sig", "alg": "EdDSA", "kid": s.kid,
		"x": base64.RawURLEncoding.EncodeToString(s.publicKey),
	}}}
}

func (s *WebService) Issuer() string { return s.cfg.Issuer }

func (s *WebService) sign(payload map[string]any) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "EdDSA", "kid": s.kid, "typ": "JWT"})
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signed := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
	sig := ed25519.Sign(s.privateKey, []byte(signed))
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (s *WebService) verifyAccessToken(authorization string) (*authnext.Claims, error) {
	parts := strings.Fields(authorization)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, authnext.ErrInvalidBearer
	}
	segments := strings.Split(parts[1], ".")
	if len(segments) != 3 {
		return nil, authnext.ErrInvalidToken
	}
	headerData, err := base64.RawURLEncoding.DecodeString(segments[0])
	if err != nil {
		return nil, authnext.ErrInvalidToken
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(headerData, &header) != nil || header.Alg != "EdDSA" || header.Kid != s.kid {
		return nil, authnext.ErrInvalidToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(segments[2])
	if err != nil || subtle.ConstantTimeCompare([]byte{boolByte(ed25519.Verify(s.publicKey, []byte(segments[0]+"."+segments[1]), sig))}, []byte{1}) != 1 {
		return nil, authnext.ErrInvalidToken
	}
	data, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return nil, authnext.ErrInvalidToken
	}
	var raw struct {
		Issuer            string   `json:"iss"`
		Subject           string   `json:"sub"`
		SubjectType       string   `json:"subject_type"`
		Audience          []string `json:"aud"`
		ExpiresAt         int64    `json:"exp"`
		IssuedAt          int64    `json:"iat"`
		PreferredUsername string   `json:"preferred_username"`
		Email             string   `json:"email"`
		EmailVerified     bool     `json:"email_verified"`
		OrganizationID    string   `json:"organization_id"`
		CredentialVersion int64    `json:"credential_version"`
		AuthTime          int64    `json:"auth_time"`
		ACR               int      `json:"acr"`
		AMR               []string `json:"amr"`
		ClientID          string   `json:"client_id"`
		NetworkZone       string   `json:"network_zone"`
	}
	if json.Unmarshal(data, &raw) != nil || raw.Issuer != s.cfg.Issuer || raw.Subject == "" || !validSubjectType(raw.SubjectType) || s.now().UTC().After(time.Unix(raw.ExpiresAt, 0).Add(s.cfg.ClockSkew)) {
		return nil, authnext.ErrInvalidToken
	}
	if len(s.cfg.Audience) > 0 && !hasAudience(raw.Audience, s.cfg.Audience) {
		return nil, authnext.ErrInvalidAud
	}
	return &authnext.Claims{
		Issuer: raw.Issuer, Subject: raw.Subject, SubjectType: raw.SubjectType, Audience: raw.Audience,
		ExpiresAt: time.Unix(raw.ExpiresAt, 0).UTC(), IssuedAt: time.Unix(raw.IssuedAt, 0).UTC(), AuthTime: time.Unix(raw.AuthTime, 0).UTC(),
		PreferredUsername: raw.PreferredUsername, Email: raw.Email, EmailVerified: raw.EmailVerified,
		OrganizationID: raw.OrganizationID, CredentialVersion: raw.CredentialVersion,
		ACR: raw.ACR, AMR: raw.AMR, ClientID: raw.ClientID, NetworkZone: raw.NetworkZone,
	}, nil
}

func validSubjectType(subjectType string) bool {
	return subjectType == authnext.SubjectTypePrincipal || subjectType == authnext.SubjectTypeOAuthClient || subjectType == authnext.SubjectTypeService || subjectType == authnext.SubjectTypeServiceAccount
}

func (s *WebService) verifySubjectState(ctx context.Context, claims *authnext.Claims) error {
	switch claims.SubjectType {
	case authnext.SubjectTypePrincipal:
		var state struct {
			Status            string `bun:"status"`
			CredentialVersion int64  `bun:"credential_version"`
		}
		err := s.db.NewSelect().TableExpr("iam_principals AS principal").
			ColumnExpr("principal.status AS status, credential.credential_version AS credential_version").
			Join("JOIN iam_credentials AS credential ON credential.principal_id = principal.id").
			Where("principal.id = ?", claims.Subject).Scan(ctx, &state)
		if errors.Is(err, sql.ErrNoRows) || err == nil && (state.Status != "active" || state.CredentialVersion != claims.CredentialVersion) {
			return authnext.ErrInvalidToken
		}
		if err != nil {
			return fmt.Errorf("%w: verify principal status: %v", authnext.ErrUnavailable, err)
		}
		return nil
	case authnext.SubjectTypeOAuthClient:
		clientID, ok := strings.CutPrefix(claims.Subject, "client:")
		if !ok || clientID == "" {
			return authnext.ErrInvalidToken
		}
		return s.verifyOAuthClientState(ctx, clientID, claims.OrganizationID)
	case authnext.SubjectTypeServiceAccount:
		var state struct {
			TenantID        string `bun:"tenant_id"`
			AccountStatus   string `bun:"account_status"`
			PrincipalStatus string `bun:"principal_status"`
			MemberStatus    string `bun:"member_status"`
			TenantStatus    string `bun:"tenant_status"`
			ExpiresAt       int64  `bun:"expires_at"`
			TokenVersion    int64  `bun:"token_version"`
		}
		err := s.db.NewSelect().TableExpr("iam_service_accounts AS account").
			ColumnExpr("account.owner_tenant_id AS tenant_id, account.status AS account_status, account.expires_at AS expires_at, account.token_version AS token_version").
			ColumnExpr("principal.status AS principal_status, member.status AS member_status, tenant.status AS tenant_status").
			Join("JOIN iam_principals AS principal ON principal.id = account.principal_id").
			Join("JOIN iam_tenant_members AS member ON member.tenant_id = account.owner_tenant_id AND member.user_subject = account.principal_id").
			Join("JOIN iam_tenants AS tenant ON tenant.id = account.owner_tenant_id").
			Where("account.principal_id = ?", claims.Subject).Scan(ctx, &state)
		now := s.now().UTC().UnixMilli()
		if errors.Is(err, sql.ErrNoRows) || err == nil && (claims.OrganizationID == "" || state.TenantID != claims.OrganizationID || state.AccountStatus != "active" || state.PrincipalStatus != "active" || state.MemberStatus != "active" || state.TenantStatus != "active" || state.ExpiresAt > 0 && state.ExpiresAt <= now || state.TokenVersion != claims.CredentialVersion) {
			return authnext.ErrInvalidToken
		}
		if err != nil {
			return fmt.Errorf("%w: verify service account status: %v", authnext.ErrUnavailable, err)
		}
		return nil
	case authnext.SubjectTypeService:
		return nil
	default:
		return authnext.ErrInvalidToken
	}
}

func (s *WebService) verifyOAuthClientState(ctx context.Context, clientID, tenantID string) error {
	var client struct {
		TenantID string `bun:"tenant_id"`
		Status   string `bun:"status"`
	}
	err := s.db.NewSelect().Table("iam_oauth_clients").Column("tenant_id", "status").Where("id = ?", clientID).Scan(ctx, &client)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (client.Status != "active" || client.TenantID != tenantID) {
		return authnext.ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("%w: verify OAuth client status: %v", authnext.ErrUnavailable, err)
	}
	return nil
}

func (s *WebService) claims(ctx context.Context, principalID string) (*authnext.Claims, error) {
	var principal principalRow
	if err := s.db.NewSelect().Model(&principal).Where("id = ? AND status = 'active' AND activation_required = ?", principalID, false).Scan(ctx); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: load principal: %v", authnext.ErrUnavailable, err)
		}
		return nil, ErrInvalidSession
	}
	var credentialVersion int64
	if err := s.db.NewSelect().Model((*credentialRow)(nil)).Column("credential_version").Where("principal_id = ?", principalID).Scan(ctx, &credentialVersion); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: load credential version: %v", authnext.ErrUnavailable, err)
		}
		return nil, ErrInvalidSession
	}
	return &authnext.Claims{Issuer: s.cfg.Issuer, Subject: principal.ID, SubjectType: authnext.SubjectTypePrincipal, PreferredUsername: principal.LoginName, Email: principal.Email, EmailVerified: principal.EmailVerified, CredentialVersion: credentialVersion}, nil
}

func (s *WebService) audit(ctx context.Context, principalID, eventType, outcome string) {
	if s.auditTrail == nil {
		return
	}
	_, _ = s.auditTrail.Append(ctx, securityAudit(principalID, eventType, outcome))
}

func (s *WebService) appendSecurityAudit(ctx context.Context, db bun.IDB, principalID, eventType, outcome string) error {
	if s.auditTrail == nil {
		return nil
	}
	_, err := s.auditTrail.AppendTo(ctx, db, securityAudit(principalID, eventType, outcome))
	return err
}

func securityAudit(principalID, eventType, outcome string) auditmod.EventInput {
	return auditmod.EventInput{
		TenantID: authnAuditTenant, PrincipalID: principalID,
		EventType: eventType, TargetType: "principal", TargetID: principalID, Outcome: outcome,
	}
}

type BootstrapPrincipal struct {
	LoginName   string
	Password    string
	DisplayName string
	Email       string
}

func EnsureBootstrapPrincipal(ctx context.Context, db *bun.DB, spec BootstrapPrincipal) (string, error) {
	if db == nil {
		return "", errors.New("bootstrap principal requires database")
	}
	spec.LoginName = normalizeLogin(spec.LoginName)
	spec.DisplayName = strings.TrimSpace(spec.DisplayName)
	spec.Email = strings.ToLower(strings.TrimSpace(spec.Email))
	if spec.LoginName == "" || len(spec.LoginName) > 200 || len(spec.Password) < 12 || len(spec.Password) > 1024 {
		return "", errors.New("bootstrap login name and a 12-1024 character password are required")
	}
	if spec.DisplayName == "" {
		spec.DisplayName = spec.LoginName
	}
	hash, err := passwordx.Hash(spec.Password)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().UnixMilli()
	var principalID string
	err = db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var existing principalRow
		err := tx.NewSelect().Model(&existing).Where("login_name = ?", spec.LoginName).Scan(ctx)
		if err == nil {
			principalID = existing.ID
			var credential credentialRow
			if err := tx.NewSelect().Model(&credential).Where("principal_id = ?", principalID).Scan(ctx); err != nil {
				return err
			}
			passwordMatches, err := passwordx.Verify(credential.PasswordHash, spec.Password)
			if err != nil {
				return err
			}
			emailVerified := spec.Email != ""
			securityStateChanged := !passwordMatches || existing.Email != spec.Email || existing.EmailVerified != emailVerified || existing.ActivationRequired || existing.Status != "active"
			if _, err := tx.NewUpdate().Model((*principalRow)(nil)).Set("email = ?", spec.Email).Set("email_verified = ?", spec.Email != "").Set("activation_required = ?", false).Set("display_name = ?", spec.DisplayName).Set("status = 'active'").Set("updated_at = ?", now).Where("id = ?", principalID).Exec(ctx); err != nil {
				return err
			}
			credentialUpdate := tx.NewUpdate().Model((*credentialRow)(nil)).Set("failed_attempts = 0").Set("locked_until = 0").Set("updated_at = ?", now).Where("principal_id = ?", principalID)
			if !passwordMatches {
				credentialUpdate = credentialUpdate.Set("password_hash = ?", hash).Set("password_changed_at = ?", now)
			}
			if securityStateChanged {
				credentialUpdate = credentialUpdate.Set("credential_version = credential_version + 1")
			}
			result, err := credentialUpdate.Exec(ctx)
			if err != nil {
				return err
			}
			if affected, _ := result.RowsAffected(); affected != 1 {
				return errors.New("bootstrap principal credential not found")
			}
			if !securityStateChanged {
				return nil
			}
			if _, err := tx.NewUpdate().Model((*sessionRow)(nil)).Set("revoked_at = ?", now).Where("principal_id = ? AND revoked_at = 0", principalID).Exec(ctx); err != nil {
				return err
			}
			if _, err := tx.NewUpdate().Table("iam_refresh_tokens").Set("revoked_at = ?", now).Where("principal_id = ? AND revoked_at = 0", principalID).Exec(ctx); err != nil {
				return err
			}
			if _, err = tx.NewUpdate().Model((*passwordRecoveryRow)(nil)).Set("consumed_at = ?", now).Where("principal_id = ? AND consumed_at = 0", principalID).Exec(ctx); err != nil {
				return err
			}
			_, err = tx.NewUpdate().Model((*emailVerificationRow)(nil)).Set("consumed_at = ?", now).Where("principal_id = ? AND consumed_at = 0", principalID).Exec(ctx)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		principalID, err = randomToken(18)
		if err != nil {
			return err
		}
		principal := principalRow{ID: principalID, LoginName: spec.LoginName, Email: spec.Email, EmailVerified: spec.Email != "", DisplayName: spec.DisplayName, Status: "active", CreatedAt: now, UpdatedAt: now}
		if _, err := tx.NewInsert().Model(&principal).Exec(ctx); err != nil {
			return err
		}
		credential := credentialRow{PrincipalID: principalID, PasswordHash: hash, PasswordChangedAt: now, CredentialVersion: 1, UpdatedAt: now}
		_, err = tx.NewInsert().Model(&credential).Exec(ctx)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("ensure bootstrap principal: %w", err)
	}
	return principalID, nil
}

func parseSigningKey(encoded string) (ed25519.PrivateKey, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, errors.New("authn signing key is required")
	}
	var raw []byte
	var err error
	for _, decoder := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding, base64.RawURLEncoding, base64.URLEncoding} {
		raw, err = decoder.DecodeString(encoded)
		if err == nil {
			break
		}
	}
	if err != nil {
		raw = []byte(encoded)
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), nil
	default:
		return nil, errors.New("authn signing key must be a 32-byte Ed25519 seed")
	}
}

func normalizeLogin(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func randomToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate secure token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func mustRandomToken(size int) string {
	value, _ := randomToken(size)
	return value
}

func tokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func cookieValue(header, name string) (string, error) {
	request := &http.Request{Header: http.Header{"Cookie": []string{header}}}
	cookie, err := request.Cookie(name)
	if err != nil || cookie.Value == "" {
		return "", http.ErrNoCookie
	}
	return cookie.Value, nil
}

func hasAudience(got, accepted []string) bool {
	for _, value := range got {
		if slices.Contains(accepted, value) {
			return true
		}
	}
	return false
}

func boolByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}

var (
	dummyOnce sync.Once
	dummyHash string
)

func verifyDummy(password string) {
	dummyOnce.Do(func() { dummyHash, _ = passwordx.Hash("not-a-real-password") })
	_, _ = passwordx.Verify(dummyHash, password)
}
