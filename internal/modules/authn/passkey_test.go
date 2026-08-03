package authn

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/webauthnx"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPasskeyAuditFailuresRollBackSecurityMutations(t *testing.T) {
	t.Run("registration", func(t *testing.T) {
		service, principalID := newLocalService(t)
		now := time.Unix(6_000, 0).UTC()
		credential := webauthnxCredential([]byte("audit-registration"), 1, false)
		rejectAuthnAudit(t, service, "passkey_registered")

		_, err := service.storePasskeyCredential(t.Context(), principalID, "Security key", credential, now)
		require.ErrorContains(t, err, "append audit event")
		count, countErr := service.db.NewSelect().Model((*passkeyRow)(nil)).Where("id_hash = ?", passkeyIDHash(credential.ID)).Count(t.Context())
		require.NoError(t, countErr)
		assert.Zero(t, count)
		assertAuthnAuditCount(t, service, "passkey_registered", 0)
	})

	t.Run("login", func(t *testing.T) {
		service, principalID := newLocalService(t)
		now := time.Unix(7_000, 0).UTC()
		credential := webauthnxCredential([]byte("audit-login"), 8, false)
		idHash := passkeyIDHash(credential.ID)
		ciphertext, err := service.encryptAuthnData(passkeyCipher, passkeyAAD(principalID, idHash), webauthnxCredential(credential.ID, 7, false).Data)
		require.NoError(t, err)
		stored := passkeyRow{IDHash: idHash, PrincipalID: principalID, Name: "Key", CredentialCiphertext: ciphertext, SignCount: 7, CreatedAt: now.Add(-time.Hour).UnixMilli(), UpdatedAt: now.Add(-time.Hour).UnixMilli()}
		_, err = service.db.NewInsert().Model(&stored).Exec(t.Context())
		require.NoError(t, err)
		rejectAuthnAudit(t, service, "passkey_login")

		_, err = service.completePasskeyLogin(t.Context(), principalID, stored, credential, now)
		require.ErrorContains(t, err, "append audit event")
		var after passkeyRow
		require.NoError(t, service.db.NewSelect().Model(&after).Where("id_hash = ?", idHash).Scan(t.Context()))
		assert.Equal(t, stored.CredentialCiphertext, after.CredentialCiphertext)
		assert.Equal(t, stored.SignCount, after.SignCount)
		assert.Equal(t, stored.UpdatedAt, after.UpdatedAt)
		assert.Zero(t, after.LastUsedAt)
		sessions, countErr := service.db.NewSelect().Model((*sessionRow)(nil)).Where("principal_id = ?", principalID).Count(t.Context())
		require.NoError(t, countErr)
		assert.Zero(t, sessions)
		assertAuthnAuditCount(t, service, "passkey_login", 0)
	})

	t.Run("rename", func(t *testing.T) {
		service, principalID := newLocalService(t)
		cookie := authenticatedCookie(t, service)
		now := time.Unix(8_000, 0).UTC().UnixMilli()
		stored := passkeyRow{IDHash: strings.Repeat("8", 64), PrincipalID: principalID, Name: "Original", CredentialCiphertext: "metadata", CreatedAt: now, UpdatedAt: now}
		_, err := service.db.NewInsert().Model(&stored).Exec(t.Context())
		require.NoError(t, err)
		rejectAuthnAudit(t, service, "passkey_renamed")

		_, err = service.RenamePasskey(t.Context(), "", cookie, stored.IDHash, "Renamed")
		require.ErrorContains(t, err, "append audit event")
		var after passkeyRow
		require.NoError(t, service.db.NewSelect().Model(&after).Where("id_hash = ?", stored.IDHash).Scan(t.Context()))
		assert.Equal(t, stored.Name, after.Name)
		assert.Equal(t, stored.UpdatedAt, after.UpdatedAt)
		assertAuthnAuditCount(t, service, "passkey_renamed", 0)
	})

	t.Run("delete", func(t *testing.T) {
		service, principalID := newLocalService(t)
		current, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		other, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
		require.NoError(t, err)
		cookie := service.SessionCookie(current)
		now := time.Unix(9_000, 0).UTC().UnixMilli()
		stored := passkeyRow{IDHash: strings.Repeat("9", 64), PrincipalID: principalID, Name: "Key", CredentialCiphertext: "metadata", CreatedAt: now, UpdatedAt: now}
		_, err = service.db.NewInsert().Model(&stored).Exec(t.Context())
		require.NoError(t, err)
		rejectAuthnAudit(t, service, "passkey_deleted")

		err = service.DeletePasskey(t.Context(), "", cookie, stored.IDHash, "correct horse battery staple")
		require.ErrorContains(t, err, "append audit event")
		count, countErr := service.db.NewSelect().Model((*passkeyRow)(nil)).Where("id_hash = ?", stored.IDHash).Count(t.Context())
		require.NoError(t, countErr)
		assert.Equal(t, 1, count)
		_, err = service.Authenticate(t.Context(), "", service.SessionCookie(other))
		require.NoError(t, err)
		assertAuthnAuditCount(t, service, "passkey_deleted", 0)
	})
}

