package authn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/webauthnx"
	"github.com/uptrace/bun"
)

const (
	passkeyRegistration = "registration"
	passkeyLogin        = "login"
	passkeyCipher       = "passkey:v1"
)

type passkeyUserRow struct {
	bun.BaseModel `bun:"table:iam_passkey_users"`
	PrincipalID   string `bun:"principal_id,pk"`
	UserHandle    string
	CreatedAt     int64
}

type passkeyRow struct {
	bun.BaseModel        `bun:"table:iam_passkeys"`
	IDHash               string `bun:"id_hash,pk"`
	PrincipalID          string
	Name                 string
	CredentialCiphertext string
	SignCount            uint32
	CreatedAt            int64
	UpdatedAt            int64
	LastUsedAt           int64
}

type passkeyChallengeRow struct {
	bun.BaseModel `bun:"table:iam_passkey_challenges"`
	IDHash        string `bun:"id_hash,pk"`
	Kind          string
	PrincipalID   string
	ReturnURL     string
	SessionData   string
	CreatedAt     int64
	ExpiresAt     int64
	ConsumedAt    int64
}

func (s *WebService) ListPasskeys(ctx context.Context, authorization, cookieHeader string) ([]authnext.Passkey, error) {
	if err := s.requirePasskeys(); err != nil {
		return nil, err
	}
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return nil, err
	}
	var rows []passkeyRow
	if err := s.db.NewSelect().Model(&rows).Where("principal_id = ?", claims.Subject).Order("created_at DESC").Scan(ctx); err != nil {
		return nil, fmt.Errorf("list passkeys: %w", err)
	}
	result := make([]authnext.Passkey, 0, len(rows))
	for _, row := range rows {
		result = append(result, passkeyView(row))
	}
	return result, nil
}

func (s *WebService) BeginPasskeyRegistration(ctx context.Context, authorization, cookieHeader, currentPassword string) (authnext.PasskeyOptions, error) {
	if err := s.requirePasskeys(); err != nil {
		return authnext.PasskeyOptions{}, err
	}
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return authnext.PasskeyOptions{}, err
	}
	if err := s.ensureRecoveryCooldownExpired(ctx, claims.Subject); err != nil {
		return authnext.PasskeyOptions{}, err
	}
	principal, _, err := s.verifyCurrentPassword(ctx, claims.Subject, currentPassword)
	if err != nil {
		return authnext.PasskeyOptions{}, err
	}
	count, err := s.db.NewSelect().Model((*passkeyRow)(nil)).Where("principal_id = ?", claims.Subject).Count(ctx)
	if err != nil {
		return authnext.PasskeyOptions{}, fmt.Errorf("count passkeys: %w", err)
	}
	if count >= s.cfg.Passkey.MaxCredentials {
		return authnext.PasskeyOptions{}, authnext.ErrPasskeyLimit
	}
	now := s.now().UTC()
	user, err := s.ensurePasskeyUser(ctx, principal, now)
	if err != nil {
		return authnext.PasskeyOptions{}, err
	}
	expires := now.Add(s.cfg.Passkey.ChallengeTTL)
	options, session, err := s.passkeys.BeginRegistration(user, expires)
	if err != nil {
		return authnext.PasskeyOptions{}, fmt.Errorf("begin passkey registration: %w", err)
	}
	result, err := s.storePasskeyChallenge(ctx, passkeyRegistration, claims.Subject, "", session, now, expires)
	if err != nil {
		return authnext.PasskeyOptions{}, err
	}
	result.Options = json.RawMessage(options)
	s.audit(ctx, claims.Subject, "passkey_registration_started", "success")
	return result, nil
}

