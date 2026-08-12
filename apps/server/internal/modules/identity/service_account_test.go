package identity

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/oauth"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestServiceAccountHTTPAndOAuthLifecycle(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, testID("tenant")))

	identityService := newIdentityService(db)
	authentication := newServiceAccountAuthentication(t, db)
	oauthService := oauth.NewService(db, authentication, newTestIDGenerator())
	_, api := humatest.New(t)
	registrar := authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry())
	RegisterREST(api, identityService, registrar)
	oauth.RegisterREST(api, oauthService, nil)
	tenant := authz.TenantHeader + ": " + wireID("tenant")
	doc := api.OpenAPI()
	assert.Contains(t, doc.Paths["/iam/service-accounts"].Post.Responses, "409")
	for _, operation := range []map[string]*huma.Response{
		doc.Paths["/iam/service-accounts/{id}"].Get.Responses,
		doc.Paths["/iam/service-accounts/{id}"].Put.Responses,
		doc.Paths["/iam/service-accounts/{id}"].Delete.Responses,
		doc.Paths["/iam/service-accounts/{id}/credentials"].Get.Responses,
		doc.Paths["/iam/service-accounts/{id}/credentials"].Post.Responses,
		doc.Paths["/iam/service-accounts/{id}/credentials/{credential_id}"].Delete.Responses,
	} {
		assert.Contains(t, operation, "404")
	}
	assert.Contains(t, doc.Paths["/iam/service-accounts/{id}"].Put.Responses, "409")
	assert.Contains(t, doc.Paths["/iam/service-accounts/{id}"].Delete.Responses, "409")
	assert.Contains(t, doc.Paths["/iam/service-accounts/{id}/credentials"].Post.Responses, "409")

	created := api.Post("/iam/service-accounts", tenant, map[string]any{
		"login_name": "report-worker", "display_name": "Report Worker", "description": "Builds tenant reports",
	})
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	var createdEnvelope struct {
		Data ServiceAccount `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &createdEnvelope))
	account := createdEnvelope.Data
	require.NotEmpty(t, account.ID)
	assert.Equal(t, int64(1), account.Version)
	listed, total, err := identityService.ListServiceAccounts(t.Context(), testID("tenant"), "report", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, listed, 1)
	assert.Equal(t, account.ID, listed[0].ID)
	empty, total, err := identityService.ListServiceAccounts(t.Context(), testID("tenant"), "missing", 10, 0)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, empty)

	principals := api.Get("/iam/principals", tenant)
	require.Equal(t, http.StatusOK, principals.Code, principals.Body.String())
	assert.Contains(t, principals.Body.String(), `"items":[]`)
	_, loginErr := authentication.BeginLogin(t.Context(), account.LoginName, "irrelevant password", "/")
	assert.ErrorIs(t, loginErr, authnext.ErrInvalidCredentials)

	credentialResponse := api.Post("/iam/service-accounts/"+account.ID.String()+"/credentials", tenant, map[string]any{
		"name": "automation", "scopes": []string{"reports.read", "reports.write"},
	})
	require.Equal(t, http.StatusOK, credentialResponse.Code, credentialResponse.Body.String())
	var credentialEnvelope struct {
		Data ServiceAccountCredentialSecret `json:"data"`
	}
	require.NoError(t, json.Unmarshal(credentialResponse.Body.Bytes(), &credentialEnvelope))
	credential := credentialEnvelope.Data
	require.NotEmpty(t, credential.Credential.ID)
	require.NotEmpty(t, credential.Secret)

	listedCredentials := api.Get("/iam/service-accounts/"+account.ID.String()+"/credentials", tenant)
	require.Equal(t, http.StatusOK, listedCredentials.Code, listedCredentials.Body.String())
	assert.NotContains(t, listedCredentials.Body.String(), credential.Secret)
	assert.NotContains(t, listedCredentials.Body.String(), "secret_hash")

	token := exchangeServiceAccountToken(t, api, guidString(credential.Credential.ID), credential.Secret, "reports.read")
	claims, err := authentication.Authenticate(t.Context(), "Bearer "+token.AccessToken, "")
	require.NoError(t, err)
	assert.Equal(t, guidString(account.ID), claims.Subject)
	assert.Equal(t, authnext.SubjectTypeServiceAccount, claims.SubjectType)
	assert.Equal(t, wireID("tenant"), claims.OrganizationID)

	revoked := api.Delete("/iam/service-accounts/"+account.ID.String()+"/credentials/"+credential.Credential.ID.String(), tenant)
	require.Equal(t, http.StatusOK, revoked.Code, revoked.Body.String())
	_, err = authentication.Authenticate(t.Context(), "Bearer "+token.AccessToken, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
	assert.Equal(t, http.StatusBadRequest, exchangeServiceAccountTokenResponse(api, guidString(credential.Credential.ID), credential.Secret, "reports.read").Code)

	secondResponse := api.Post("/iam/service-accounts/"+account.ID.String()+"/credentials", tenant, map[string]any{
		"name": "replacement", "scopes": []string{"reports.read"},
	})
	require.Equal(t, http.StatusOK, secondResponse.Code, secondResponse.Body.String())
	require.NoError(t, json.Unmarshal(secondResponse.Body.Bytes(), &credentialEnvelope))
	second := credentialEnvelope.Data
	activeToken := exchangeServiceAccountToken(t, api, guidString(second.Credential.ID), second.Secret, "")

	disabled := api.Put("/iam/service-accounts/"+account.ID.String(), tenant, map[string]any{
		"display_name": account.DisplayName, "description": account.Description, "status": "disabled", "version": account.Version,
	})
	require.Equal(t, http.StatusOK, disabled.Code, disabled.Body.String())
	var accountEnvelope struct {
		Data ServiceAccount `json:"data"`
	}
	require.NoError(t, json.Unmarshal(disabled.Body.Bytes(), &accountEnvelope))
	account = accountEnvelope.Data
	_, err = authentication.Authenticate(t.Context(), "Bearer "+activeToken.AccessToken, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)
	assert.Equal(t, http.StatusBadRequest, exchangeServiceAccountTokenResponse(api, guidString(second.Credential.ID), second.Secret, "").Code)

	restored := api.Put("/iam/service-accounts/"+account.ID.String(), tenant, map[string]any{
		"display_name": account.DisplayName, "description": account.Description, "status": "active", "version": account.Version,
	})
	require.Equal(t, http.StatusOK, restored.Code, restored.Body.String())
	require.NoError(t, json.Unmarshal(restored.Body.Bytes(), &accountEnvelope))
	account = accountEnvelope.Data
	activeToken = exchangeServiceAccountToken(t, api, guidString(second.Credential.ID), second.Secret, "reports.read")
	now := time.Now().UTC().UnixMilli()
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_roles (tenant_id,id,name,description,created_at,updated_at) VALUES (?,?,?,'',?,?)`, testID("tenant"), testID("administrator"), "Administrator", now, now)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_role_permissions (tenant_id,role_id,permission_code,created_at) VALUES (?,?, 'tenant_administer',?)`, testID("tenant"), testID("administrator"), now)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_role_members (tenant_id,role_id,principal_id,created_at) VALUES (?,?,?,?)`, testID("tenant"), testID("administrator"), account.ID, now)
	require.NoError(t, err)
	lastAdministrator := api.Put("/iam/service-accounts/"+account.ID.String(), tenant, map[string]any{
		"display_name": account.DisplayName, "description": account.Description, "status": "disabled", "version": account.Version,
	})
	assert.Equal(t, http.StatusConflict, lastAdministrator.Code, lastAdministrator.Body.String())
	assert.Contains(t, lastAdministrator.Body.String(), "last_tenant_administrator")
	backup, err := identityService.Create(t.Context(), testID("tenant"), "backup-admin", "backup administrator password", "Backup Admin", "")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO iam_role_members (tenant_id,role_id,principal_id,created_at) VALUES (?,?,?,?)`, testID("tenant"), testID("administrator"), backup.ID, now)
	require.NoError(t, err)

	stale := api.Put("/iam/service-accounts/"+account.ID.String(), tenant, map[string]any{
		"display_name": account.DisplayName, "description": "stale", "status": "active", "version": account.Version - 1,
	})
	assert.Equal(t, http.StatusConflict, stale.Code, stale.Body.String())
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/service-accounts/"+account.ID.String(), authz.TenantHeader+": "+wireID("other")).Code)

	deleted := api.Delete("/iam/service-accounts/"+account.ID.String()+"?version="+fmtInt(account.Version), tenant)
	require.Equal(t, http.StatusOK, deleted.Code, deleted.Body.String())
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/service-accounts/"+account.ID.String(), tenant).Code)
	_, err = authentication.Authenticate(t.Context(), "Bearer "+activeToken.AccessToken, "")
	assert.ErrorIs(t, err, authnext.ErrInvalidToken)

	var passwordCredentials int
	require.NoError(t, db.NewSelect().Table("iam_credentials").Where("principal_id = ?", account.ID).ColumnExpr("COUNT(*)").Scan(t.Context(), &passwordCredentials))
	assert.Zero(t, passwordCredentials)
	auditEvents, err := db.NewSelect().Table("iam_audit_events").Where("target_id = ? OR detail LIKE ?", account.ID, "%"+account.ID.String()+"%").Count(t.Context())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, auditEvents, 6)
	var auditDetail string
	require.NoError(t, db.NewSelect().Table("iam_audit_events").Column("detail").Where("event_type = 'service_account_credential_created'").Limit(1).Scan(t.Context(), &auditDetail))
	assert.NotContains(t, auditDetail, credential.Secret)
}

func TestServiceAccountTransactionsAndValidation(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, testID("tenant")))
	service := newIdentityService(db)

	_, err = service.CreateServiceAccount(t.Context(), testID("missing"), "worker", "Worker", "", nil)
	assert.ErrorIs(t, err, ErrServiceAccountInvalid)
	_, err = service.CreateServiceAccount(t.Context(), testID("tenant"), "", "", "", nil)
	assert.ErrorIs(t, err, ErrServiceAccountInvalid)
	past := time.Now().UTC().Add(-time.Minute)
	_, err = service.CreateServiceAccount(t.Context(), testID("tenant"), "worker", "Worker", "", &past)
	assert.ErrorIs(t, err, ErrServiceAccountInvalid)

	rejectIdentityAuditEvent(t, db, "service_account_created")
	_, err = service.CreateServiceAccount(t.Context(), testID("tenant"), "rollback", "Rollback", "", nil)
	require.ErrorContains(t, err, "append audit event")
	var count int
	require.NoError(t, db.NewSelect().Table("iam_principals").Where("login_name = 'rollback'").ColumnExpr("COUNT(*)").Scan(t.Context(), &count))
	assert.Zero(t, count)
}

func TestServiceAccountCredentialLimitsAndRollbacks(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, testID("tenant")))
	service := newIdentityService(db)

	accountExpiry := time.Now().UTC().Add(2 * time.Hour)
	account, err := service.CreateServiceAccount(t.Context(), testID("tenant"), "batch-worker", "Batch Worker", "", &accountExpiry)
	require.NoError(t, err)
	require.NotNil(t, account.ExpiresAt)

	_, _, err = service.ListServiceAccounts(t.Context(), 0, "", 50, 0)
	assert.ErrorIs(t, err, ErrServiceAccountInvalid)
	_, err = service.GetServiceAccount(t.Context(), testID("tenant"), testID("missing"))
	assert.ErrorIs(t, err, ErrServiceAccountNotFound)
	_, err = service.ReplaceServiceAccount(t.Context(), testID("tenant"), account.ID, "Batch Worker", "", "active", nil, 0)
	assert.ErrorIs(t, err, ErrServiceAccountInvalid)
	assert.ErrorIs(t, service.DeleteServiceAccount(t.Context(), testID("tenant"), account.ID, 0), ErrServiceAccountInvalid)
	_, err = service.CreateServiceAccountCredential(t.Context(), testID("tenant"), account.ID, "", []string{"jobs.run"}, nil)
	assert.ErrorIs(t, err, ErrServiceAccountInvalid)
	_, err = service.CreateServiceAccountCredential(t.Context(), testID("tenant"), account.ID, "invalid", nil, nil)
	assert.ErrorIs(t, err, ErrServiceAccountInvalid)
	_, err = service.CreateServiceAccountCredential(t.Context(), testID("tenant"), account.ID, "invalid", []string{"jobs run"}, nil)
	assert.ErrorIs(t, err, ErrServiceAccountInvalid)
	past := time.Now().UTC().Add(-time.Minute)
	_, err = service.CreateServiceAccountCredential(t.Context(), testID("tenant"), account.ID, "expired", []string{"jobs.run"}, &past)
	assert.ErrorIs(t, err, ErrServiceAccountInvalid)
	beyondAccountExpiry := accountExpiry.Add(time.Hour)
	_, err = service.CreateServiceAccountCredential(t.Context(), testID("tenant"), account.ID, "too-long", []string{"jobs.run"}, &beyondAccountExpiry)
	assert.ErrorIs(t, err, ErrServiceAccountInvalid)
	assert.ErrorIs(t, service.RevokeServiceAccountCredential(t.Context(), 0, account.ID, testID("credential")), ErrServiceAccountInvalid)

	credentialExpiry := time.Now().UTC().Add(time.Hour)
	rejectIdentityAuditEvent(t, db, "service_account_credential_created")
	_, err = service.CreateServiceAccountCredential(t.Context(), testID("tenant"), account.ID, "rollback", []string{"jobs.run"}, &credentialExpiry)
	require.ErrorContains(t, err, "append audit event")
	var count int
	require.NoError(t, db.NewSelect().Table("iam_service_account_credentials").Where("principal_id = ?", account.ID).ColumnExpr("COUNT(*)").Scan(t.Context(), &count))
	assert.Zero(t, count)
	_, err = db.ExecContext(t.Context(), "DROP TRIGGER reject_identity_audit")
	require.NoError(t, err)

	credential, err := service.CreateServiceAccountCredential(t.Context(), testID("tenant"), account.ID, "primary", []string{"jobs.write", "jobs.read", "jobs.read"}, &credentialExpiry)
	require.NoError(t, err)
	assert.Equal(t, []string{"jobs.read", "jobs.write"}, credential.Credential.Scopes)
	require.NotNil(t, credential.Credential.ExpiresAt)

	rejectIdentityAuditEvent(t, db, "service_account_credential_revoked")
	err = service.RevokeServiceAccountCredential(t.Context(), testID("tenant"), account.ID, credential.Credential.ID)
	require.ErrorContains(t, err, "append audit event")
	credentials, err := service.ListServiceAccountCredentials(t.Context(), testID("tenant"), account.ID)
	require.NoError(t, err)
	require.Len(t, credentials, 1)
	assert.Nil(t, credentials[0].RevokedAt)
	_, err = db.ExecContext(t.Context(), "DROP TRIGGER reject_identity_audit")
	require.NoError(t, err)

	rejectIdentityAuditEvent(t, db, "service_account_deleted")
	err = service.DeleteServiceAccount(t.Context(), testID("tenant"), account.ID, account.Version)
	require.ErrorContains(t, err, "append audit event")
	current, err := service.GetServiceAccount(t.Context(), testID("tenant"), account.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", current.Status)
	assert.Equal(t, account.Version, current.Version)
	_, err = db.ExecContext(t.Context(), "DROP TRIGGER reject_identity_audit")
	require.NoError(t, err)

	assert.ErrorIs(t, service.RevokeServiceAccountCredential(t.Context(), testID("tenant"), account.ID, testID("missing")), ErrCredentialNotFound)
	require.NoError(t, service.RevokeServiceAccountCredential(t.Context(), testID("tenant"), account.ID, credential.Credential.ID))
	require.NoError(t, service.RevokeServiceAccountCredential(t.Context(), testID("tenant"), account.ID, credential.Credential.ID))
	_, err = service.ListServiceAccountCredentials(t.Context(), testID("tenant"), testID("missing"))
	assert.ErrorIs(t, err, ErrServiceAccountNotFound)
	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER reject_service_account_delete BEFORE UPDATE ON iam_service_accounts
		WHEN NEW.status = 'deleted' BEGIN SELECT RAISE(ABORT, 'forced delete failure'); END`)
	require.NoError(t, err)
	assert.Error(t, service.DeleteServiceAccount(t.Context(), testID("tenant"), account.ID, account.Version))
	_, err = db.ExecContext(t.Context(), "DROP TRIGGER reject_service_account_delete")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER reject_service_account_credential BEFORE INSERT ON iam_service_account_credentials
		BEGIN SELECT RAISE(ABORT, 'forced credential failure'); END`)
	require.NoError(t, err)
	_, err = service.CreateServiceAccountCredential(t.Context(), testID("tenant"), account.ID, "rejected", []string{"jobs.run"}, nil)
	assert.Error(t, err)
	_, err = db.ExecContext(t.Context(), "DROP TRIGGER reject_service_account_credential")
	require.NoError(t, err)

	now := time.Now().UTC().UnixMilli()
	for index := range maxServiceAccountCredentials {
		_, err = db.ExecContext(t.Context(), `INSERT INTO iam_service_account_credentials
			(id,principal_id,name,secret_hash,scopes,created_at) VALUES (?,?,?,?,?,?)`,
			"seed-"+strconv.Itoa(index), account.ID, "seed", "unused", "jobs.run", now+int64(index))
		require.NoError(t, err)
	}
	_, api := humatest.New(t)
	RegisterREST(api, service, authz.NewDeclarationOnlyRegistrar(authz.DefaultRegistry()))
	tenant := authz.TenantHeader + ": " + wireID("tenant")
	assert.Equal(t, http.StatusOK, api.Get("/iam/service-accounts", tenant).Code)
	assert.Equal(t, http.StatusOK, api.Get("/iam/service-accounts/"+account.ID.String(), tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Delete("/iam/service-accounts/"+wireID("missing")+"?version=1", tenant).Code)
	assert.Equal(t, http.StatusNotFound, api.Get("/iam/service-accounts/"+wireID("missing")+"/credentials", tenant).Code)
	assert.ErrorIs(t, service.DeleteServiceAccount(t.Context(), testID("tenant"), account.ID, account.Version+1), ErrServiceAccountConflict)
	_, err = service.GetServiceAccount(t.Context(), 0, account.ID)
	assert.ErrorIs(t, err, ErrServiceAccountInvalid)

	response := api.Post("/iam/service-accounts", tenant, map[string]any{"login_name": account.LoginName, "display_name": "Duplicate"})
	assert.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "login_name_exists")
	response = api.Delete("/iam/service-accounts/"+account.ID.String()+"/credentials/"+wireID("missing"), tenant)
	assert.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "service_account_credential_not_found")
	response = api.Post("/iam/service-accounts/"+account.ID.String()+"/credentials", tenant, map[string]any{"name": "overflow", "scopes": []string{"jobs.run"}})
	assert.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "service_account_credential_limit")
	response = api.Post("/iam/service-accounts", tenant, map[string]any{"login_name": "expired-worker", "expires_at": past})
	assert.Equal(t, http.StatusUnprocessableEntity, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "invalid_service_account")

	require.NoError(t, db.Close())
	response = api.Get("/iam/service-accounts", tenant)
	assert.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "identity_unavailable")
	_, err = service.GetServiceAccount(t.Context(), testID("tenant"), account.ID)
	assert.Error(t, err)
	_, err = service.ReplaceServiceAccount(t.Context(), testID("tenant"), account.ID, "Batch Worker", "", "active", &accountExpiry, account.Version)
	assert.Error(t, err)
	assert.Error(t, service.DeleteServiceAccount(t.Context(), testID("tenant"), account.ID, account.Version))
	_, err = service.CreateServiceAccountCredential(t.Context(), testID("tenant"), account.ID, "closed", []string{"jobs.run"}, nil)
	assert.Error(t, err)
	assert.Error(t, service.RevokeServiceAccountCredential(t.Context(), testID("tenant"), account.ID, testID("closed")))
}

func newServiceAccountAuthentication(t *testing.T, db *bun.DB) *authnmod.WebService {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = byte(index + 1)
	}
	encoded := base64.RawStdEncoding.EncodeToString(seed)
	service, err := authnmod.NewWebService(authnext.Config{
		Enabled: true, Issuer: "https://iam.example", Audience: []string{"chaosplus-api"}, SigningKey: encoded, AccessTokenTTL: time.Minute,
		Web: authnext.WebConfig{Enabled: true, CookieName: "session", CookieSecure: false, PostLoginURL: "/", AllowedReturnURLs: []string{"/"}, SessionTTL: time.Hour, IdleTTL: time.Minute},
		MFA: authnext.MFAConfig{EncryptionKey: encoded},
	}, db, authnmod.WithIDGenerator(newTestIDGenerator()))
	require.NoError(t, err)
	return service
}

func exchangeServiceAccountToken(t *testing.T, api humatest.TestAPI, id, secret, scope string) oauth.TokenResponse {
	t.Helper()
	response := exchangeServiceAccountTokenResponse(api, id, secret, scope)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var token oauth.TokenResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &token))
	require.NotEmpty(t, token.AccessToken)
	return token
}

func exchangeServiceAccountTokenResponse(api humatest.TestAPI, id, secret, scope string) *httptest.ResponseRecorder {
	basic := base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(id) + ":" + url.QueryEscape(secret)))
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {scope}}
	return api.Post("/oauth/token", "Authorization: Basic "+basic, "Content-Type: application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
}

func fmtInt(value int64) string { return strconv.FormatInt(value, 10) }

func TestServiceAccountSurfacesDatabaseErrors(t *testing.T) {
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(t.Context(), db))
	require.NoError(t, organization.Migrate(t.Context(), db))
	require.NoError(t, organization.EnsureTenant(t.Context(), db, testID("tenant")))
	service := newIdentityService(db)
	require.NoError(t, db.Close())

	_, err = service.CreateServiceAccount(t.Context(), testID("tenant"), "worker", "Worker", "", nil)
	assert.Error(t, err)
	_, _, err = service.ListServiceAccounts(t.Context(), testID("tenant"), "", 10, 0)
	assert.Error(t, err)
	_, err = service.ReplaceServiceAccount(t.Context(), testID("tenant"), testID("id"), "Worker", "", "active", nil, 1)
	assert.Error(t, err)
	assert.Error(t, service.DeleteServiceAccount(t.Context(), testID("tenant"), testID("id"), 1))
	_, err = service.CreateServiceAccountCredential(t.Context(), testID("tenant"), testID("id"), "key", []string{"a"}, nil)
	assert.Error(t, err)
	_, err = service.ListServiceAccountCredentials(t.Context(), testID("tenant"), testID("id"))
	assert.Error(t, err)
	assert.Error(t, service.RevokeServiceAccountCredential(t.Context(), testID("tenant"), testID("id"), testID("cred")))
}