func TestPasskeyChallengeBoundaries(t *testing.T) {
	service, principalID := newLocalService(t)
	token, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	cookie := service.SessionCookie(token)

	passkeys, err := service.ListPasskeys(t.Context(), "", cookie)
	require.NoError(t, err)
	assert.Empty(t, passkeys)
	_, err = service.BeginPasskeyRegistration(t.Context(), "", cookie, "wrong password")
	assert.ErrorIs(t, err, authnext.ErrInvalidCredentials)
	registration, err := service.BeginPasskeyRegistration(t.Context(), "", cookie, "correct horse battery staple")
	require.NoError(t, err)
	assert.NotEmpty(t, registration.ChallengeID)
	assert.WithinDuration(t, time.Now().Add(5*time.Minute), registration.ExpiresAt, 2*time.Second)
	assert.Contains(t, string(registration.Options), `"residentKey":"required"`)

	_, err = service.FinishPasskeyRegistration(t.Context(), "", cookie, registration.ChallengeID, "Laptop", json.RawMessage(`{"invalid":true}`))
	assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
	_, err = service.FinishPasskeyRegistration(t.Context(), "", cookie, registration.ChallengeID, "Laptop", json.RawMessage(`{"invalid":true}`))
	assert.ErrorIs(t, err, authnext.ErrPasskeyChallenge, "a failed ceremony still burns its one-time challenge")

	_, err = service.BeginPasskeyLogin(t.Context(), "https://evil.example", "https://app.example/")
	assert.ErrorIs(t, err, ErrCSRF)
	_, err = service.BeginPasskeyLogin(t.Context(), "https://app.example", "https://evil.example/")
	assert.ErrorIs(t, err, authnext.ErrReturnURL)
	login, err := service.BeginPasskeyLogin(t.Context(), "https://app.example", "")
	require.NoError(t, err)
	assert.Contains(t, string(login.Options), `"userVerification":"required"`)
	_, err = service.FinishPasskeyLogin(t.Context(), "https://app.example", login.ChallengeID, json.RawMessage(`{"invalid":true}`))
	assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
	_, err = service.FinishPasskeyLogin(t.Context(), "https://app.example", login.ChallengeID, json.RawMessage(`{"invalid":true}`))
	assert.ErrorIs(t, err, authnext.ErrPasskeyChallenge)

	var user passkeyUserRow
	require.NoError(t, service.db.NewSelect().Model(&user).Where("principal_id = ?", principalID).Scan(t.Context()))
	assert.NotEmpty(t, user.UserHandle)
}

func TestPasskeyCredentialManagement(t *testing.T) {
	service, principalID := newLocalService(t)
	current, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	other, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	cookie := service.SessionCookie(current)
	now := time.Now().UTC().UnixMilli()
	row := passkeyRow{
		IDHash: strings.Repeat("a", 64), PrincipalID: principalID, Name: "Laptop",
		CredentialCiphertext: "unused-for-metadata-operations", SignCount: 7, CreatedAt: now, UpdatedAt: now,
	}
	_, err = service.db.NewInsert().Model(&row).Exec(t.Context())
	require.NoError(t, err)

	renamed, err := service.RenamePasskey(t.Context(), "", cookie, row.IDHash, " Security key ")
	require.NoError(t, err)
	assert.Equal(t, "Security key", renamed.Name)
	assert.Equal(t, uint32(7), renamed.SignCount)
	_, err = service.RenamePasskey(t.Context(), "", cookie, strings.Repeat("b", 64), "Missing")
	assert.ErrorIs(t, err, authnext.ErrPasskeyNotFound)
	_, err = service.RenamePasskey(t.Context(), "", cookie, row.IDHash, "")
	assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)

	assert.ErrorIs(t, service.DeletePasskey(t.Context(), "", cookie, row.IDHash, "wrong"), authnext.ErrInvalidCredentials)
	require.NoError(t, service.DeletePasskey(t.Context(), "", cookie, row.IDHash, "correct horse battery staple"))
	_, err = service.Authenticate(t.Context(), "", service.SessionCookie(other))
	assert.ErrorIs(t, err, ErrInvalidSession)
	assert.ErrorIs(t, service.DeletePasskey(t.Context(), "", cookie, row.IDHash, "correct horse battery staple"), authnext.ErrPasskeyNotFound)
}