func (s *WebService) FinishPasskeyRegistration(ctx context.Context, authorization, cookieHeader, challengeID, name string, response json.RawMessage) (authnext.Passkey, error) {
	if err := s.requirePasskeys(); err != nil {
		return authnext.Passkey{}, err
	}
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return authnext.Passkey{}, err
	}
	name, err = normalizePasskeyName(name)
	if err != nil || len(response) == 0 {
		return authnext.Passkey{}, authnext.ErrPasskeyCredential
	}
	now := s.now().UTC()
	challenge, err := s.consumePasskeyChallenge(ctx, challengeID, passkeyRegistration, claims.Subject, now)
	if err != nil {
		return authnext.Passkey{}, err
	}
	user, err := s.loadPasskeyUser(ctx, s.db, claims.Subject)
	if err != nil {
		return authnext.Passkey{}, err
	}
	credential, err := s.passkeys.FinishRegistration(user, []byte(challenge.SessionData), response)
	if err != nil {
		s.audit(ctx, claims.Subject, "passkey_registration", "denied")
		return authnext.Passkey{}, authnext.ErrPasskeyCredential
	}
	view, err := s.storePasskeyCredential(ctx, claims.Subject, name, credential, now)
	if err != nil {
		return authnext.Passkey{}, err
	}
	return view, nil
}

func (s *WebService) storePasskeyCredential(ctx context.Context, principalID, name string, credential webauthnx.Credential, now time.Time) (authnext.Passkey, error) {
	idHash := passkeyIDHash(credential.ID)
	ciphertext, err := s.encryptAuthnData(passkeyCipher, passkeyAAD(principalID, idHash), credential.Data)
	if err != nil {
		return authnext.Passkey{}, err
	}
	row := passkeyRow{IDHash: idHash, PrincipalID: principalID, Name: name, CredentialCiphertext: ciphertext, SignCount: credential.SignCount, CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli()}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		count, err := tx.NewSelect().Model((*passkeyRow)(nil)).Where("principal_id = ?", principalID).Count(ctx)
		if err != nil {
			return err
		}
		if count >= s.cfg.Passkey.MaxCredentials {
			return authnext.ErrPasskeyLimit
		}
		if _, err = tx.NewInsert().Model(&row).Exec(ctx); err != nil {
			return err
		}
		return s.appendSecurityAudit(ctx, tx, principalID, "passkey_registered", "success")
	})
	if err != nil {
		if errors.Is(err, authnext.ErrPasskeyLimit) {
			return authnext.Passkey{}, err
		}
		return authnext.Passkey{}, fmt.Errorf("store passkey: %w", err)
	}
	return passkeyView(row), nil
}

func (s *WebService) BeginPasskeyLogin(ctx context.Context, origin, returnURL string) (authnext.PasskeyOptions, error) {
	if err := s.requirePasskeys(); err != nil {
		return authnext.PasskeyOptions{}, err
	}
	if !slices.Contains(s.cfg.Passkey.Origins, origin) {
		return authnext.PasskeyOptions{}, ErrCSRF
	}
	returnURL = strings.TrimSpace(returnURL)
	if returnURL == "" {
		returnURL = s.web.PostLoginURL
	}
	if !slices.Contains(s.web.AllowedReturnURLs, returnURL) {
		return authnext.PasskeyOptions{}, authnext.ErrReturnURL
	}
	now := s.now().UTC()
	expires := now.Add(s.cfg.Passkey.ChallengeTTL)
	options, session, err := s.passkeys.BeginLogin(expires)
	if err != nil {
		return authnext.PasskeyOptions{}, fmt.Errorf("begin passkey login: %w", err)
	}
	result, err := s.storePasskeyChallenge(ctx, passkeyLogin, "", returnURL, session, now, expires)
	if err != nil {
		return authnext.PasskeyOptions{}, err
	}
	result.Options = json.RawMessage(options)
	return result, nil
}

func (s *WebService) FinishPasskeyLogin(ctx context.Context, origin, challengeID string, response json.RawMessage) (authnext.LoginResult, error) {
	if err := s.requirePasskeys(); err != nil {
		return authnext.LoginResult{}, err
	}
	if !slices.Contains(s.cfg.Passkey.Origins, origin) {
		return authnext.LoginResult{}, ErrCSRF
	}
	if len(response) == 0 {
		return authnext.LoginResult{}, authnext.ErrPasskeyCredential
	}
	now := s.now().UTC()
	challenge, err := s.consumePasskeyChallenge(ctx, challengeID, passkeyLogin, "", now)
	if err != nil {
		return authnext.LoginResult{}, err
	}
	var principalID string
	var current passkeyRow
	_, credential, err := s.passkeys.FinishLogin([]byte(challenge.SessionData), response, func(credentialID, userHandle []byte) (webauthnx.User, error) {
		var lookupErr error
		principalID, current, lookupErr = s.resolvePasskey(ctx, credentialID, userHandle)
		if lookupErr != nil {
			return webauthnx.User{}, lookupErr
		}
		return s.loadPasskeyUser(ctx, s.db, principalID)
	})
	if err != nil || principalID == "" {
		s.audit(ctx, principalID, "passkey_login", "denied")
		return authnext.LoginResult{}, authnext.ErrPasskeyCredential
	}
	sessionID, err := s.completePasskeyLogin(ctx, principalID, current, credential, now)
	if err != nil {
		return authnext.LoginResult{}, err
	}
	return authnext.LoginResult{Status: "authenticated", ReturnURL: challenge.ReturnURL, SessionID: sessionID}, nil
}

func (s *WebService) completePasskeyLogin(ctx context.Context, principalID string, current passkeyRow, credential webauthnx.Credential, now time.Time) (string, error) {
	ciphertext, err := s.encryptAuthnData(passkeyCipher, passkeyAAD(principalID, current.IDHash), credential.Data)
	if err != nil {
		return "", err
	}
	if credential.CloneWarning {
		_, _ = s.db.NewUpdate().Model((*passkeyRow)(nil)).Set("credential_ciphertext = ?", ciphertext).Set("updated_at = ?", now.UnixMilli()).Where("id_hash = ?", current.IDHash).Exec(ctx)
		s.audit(ctx, principalID, "passkey_counter_regression", "denied")
		return "", authnext.ErrPasskeyCredential
	}
	var sessionID string
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*passkeyRow)(nil)).Set("credential_ciphertext = ?", ciphertext).
			Set("sign_count = ?", credential.SignCount).Set("updated_at = ?", now.UnixMilli()).Set("last_used_at = ?", now.UnixMilli()).
			Where("id_hash = ? AND sign_count = ?", current.IDHash, current.SignCount).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return authnext.ErrPasskeyCredential
		}
		sessionID, err = s.insertSession(ctx, tx, principalID, now, authnext.Assurance{AuthTime: now, Level: 2, Methods: []string{"passkey"}})
		if err != nil {
			return err
		}
		return s.appendSecurityAudit(ctx, tx, principalID, "passkey_login", "success")
	})
	if err != nil {
		return "", fmt.Errorf("complete passkey login: %w", err)
	}
	return sessionID, nil
}

func (s *WebService) RenamePasskey(ctx context.Context, authorization, cookieHeader, id, name string) (authnext.Passkey, error) {
	if err := s.requirePasskeys(); err != nil {
		return authnext.Passkey{}, err
	}
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return authnext.Passkey{}, err
	}
	name, err = normalizePasskeyName(name)
	if err != nil {
		return authnext.Passkey{}, err
	}
	now := s.now().UTC().UnixMilli()
	var row passkeyRow
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*passkeyRow)(nil)).Set("name = ?", name).Set("updated_at = ?", now).
			Where("id_hash = ? AND principal_id = ?", id, claims.Subject).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return authnext.ErrPasskeyNotFound
		}
		if err := tx.NewSelect().Model(&row).Where("id_hash = ? AND principal_id = ?", id, claims.Subject).Scan(ctx); err != nil {
			return fmt.Errorf("load renamed passkey: %w", err)
		}
		return s.appendSecurityAudit(ctx, tx, claims.Subject, "passkey_renamed", "success")
	})
	if err != nil {
		if errors.Is(err, authnext.ErrPasskeyNotFound) {
			return authnext.Passkey{}, err
		}
		return authnext.Passkey{}, fmt.Errorf("rename passkey: %w", err)
	}
	return passkeyView(row), nil
}