func TestPasskeyDisabledAndStorageFailures(t *testing.T) {
	service, _ := newLocalService(t)
	service.cfg.Passkey.Enabled = false
	assert.ErrorIs(t, service.requirePasskeys(), authnext.ErrPasskeyDisabled)
	_, err := service.ListPasskeys(t.Context(), "", "")
	assert.ErrorIs(t, err, authnext.ErrPasskeyDisabled)
	_, err = service.BeginPasskeyRegistration(t.Context(), "", "", "password")
	assert.ErrorIs(t, err, authnext.ErrPasskeyDisabled)
	_, err = service.FinishPasskeyRegistration(t.Context(), "", "", "challenge", "Key", json.RawMessage(`{}`))
	assert.ErrorIs(t, err, authnext.ErrPasskeyDisabled)
	_, err = service.BeginPasskeyLogin(t.Context(), "https://app.example", "")
	assert.ErrorIs(t, err, authnext.ErrPasskeyDisabled)
	_, err = service.FinishPasskeyLogin(t.Context(), "https://app.example", "challenge", json.RawMessage(`{}`))
	assert.ErrorIs(t, err, authnext.ErrPasskeyDisabled)
	_, err = service.RenamePasskey(t.Context(), "", "", strings.Repeat("a", 64), "Key")
	assert.ErrorIs(t, err, authnext.ErrPasskeyDisabled)
	assert.ErrorIs(t, service.DeletePasskey(t.Context(), "", "", strings.Repeat("a", 64), "password"), authnext.ErrPasskeyDisabled)

	service.cfg.Passkey.Enabled = true
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	challenge, err := service.BeginPasskeyLogin(t.Context(), "https://app.example", "")
	require.NoError(t, err)
	service.now = func() time.Time { return time.Unix(100, 0).UTC().Add(6 * time.Minute) }
	_, err = service.FinishPasskeyLogin(t.Context(), "https://app.example", challenge.ChallengeID, json.RawMessage(`{"invalid":true}`))
	assert.ErrorIs(t, err, authnext.ErrPasskeyChallenge)

	require.NoError(t, service.db.Close())
	_, err = service.BeginPasskeyLogin(t.Context(), "https://app.example", "")
	assert.Error(t, err)
}

func TestPasskeyConfigurationValidation(t *testing.T) {
	service, _ := newLocalService(t)
	cfg := service.cfg
	cfg.Passkey.Origins = []string{"https://untrusted.example"}
	_, err := NewWebService(cfg, service.db)
	assert.ErrorContains(t, err, "not an allowed web origin")
	cfg.Passkey.Origins = []string{"https://app.example"}
	cfg.Passkey.MaxCredentials = 21
	_, err = NewWebService(cfg, service.db)
	assert.ErrorContains(t, err, "security limits")
}