func (s *WebService) DeletePasskey(ctx context.Context, authorization, cookieHeader, id, currentPassword string) error {
	if err := s.requirePasskeys(); err != nil {
		return err
	}
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return err
	}
	if _, _, err := s.verifyCurrentPassword(ctx, claims.Subject, currentPassword); err != nil {
		return err
	}
	now := s.now().UTC().UnixMilli()
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewDelete().Model((*passkeyRow)(nil)).Where("id_hash = ? AND principal_id = ?", id, claims.Subject).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return authnext.ErrPasskeyNotFound
		}
		if err := s.revokeOtherAuthentication(ctx, tx, claims.Subject, currentSessionHash(cookieHeader, s.web.CookieName), now); err != nil {
			return err
		}
		return s.appendSecurityAudit(ctx, tx, claims.Subject, "passkey_deleted", "success")
	})
	if err != nil {
		if errors.Is(err, authnext.ErrPasskeyNotFound) {
			return err
		}
		return fmt.Errorf("delete passkey: %w", err)
	}
	return nil
}

func (s *WebService) requirePasskeys() error {
	if s == nil || !s.Enabled() || !s.cfg.Passkey.Enabled || s.passkeys == nil {
		return authnext.ErrPasskeyDisabled
	}
	return nil
}

func (s *WebService) ensurePasskeyUser(ctx context.Context, principal principalRow, now time.Time) (webauthnx.User, error) {
	var row passkeyUserRow
	err := s.db.NewSelect().Model(&row).Where("principal_id = ?", principal.ID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		handle := make([]byte, 32)
		if _, err := rand.Read(handle); err != nil {
			return webauthnx.User{}, fmt.Errorf("generate passkey user handle: %w", err)
		}
		row = passkeyUserRow{PrincipalID: principal.ID, UserHandle: base64.RawURLEncoding.EncodeToString(handle), CreatedAt: now.UnixMilli()}
		if _, err := s.db.NewInsert().Model(&row).Ignore().Exec(ctx); err != nil {
			return webauthnx.User{}, fmt.Errorf("store passkey user handle: %w", err)
		}
		err = s.db.NewSelect().Model(&row).Where("principal_id = ?", principal.ID).Scan(ctx)
	}
	if err != nil {
		return webauthnx.User{}, fmt.Errorf("load passkey user handle: %w", err)
	}
	return s.loadPasskeyUserWithIdentity(ctx, s.db, principal, row)
}

func (s *WebService) loadPasskeyUser(ctx context.Context, db bun.IDB, principalID string) (webauthnx.User, error) {
	var principal principalRow
	if err := db.NewSelect().Model(&principal).Where("id = ? AND status = 'active'", principalID).Scan(ctx); err != nil {
		return webauthnx.User{}, authnext.ErrPasskeyCredential
	}
	var user passkeyUserRow
	if err := db.NewSelect().Model(&user).Where("principal_id = ?", principalID).Scan(ctx); err != nil {
		return webauthnx.User{}, authnext.ErrPasskeyCredential
	}
	return s.loadPasskeyUserWithIdentity(ctx, db, principal, user)
}

func (s *WebService) loadPasskeyUserWithIdentity(ctx context.Context, db bun.IDB, principal principalRow, user passkeyUserRow) (webauthnx.User, error) {
	handle, err := base64.RawURLEncoding.DecodeString(user.UserHandle)
	if err != nil || len(handle) == 0 || len(handle) > 64 {
		return webauthnx.User{}, authnext.ErrPasskeyCredential
	}
	var rows []passkeyRow
	if err := db.NewSelect().Model(&rows).Where("principal_id = ?", principal.ID).Scan(ctx); err != nil {
		return webauthnx.User{}, fmt.Errorf("load passkey credentials: %w", err)
	}
	stored := make([][]byte, 0, len(rows))
	for _, row := range rows {
		plain, err := s.decryptAuthnData(passkeyCipher, passkeyAAD(principal.ID, row.IDHash), row.CredentialCiphertext)
		if err != nil {
			return webauthnx.User{}, authnext.ErrPasskeyCredential
		}
		stored = append(stored, plain)
	}
	resolved, err := webauthnx.NewUser(handle, principal.LoginName, principal.DisplayName, stored)
	if err != nil {
		return webauthnx.User{}, authnext.ErrPasskeyCredential
	}
	return resolved, nil
}

func (s *WebService) resolvePasskey(ctx context.Context, credentialID, userHandle []byte) (string, passkeyRow, error) {
	var row passkeyRow
	if err := s.db.NewSelect().Model(&row).Where("id_hash = ?", passkeyIDHash(credentialID)).Scan(ctx); err != nil {
		return "", row, authnext.ErrPasskeyCredential
	}
	var user passkeyUserRow
	if err := s.db.NewSelect().Model(&user).Where("principal_id = ? AND user_handle = ?", row.PrincipalID, base64.RawURLEncoding.EncodeToString(userHandle)).Scan(ctx); err != nil {
		return "", row, authnext.ErrPasskeyCredential
	}
	var status string
	if err := s.db.NewSelect().Table("iam_principals").Column("status").Where("id = ?", row.PrincipalID).Scan(ctx, &status); err != nil || status != "active" {
		return "", row, authnext.ErrPasskeyCredential
	}
	return row.PrincipalID, row, nil
}

func (s *WebService) storePasskeyChallenge(ctx context.Context, kind, principalID, returnURL string, session []byte, now, expires time.Time) (authnext.PasskeyOptions, error) {
	id, err := randomToken(32)
	if err != nil {
		return authnext.PasskeyOptions{}, err
	}
	row := passkeyChallengeRow{IDHash: tokenHash(id), Kind: kind, PrincipalID: principalID, ReturnURL: returnURL, SessionData: string(session), CreatedAt: now.UnixMilli(), ExpiresAt: expires.UnixMilli()}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewDelete().Model((*passkeyChallengeRow)(nil)).Where("consumed_at <> 0 OR expires_at <= ?", now.UnixMilli()).Exec(ctx); err != nil {
			return err
		}
		_, err := tx.NewInsert().Model(&row).Exec(ctx)
		return err
	})
	if err != nil {
		return authnext.PasskeyOptions{}, fmt.Errorf("store passkey challenge: %w", err)
	}
	return authnext.PasskeyOptions{ChallengeID: id, ExpiresAt: expires}, nil
}

func (s *WebService) consumePasskeyChallenge(ctx context.Context, id, kind, principalID string, now time.Time) (passkeyChallengeRow, error) {
	if strings.TrimSpace(id) == "" {
		return passkeyChallengeRow{}, authnext.ErrPasskeyChallenge
	}
	hash := tokenHash(id)
	var row passkeyChallengeRow
	query := s.db.NewSelect().Model(&row).Where("id_hash = ? AND kind = ?", hash, kind)
	if principalID != "" {
		query = query.Where("principal_id = ?", principalID)
	}
	if err := query.Scan(ctx); err != nil {
		return passkeyChallengeRow{}, authnext.ErrPasskeyChallenge
	}
	update := s.db.NewUpdate().Model((*passkeyChallengeRow)(nil)).Set("consumed_at = ?", now.UnixMilli()).
		Where("id_hash = ? AND kind = ? AND consumed_at = 0 AND expires_at > ?", hash, kind, now.UnixMilli())
	if principalID != "" {
		update = update.Where("principal_id = ?", principalID)
	}
	result, err := update.Exec(ctx)
	if err != nil {
		return passkeyChallengeRow{}, fmt.Errorf("consume passkey challenge: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return passkeyChallengeRow{}, authnext.ErrPasskeyChallenge
	}
	return row, nil
}

func normalizePasskeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return "", authnext.ErrPasskeyCredential
	}
	return name, nil
}

func passkeyIDHash(id []byte) string {
	sum := sha256.Sum256(id)
	return hex.EncodeToString(sum[:])
}

func passkeyAAD(principalID, idHash string) string { return principalID + "\x00" + idHash }

func passkeyView(row passkeyRow) authnext.Passkey {
	view := authnext.Passkey{ID: row.IDHash, Name: row.Name, SignCount: row.SignCount, CreatedAt: time.UnixMilli(row.CreatedAt).UTC()}
	if row.LastUsedAt > 0 {
		lastUsed := time.UnixMilli(row.LastUsedAt).UTC()
		view.LastUsedAt = &lastUsed
	}
	return view
}