func TestPasskeyStorageFailuresFailClosed(t *testing.T) {
	t.Run("list credentials", func(t *testing.T) {
		service, _ := newLocalService(t)
		cookie := authenticatedCookie(t, service)
		_, err := service.db.ExecContext(t.Context(), "DROP TABLE iam_passkeys")
		require.NoError(t, err)
		_, err = service.ListPasskeys(t.Context(), "", cookie)
		assert.ErrorContains(t, err, "list passkeys")
	})

	t.Run("count credentials", func(t *testing.T) {
		service, _ := newLocalService(t)
		cookie := authenticatedCookie(t, service)
		_, err := service.db.ExecContext(t.Context(), "DROP TABLE iam_passkeys")
		require.NoError(t, err)
		_, err = service.BeginPasskeyRegistration(t.Context(), "", cookie, "correct horse battery staple")
		assert.ErrorContains(t, err, "count passkeys")
	})

	t.Run("rename credential", func(t *testing.T) {
		service, _ := newLocalService(t)
		cookie := authenticatedCookie(t, service)
		_, err := service.db.ExecContext(t.Context(), "DROP TABLE iam_passkeys")
		require.NoError(t, err)
		_, err = service.RenamePasskey(t.Context(), "", cookie, strings.Repeat("a", 64), "Security key")
		assert.ErrorContains(t, err, "rename passkey")
	})

	t.Run("delete credential", func(t *testing.T) {
		service, _ := newLocalService(t)
		cookie := authenticatedCookie(t, service)
		_, err := service.db.ExecContext(t.Context(), "DROP TABLE iam_passkeys")
		require.NoError(t, err)
		err = service.DeletePasskey(t.Context(), "", cookie, strings.Repeat("a", 64), "correct horse battery staple")
		assert.ErrorContains(t, err, "delete passkey")
	})

	t.Run("load user without handle", func(t *testing.T) {
		service, principalID := newLocalService(t)
		_, err := service.loadPasskeyUser(t.Context(), service.db, principalID)
		assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
	})

	t.Run("load user credential storage", func(t *testing.T) {
		service, principalID := newLocalService(t)
		handle := base64.RawURLEncoding.EncodeToString([]byte("user-handle"))
		_, err := service.db.NewInsert().Model(&passkeyUserRow{PrincipalID: principalID, UserHandle: handle, CreatedAt: time.Now().UnixMilli()}).Exec(t.Context())
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), "DROP TABLE iam_passkeys")
		require.NoError(t, err)
		_, err = service.loadPasskeyUser(t.Context(), service.db, principalID)
		assert.ErrorContains(t, err, "load passkey credentials")
	})

	t.Run("ensure user storage", func(t *testing.T) {
		service, principalID := newLocalService(t)
		var principal principalRow
		require.NoError(t, service.db.NewSelect().Model(&principal).Where("id = ?", principalID).Scan(t.Context()))
		_, err := service.db.ExecContext(t.Context(), "DROP TABLE iam_passkey_users")
		require.NoError(t, err)
		_, err = service.ensurePasskeyUser(t.Context(), principal, time.Now().UTC())
		assert.ErrorContains(t, err, "load passkey user handle")
	})

	t.Run("challenge cleanup storage", func(t *testing.T) {
		service, _ := newLocalService(t)
		_, err := service.db.ExecContext(t.Context(), "DROP TABLE iam_passkey_challenges")
		require.NoError(t, err)
		now := time.Now().UTC()
		_, err = service.storePasskeyChallenge(t.Context(), passkeyLogin, "", "https://app.example/", []byte(`{"state":true}`), now, now.Add(time.Minute))
		assert.ErrorContains(t, err, "store passkey challenge")
	})

	t.Run("challenge insert storage", func(t *testing.T) {
		service, _ := newLocalService(t)
		_, err := service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_passkey_challenge BEFORE INSERT ON iam_passkey_challenges BEGIN SELECT RAISE(ABORT, 'challenge denied'); END`)
		require.NoError(t, err)
		now := time.Now().UTC()
		_, err = service.storePasskeyChallenge(t.Context(), passkeyLogin, "", "https://app.example/", []byte(`{"state":true}`), now, now.Add(time.Minute))
		assert.ErrorContains(t, err, "challenge denied")
	})

	t.Run("credential transaction", func(t *testing.T) {
		service, principalID := newLocalService(t)
		_, err := service.db.ExecContext(t.Context(), "DROP TABLE iam_passkeys")
		require.NoError(t, err)
		_, err = service.storePasskeyCredential(t.Context(), principalID, "Security key", webauthnxCredential([]byte("credential"), 1, false), time.Now().UTC())
		assert.ErrorContains(t, err, "store passkey")
	})

	t.Run("registration user state", func(t *testing.T) {
		service, principalID := newLocalService(t)
		cookie := authenticatedCookie(t, service)
		now := time.Now().UTC()
		challenge, err := service.storePasskeyChallenge(t.Context(), passkeyRegistration, principalID, "", []byte(`{"state":true}`), now, now.Add(time.Minute))
		require.NoError(t, err)
		_, err = service.FinishPasskeyRegistration(t.Context(), "", cookie, challenge.ChallengeID, "Security key", json.RawMessage(`{"invalid":true}`))
		assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
	})

	t.Run("login transaction", func(t *testing.T) {
		service, principalID := newLocalService(t)
		require.NoError(t, service.db.Close())
		current := passkeyRow{IDHash: strings.Repeat("a", 64), PrincipalID: principalID, SignCount: 1}
		_, err := service.completePasskeyLogin(t.Context(), principalID, current, webauthnxCredential([]byte("credential"), 2, false), time.Now().UTC())
		assert.ErrorContains(t, err, "complete passkey login")
	})

	t.Run("user handle insert", func(t *testing.T) {
		service, _ := newLocalService(t)
		cookie := authenticatedCookie(t, service)
		_, err := service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_passkey_user BEFORE INSERT ON iam_passkey_users BEGIN SELECT RAISE(ABORT, 'user handle denied'); END`)
		require.NoError(t, err)
		_, err = service.BeginPasskeyRegistration(t.Context(), "", cookie, "correct horse battery staple")
		assert.ErrorContains(t, err, "user handle denied")
	})

	t.Run("registration challenge", func(t *testing.T) {
		service, _ := newLocalService(t)
		cookie := authenticatedCookie(t, service)
		_, err := service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_registration_challenge BEFORE INSERT ON iam_passkey_challenges BEGIN SELECT RAISE(ABORT, 'registration challenge denied'); END`)
		require.NoError(t, err)
		_, err = service.BeginPasskeyRegistration(t.Context(), "", cookie, "correct horse battery staple")
		assert.ErrorContains(t, err, "registration challenge denied")
	})

	t.Run("challenge consumption", func(t *testing.T) {
		service, _ := newLocalService(t)
		now := time.Now().UTC()
		challenge, err := service.storePasskeyChallenge(t.Context(), passkeyLogin, "", "https://app.example/", []byte(`{"state":true}`), now, now.Add(time.Minute))
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), `CREATE TRIGGER deny_passkey_consume BEFORE UPDATE ON iam_passkey_challenges BEGIN SELECT RAISE(ABORT, 'consume denied'); END`)
		require.NoError(t, err)
		_, err = service.consumePasskeyChallenge(t.Context(), challenge.ChallengeID, passkeyLogin, "", now)
		assert.ErrorContains(t, err, "consume denied")
	})

	t.Run("renamed credential disappears", func(t *testing.T) {
		service, principalID := newLocalService(t)
		cookie := authenticatedCookie(t, service)
		now := time.Now().UTC().UnixMilli()
		row := passkeyRow{IDHash: strings.Repeat("a", 64), PrincipalID: principalID, Name: "Key", CredentialCiphertext: "metadata", CreatedAt: now, UpdatedAt: now}
		_, err := service.db.NewInsert().Model(&row).Exec(t.Context())
		require.NoError(t, err)
		_, err = service.db.ExecContext(t.Context(), `CREATE TRIGGER delete_renamed_passkey AFTER UPDATE ON iam_passkeys BEGIN DELETE FROM iam_passkeys WHERE id_hash = NEW.id_hash; END`)
		require.NoError(t, err)
		_, err = service.RenamePasskey(t.Context(), "", cookie, row.IDHash, "Renamed")
		assert.ErrorContains(t, err, "load renamed passkey")
	})
}

func TestPasskeyStoredCredentialResolution(t *testing.T) {
	service, principalID := newLocalService(t)
	var principal principalRow
	require.NoError(t, service.db.NewSelect().Model(&principal).Where("id = ?", principalID).Scan(t.Context()))
	now := time.Now().UTC()
	user, err := service.ensurePasskeyUser(t.Context(), principal, now)
	require.NoError(t, err)
	firstHandle := append([]byte(nil), user.WebAuthnID()...)
	user, err = service.ensurePasskeyUser(t.Context(), principal, now.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, firstHandle, user.WebAuthnID(), "an existing principal keeps a stable WebAuthn user handle")

	credential := webauthn.Credential{
		ID:        []byte("credential-id"),
		PublicKey: []byte{1, 2, 3},
		Flags:     webauthn.NewCredentialFlags(protocol.FlagUserPresent | protocol.FlagUserVerified),
		Authenticator: webauthn.Authenticator{
			SignCount: 9,
		},
	}
	credentialData, err := json.Marshal(credential)
	require.NoError(t, err)
	idHash := passkeyIDHash(credential.ID)
	ciphertext, err := service.encryptAuthnData(passkeyCipher, passkeyAAD(principalID, idHash), credentialData)
	require.NoError(t, err)
	createdAt := now.UnixMilli()
	lastUsedAt := now.Add(time.Minute).UnixMilli()
	row := passkeyRow{
		IDHash: idHash, PrincipalID: principalID, Name: "Platform authenticator",
		CredentialCiphertext: ciphertext, SignCount: 9, CreatedAt: createdAt, UpdatedAt: createdAt, LastUsedAt: lastUsedAt,
	}
	_, err = service.db.NewInsert().Model(&row).Exec(t.Context())
	require.NoError(t, err)

	loaded, err := service.loadPasskeyUser(t.Context(), service.db, principalID)
	require.NoError(t, err)
	require.Len(t, loaded.WebAuthnCredentials(), 1)
	assert.Equal(t, credential.ID, loaded.WebAuthnCredentials()[0].ID)
	resolvedID, resolvedRow, err := service.resolvePasskey(t.Context(), credential.ID, firstHandle)
	require.NoError(t, err)
	assert.Equal(t, principalID, resolvedID)
	assert.Equal(t, idHash, resolvedRow.IDHash)
	_, _, err = service.resolvePasskey(t.Context(), []byte("unknown"), firstHandle)
	assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
	_, _, err = service.resolvePasskey(t.Context(), credential.ID, []byte("wrong-handle"))
	assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)

	listed, err := service.ListPasskeys(t.Context(), "", authenticatedCookie(t, service))
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, idHash, listed[0].ID)
	assert.Equal(t, time.UnixMilli(lastUsedAt).UTC(), *listed[0].LastUsedAt)
	assert.Equal(t, principalID+"\x00"+idHash, passkeyAAD(principalID, idHash))

	_, err = service.db.NewUpdate().Table("iam_principals").Set("status = 'disabled'").Where("id = ?", principalID).Exec(t.Context())
	require.NoError(t, err)
	_, _, err = service.resolvePasskey(t.Context(), credential.ID, firstHandle)
	assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
	_, err = service.loadPasskeyUser(t.Context(), service.db, principalID)
	assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
}

func TestPasskeyRejectsCorruptedStoredIdentity(t *testing.T) {
	t.Run("user handle", func(t *testing.T) {
		service, principalID := newLocalService(t)
		_, err := service.db.NewInsert().Model(&passkeyUserRow{PrincipalID: principalID, UserHandle: "%%%", CreatedAt: time.Now().UnixMilli()}).Exec(t.Context())
		require.NoError(t, err)
		_, err = service.loadPasskeyUser(t.Context(), service.db, principalID)
		assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
	})

	t.Run("ciphertext", func(t *testing.T) {
		service, principalID := newLocalService(t)
		handle := base64.RawURLEncoding.EncodeToString([]byte("user-handle"))
		_, err := service.db.NewInsert().Model(&passkeyUserRow{PrincipalID: principalID, UserHandle: handle, CreatedAt: time.Now().UnixMilli()}).Exec(t.Context())
		require.NoError(t, err)
		row := passkeyRow{IDHash: strings.Repeat("c", 64), PrincipalID: principalID, Name: "Corrupted", CredentialCiphertext: "not-ciphertext", CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli()}
		_, err = service.db.NewInsert().Model(&row).Exec(t.Context())
		require.NoError(t, err)
		_, err = service.loadPasskeyUser(t.Context(), service.db, principalID)
		assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
	})

	t.Run("credential JSON", func(t *testing.T) {
		service, principalID := newLocalService(t)
		handle := base64.RawURLEncoding.EncodeToString([]byte("user-handle"))
		_, err := service.db.NewInsert().Model(&passkeyUserRow{PrincipalID: principalID, UserHandle: handle, CreatedAt: time.Now().UnixMilli()}).Exec(t.Context())
		require.NoError(t, err)
		idHash := strings.Repeat("d", 64)
		ciphertext, err := service.encryptAuthnData(passkeyCipher, passkeyAAD(principalID, idHash), []byte("not-json"))
		require.NoError(t, err)
		row := passkeyRow{IDHash: idHash, PrincipalID: principalID, Name: "Invalid", CredentialCiphertext: ciphertext, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli()}
		_, err = service.db.NewInsert().Model(&row).Exec(t.Context())
		require.NoError(t, err)
		_, err = service.loadPasskeyUser(t.Context(), service.db, principalID)
		assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
	})
}

func TestPasskeyChallengePersistenceAndLimits(t *testing.T) {
	service, principalID := newLocalService(t)
	now := time.Unix(1_000, 0).UTC()
	_, err := service.consumePasskeyChallenge(t.Context(), "", passkeyLogin, "", now)
	assert.ErrorIs(t, err, authnext.ErrPasskeyChallenge)

	challenge, err := service.storePasskeyChallenge(t.Context(), passkeyRegistration, principalID, "", []byte(`{"state":true}`), now, now.Add(time.Minute))
	require.NoError(t, err)
	_, err = service.consumePasskeyChallenge(t.Context(), challenge.ChallengeID, passkeyLogin, principalID, now)
	assert.ErrorIs(t, err, authnext.ErrPasskeyChallenge)
	_, err = service.consumePasskeyChallenge(t.Context(), challenge.ChallengeID, passkeyRegistration, "other-principal", now)
	assert.ErrorIs(t, err, authnext.ErrPasskeyChallenge)
	consumed, err := service.consumePasskeyChallenge(t.Context(), challenge.ChallengeID, passkeyRegistration, principalID, now)
	require.NoError(t, err)
	assert.Equal(t, principalID, consumed.PrincipalID)
	_, err = service.consumePasskeyChallenge(t.Context(), challenge.ChallengeID, passkeyRegistration, principalID, now)
	assert.ErrorIs(t, err, authnext.ErrPasskeyChallenge)

	expired, err := service.storePasskeyChallenge(t.Context(), passkeyLogin, "", "https://app.example/", []byte(`{"state":true}`), now, now.Add(time.Second))
	require.NoError(t, err)
	_, err = service.consumePasskeyChallenge(t.Context(), expired.ChallengeID, passkeyLogin, "", now.Add(time.Second))
	assert.ErrorIs(t, err, authnext.ErrPasskeyChallenge)

	cookie := authenticatedCookie(t, service)
	service.cfg.Passkey.MaxCredentials = 1
	row := passkeyRow{IDHash: strings.Repeat("e", 64), PrincipalID: principalID, Name: "Existing", CredentialCiphertext: "metadata-only", CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli()}
	_, err = service.db.NewInsert().Model(&row).Exec(t.Context())
	require.NoError(t, err)
	_, err = service.BeginPasskeyRegistration(t.Context(), "", cookie, "correct horse battery staple")
	assert.ErrorIs(t, err, authnext.ErrPasskeyLimit)
}

func TestPasskeyPublicInputAndAuthenticationFailures(t *testing.T) {
	service, _ := newLocalService(t)
	_, err := service.ListPasskeys(t.Context(), "", "")
	assert.Error(t, err)
	_, err = service.BeginPasskeyRegistration(t.Context(), "", "", "correct horse battery staple")
	assert.Error(t, err)
	_, err = service.FinishPasskeyRegistration(t.Context(), "", "", "challenge", "Key", json.RawMessage(`{}`))
	assert.Error(t, err)
	_, err = service.RenamePasskey(t.Context(), "", "", strings.Repeat("a", 64), "Key")
	assert.Error(t, err)
	assert.Error(t, service.DeletePasskey(t.Context(), "", "", strings.Repeat("a", 64), "correct horse battery staple"))

	cookie := authenticatedCookie(t, service)
	_, err = service.FinishPasskeyRegistration(t.Context(), "", cookie, "challenge", "", json.RawMessage(`{}`))
	assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
	_, err = service.FinishPasskeyRegistration(t.Context(), "", cookie, "challenge", "Key", nil)
	assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
	_, err = service.FinishPasskeyRegistration(t.Context(), "", cookie, "", "Key", json.RawMessage(`{}`))
	assert.ErrorIs(t, err, authnext.ErrPasskeyChallenge)
	_, err = service.FinishPasskeyLogin(t.Context(), "https://evil.example", "challenge", json.RawMessage(`{}`))
	assert.ErrorIs(t, err, ErrCSRF)
	_, err = service.FinishPasskeyLogin(t.Context(), "https://app.example", "challenge", nil)
	assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
	_, err = service.FinishPasskeyLogin(t.Context(), "https://app.example", "", json.RawMessage(`{}`))
	assert.ErrorIs(t, err, authnext.ErrPasskeyChallenge)
}

func TestPasskeyCredentialCommit(t *testing.T) {
	service, principalID := newLocalService(t)
	now := time.Unix(2_000, 0).UTC()
	credential := webauthnxCredential([]byte("committed-credential"), 12, false)
	view, err := service.storePasskeyCredential(t.Context(), principalID, "Security key", credential, now)
	require.NoError(t, err)
	assert.Equal(t, passkeyIDHash(credential.ID), view.ID)
	assert.Equal(t, "Security key", view.Name)
	assert.Equal(t, uint32(12), view.SignCount)
	assert.Equal(t, now, view.CreatedAt)

	var row passkeyRow
	require.NoError(t, service.db.NewSelect().Model(&row).Where("id_hash = ?", view.ID).Scan(t.Context()))
	plain, err := service.decryptAuthnData(passkeyCipher, passkeyAAD(principalID, row.IDHash), row.CredentialCiphertext)
	require.NoError(t, err)
	assert.JSONEq(t, string(credential.Data), string(plain))

	service.cfg.Passkey.MaxCredentials = 1
	_, err = service.storePasskeyCredential(t.Context(), principalID, "Second", webauthnxCredential([]byte("second"), 0, false), now)
	assert.ErrorIs(t, err, authnext.ErrPasskeyLimit)

	service.cfg.Passkey.MaxCredentials = 10
	_, err = service.storePasskeyCredential(t.Context(), principalID, "Duplicate", credential, now)
	assert.ErrorContains(t, err, "store passkey")
}

func TestPasskeyLoginCommitAndCounterProtection(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		service, principalID := newLocalService(t)
		now := time.Unix(3_000, 0).UTC()
		credential := webauthnxCredential([]byte("login-credential"), 8, false)
		idHash := passkeyIDHash(credential.ID)
		ciphertext, err := service.encryptAuthnData(passkeyCipher, passkeyAAD(principalID, idHash), webauthnxCredential(credential.ID, 7, false).Data)
		require.NoError(t, err)
		row := passkeyRow{IDHash: idHash, PrincipalID: principalID, Name: "Key", CredentialCiphertext: ciphertext, SignCount: 7, CreatedAt: now.Add(-time.Hour).UnixMilli(), UpdatedAt: now.Add(-time.Hour).UnixMilli()}
		_, err = service.db.NewInsert().Model(&row).Exec(t.Context())
		require.NoError(t, err)

		sessionID, err := service.completePasskeyLogin(t.Context(), principalID, row, credential, now)
		require.NoError(t, err)
		assert.NotEmpty(t, sessionID)
		require.NoError(t, service.db.NewSelect().Model(&row).Where("id_hash = ?", idHash).Scan(t.Context()))
		assert.Equal(t, uint32(8), row.SignCount)
		assert.Equal(t, now.UnixMilli(), row.LastUsedAt)
		var sessions int
		sessions, err = service.db.NewSelect().Model((*sessionRow)(nil)).Where("principal_id = ?", principalID).Count(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 1, sessions)
	})

	t.Run("stale counter", func(t *testing.T) {
		service, principalID := newLocalService(t)
		now := time.Unix(4_000, 0).UTC()
		credential := webauthnxCredential([]byte("stale-credential"), 10, false)
		idHash := passkeyIDHash(credential.ID)
		ciphertext, err := service.encryptAuthnData(passkeyCipher, passkeyAAD(principalID, idHash), credential.Data)
		require.NoError(t, err)
		stored := passkeyRow{IDHash: idHash, PrincipalID: principalID, Name: "Key", CredentialCiphertext: ciphertext, SignCount: 9, CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli()}
		_, err = service.db.NewInsert().Model(&stored).Exec(t.Context())
		require.NoError(t, err)
		stale := stored
		stale.SignCount = 8
		_, err = service.completePasskeyLogin(t.Context(), principalID, stale, credential, now)
		assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
	})

	t.Run("clone warning", func(t *testing.T) {
		service, principalID := newLocalService(t)
		now := time.Unix(5_000, 0).UTC()
		credential := webauthnxCredential([]byte("cloned-credential"), 3, true)
		idHash := passkeyIDHash(credential.ID)
		row := passkeyRow{IDHash: idHash, PrincipalID: principalID, Name: "Key", CredentialCiphertext: "old", SignCount: 3, CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli()}
		_, err := service.db.NewInsert().Model(&row).Exec(t.Context())
		require.NoError(t, err)
		_, err = service.completePasskeyLogin(t.Context(), principalID, row, credential, now)
		assert.ErrorIs(t, err, authnext.ErrPasskeyCredential)
		count, countErr := service.db.NewSelect().Model((*sessionRow)(nil)).Where("principal_id = ?", principalID).Count(t.Context())
		require.NoError(t, countErr)
		assert.Zero(t, count)
		var auditCount int
		auditCount, err = service.db.NewSelect().Table("iam_audit_events").Where("principal_id = ? AND event_type = ?", principalID, "passkey_counter_regression").Count(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 1, auditCount)
	})
}

func webauthnxCredential(id []byte, signCount uint32, cloneWarning bool) webauthnx.Credential {
	stored := webauthn.Credential{
		ID: id, PublicKey: []byte{1, 2, 3},
		Flags:         webauthn.NewCredentialFlags(protocol.FlagUserPresent | protocol.FlagUserVerified),
		Authenticator: webauthn.Authenticator{SignCount: signCount, CloneWarning: cloneWarning},
	}
	data, err := json.Marshal(stored)
	if err != nil {
		panic(err)
	}
	return webauthnx.Credential{ID: append([]byte(nil), id...), Data: data, SignCount: signCount, CloneWarning: cloneWarning}
}

func authenticatedCookie(t *testing.T, service *WebService) string {
	t.Helper()
	token, _, err := service.Login(t.Context(), "admin", "correct horse battery staple", "")
	require.NoError(t, err)
	return service.SessionCookie(token)
}
