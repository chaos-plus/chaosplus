package authn_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/bunx/bunxtest"
	authnmod "github.com/chaos-plus/chaosplus/internal/modules/authn"
	"github.com/chaos-plus/chaosplus/internal/modules/iam"
	"github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/go-chi/chi/v5"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestAuthenticationHTTPFlow(t *testing.T) {
	db, web, principalID := newAuthenticationService(t)
	_, api := humatest.New(t)
	authnmod.RegisterREST(api, web, web)

	login := api.Post("/authn/login", map[string]any{
		"login_name": "alice", "password": "correct horse battery staple", "return_url": "https://app.example/",
	}, "Origin: https://app.example")
	require.Equal(t, http.StatusOK, login.Code, login.Body.String())
	cookie := strings.Split(login.Header().Get("Set-Cookie"), ";")[0]
	require.NotEmpty(t, cookie)

	session := api.Get("/authn/session", "Cookie: "+cookie)
	assert.Equal(t, http.StatusOK, session.Code, session.Body.String())
	assert.Contains(t, session.Body.String(), principalID)
	assert.Contains(t, session.Body.String(), `"email_verified":true`)

	accessToken, _, err := web.IssueAccessToken(context.Background(), principalID, "api", "openid")
	require.NoError(t, err)
	me := api.Get("/authn/me", "Authorization: Bearer "+accessToken)
	assert.Equal(t, http.StatusOK, me.Code, me.Body.String())

	logout := api.Post("/authn/logout", "Cookie: "+cookie, "Origin: https://app.example")
	assert.Equal(t, http.StatusOK, logout.Code, logout.Body.String())
	assert.Contains(t, logout.Header().Get("Set-Cookie"), "Max-Age=0")
	assert.Equal(t, http.StatusUnauthorized, api.Get("/authn/session", "Cookie: "+cookie).Code)

	assert.Equal(t, http.StatusUnauthorized, api.Post("/authn/login", map[string]any{
		"login_name": "alice", "password": "wrong password",
	}, "Origin: https://app.example").Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/authn/login", map[string]any{
		"login_name": "alice", "password": "correct horse battery staple", "return_url": "https://app.example/not-allowed",
	}, "Origin: https://app.example").Code)
	assert.Equal(t, http.StatusForbidden, api.Post("/authn/login", map[string]any{
		"login_name": "alice", "password": "correct horse battery staple",
	}, "Origin: https://evil.example").Code)
	assert.Equal(t, http.StatusForbidden, api.Post("/authn/logout", "Cookie: "+cookie, "Origin: https://evil.example").Code)

	_, err = db.NewUpdate().Table("iam_credentials").Set("mfa_required = ?", true).Where("principal_id = ?", principalID).Exec(context.Background())
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, api.Post("/authn/login", map[string]any{
		"login_name": "alice", "password": "correct horse battery staple",
	}, "Origin: https://app.example").Code)
	assert.Equal(t, http.StatusUnauthorized, api.Get("/authn/me", "Authorization: Bearer invalid").Code)
}

func TestAuthenticationSecurityCenterHTTPFlow(t *testing.T) {
	_, web, _ := newAuthenticationService(t)
	_, api := humatest.New(t)
	authnmod.RegisterREST(api, web, web)
	login := func(password string) string {
		response := api.Post("/authn/login", map[string]any{
			"login_name": "alice", "password": password, "return_url": "https://app.example/",
		}, "Origin: https://app.example")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		return strings.Split(response.Header().Get("Set-Cookie"), ";")[0]
	}
	first, second := login("correct horse battery staple"), login("correct horse battery staple")
	assert.Equal(t, http.StatusUnauthorized, api.Get("/authn/sessions").Code)
	sessions := api.Get("/authn/sessions", "Cookie: "+first)
	require.Equal(t, http.StatusOK, sessions.Code, sessions.Body.String())
	var envelope struct {
		Data []authn.BrowserSession `json:"data"`
	}
	require.NoError(t, json.Unmarshal(sessions.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data, 2)
	var other string
	for _, session := range envelope.Data {
		if !session.Current {
			other = session.ID
		}
	}
	require.NotEmpty(t, other)
	assert.Equal(t, http.StatusForbidden, api.Delete("/authn/sessions/"+other, "Cookie: "+first, "Origin: https://evil.example").Code)
	assert.Equal(t, http.StatusOK, api.Delete("/authn/sessions/"+other, "Cookie: "+first, "Origin: https://app.example").Code)
	assert.Equal(t, http.StatusUnauthorized, api.Get("/authn/session", "Cookie: "+second).Code)
	assert.Equal(t, http.StatusNotFound, api.Delete("/authn/sessions/"+strings.Repeat("a", 64), "Cookie: "+first, "Origin: https://app.example").Code)

	changed := api.Post("/authn/password/change", "Cookie: "+first, "Origin: https://app.example", map[string]any{
		"current_password": "correct horse battery staple", "new_password": "new correct horse battery staple",
	})
	assert.Equal(t, http.StatusOK, changed.Code, changed.Body.String())
	assert.Equal(t, http.StatusForbidden, api.Post("/authn/password/change", "Cookie: "+first, "Origin: https://evil.example", map[string]any{
		"current_password": "new correct horse battery staple", "new_password": "another correct horse battery staple",
	}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/authn/password/change", "Cookie: "+first, "Origin: https://app.example", map[string]any{
		"current_password": "new correct horse battery staple", "new_password": "correct horse battery staple",
	}).Code)
	assert.Equal(t, http.StatusUnauthorized, api.Post("/authn/password/change", "Cookie: "+first, "Origin: https://app.example", map[string]any{
		"current_password": "wrong password", "new_password": "another correct horse battery staple",
	}).Code)
	third := login("new correct horse battery staple")
	assert.Equal(t, http.StatusForbidden, api.Post("/authn/logout-all", "Cookie: "+third, "Origin: https://evil.example").Code)
	logoutAll := api.Post("/authn/logout-all", "Cookie: "+third, "Origin: https://app.example")
	assert.Equal(t, http.StatusOK, logoutAll.Code, logoutAll.Body.String())
	assert.Equal(t, http.StatusUnauthorized, api.Get("/authn/session", "Cookie: "+first).Code)
	assert.Equal(t, http.StatusUnauthorized, api.Get("/authn/session", "Cookie: "+third).Code)
}

func TestPasswordRecoveryHTTPFlow(t *testing.T) {
	deliveries := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Type        string `json:"type"`
			RecoveryURL string `json:"recovery_url"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		if payload.Type == "password_recovery" {
			parsed, err := url.Parse(payload.RecoveryURL)
			require.NoError(t, err)
			deliveries <- parsed.Query().Get("token")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	_, web, _ := newRecoveryAuthenticationService(t, server.URL)
	require.NoError(t, web.StartNotificationWorker(t.Context()))
	t.Cleanup(func() { _ = web.StopNotificationWorker(context.Background()) })
	_, api := humatest.New(t)
	authnmod.RegisterREST(api, web, web)

	assert.Equal(t, http.StatusForbidden, api.Post("/authn/password/recovery/start", map[string]any{"identifier": "alice"}, "Origin: https://evil.example").Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/authn/password/recovery/start", map[string]any{}, "Origin: https://app.example").Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/authn/password/recovery/start", "Content-Type: application/json", "Origin: https://app.example", strings.NewReader(`"not-an-object"`)).Code)
	missing := api.Post("/authn/password/recovery/start", map[string]any{"identifier": "missing@example.com"}, "Origin: https://app.example")
	assert.Equal(t, http.StatusAccepted, missing.Code, missing.Body.String())
	started := api.Post("/authn/password/recovery/start", map[string]any{"identifier": "alice@example.com"}, "Origin: https://app.example")
	require.Equal(t, http.StatusAccepted, started.Code, started.Body.String())

	var token string
	select {
	case token = <-deliveries:
	case <-time.After(3 * time.Second):
		t.Fatal("recovery webhook was not delivered")
	}
	assert.Equal(t, http.StatusBadRequest, api.Post("/authn/password/recovery/complete", map[string]any{
		"token": strings.Repeat("x", 40), "new_password": "new correct horse battery staple",
	}, "Origin: https://app.example").Code)
	assert.Equal(t, http.StatusConflict, api.Post("/authn/password/recovery/complete", map[string]any{
		"token": token, "new_password": "correct horse battery staple",
	}, "Origin: https://app.example").Code)
	completed := api.Post("/authn/password/recovery/complete", map[string]any{
		"token": token, "new_password": "new correct horse battery staple",
	}, "Origin: https://app.example")
	assert.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	assert.Equal(t, http.StatusForbidden, api.Post("/authn/password/recovery/complete", map[string]any{
		"token": token, "new_password": "another correct horse battery staple",
	}, "Origin: https://evil.example").Code)

	startOperation := api.OpenAPI().Paths["/authn/password/recovery/start"].Post
	completeOperation := api.OpenAPI().Paths["/authn/password/recovery/complete"].Post
	require.NotNil(t, startOperation)
	require.NotNil(t, completeOperation)
	assert.Equal(t, "authn-start-password-recovery", startOperation.OperationID)
	assert.Equal(t, "authn-complete-password-recovery", completeOperation.OperationID)
	assert.Contains(t, startOperation.Responses, "202")
}

func TestEmailVerificationHTTPFlow(t *testing.T) {
	deliveries := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var payload struct {
			Type            string `json:"type"`
			VerificationURL string `json:"verification_url"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		if payload.Type == "email_verification" {
			parsed, err := url.Parse(payload.VerificationURL)
			require.NoError(t, err)
			deliveries <- parsed.Query().Get("token")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	db, web, principalID := newRecoveryAuthenticationService(t, server.URL)
	_, err := db.NewUpdate().Table("iam_principals").Set("email_verified = ?", false).Where("id = ?", principalID).Exec(t.Context())
	require.NoError(t, err)
	require.NoError(t, web.StartNotificationWorker(t.Context()))
	t.Cleanup(func() { _ = web.StopNotificationWorker(context.Background()) })
	_, api := humatest.New(t)
	authnmod.RegisterREST(api, web, web)

	login := api.Post("/authn/login", map[string]any{
		"login_name": "alice", "password": "correct horse battery staple", "return_url": "https://app.example/",
	}, "Origin: https://app.example")
	require.Equal(t, http.StatusOK, login.Code, login.Body.String())
	cookie := strings.Split(login.Header().Get("Set-Cookie"), ";")[0]
	assert.Contains(t, api.Get("/authn/session", "Cookie: "+cookie).Body.String(), `"email_verified":false`)
	assert.Equal(t, http.StatusUnauthorized, api.Post("/authn/email/verification/start", "Origin: https://app.example").Code)
	assert.Equal(t, http.StatusForbidden, api.Post("/authn/email/verification/start", "Cookie: "+cookie, "Origin: https://evil.example").Code)
	started := api.Post("/authn/email/verification/start", "Cookie: "+cookie, "Origin: https://app.example")
	require.Equal(t, http.StatusAccepted, started.Code, started.Body.String())

	var token string
	select {
	case token = <-deliveries:
	case <-time.After(3 * time.Second):
		t.Fatal("email verification webhook was not delivered")
	}
	assert.Equal(t, http.StatusForbidden, api.Post("/authn/email/verification/complete", map[string]any{"token": token}, "Origin: https://evil.example").Code)
	assert.Equal(t, http.StatusBadRequest, api.Post("/authn/email/verification/complete", map[string]any{"token": strings.Repeat("x", 40)}, "Origin: https://app.example").Code)
	completed := api.Post("/authn/email/verification/complete", map[string]any{"token": token}, "Origin: https://app.example")
	assert.Equal(t, http.StatusOK, completed.Code, completed.Body.String())
	assert.Contains(t, api.Get("/authn/session", "Cookie: "+cookie).Body.String(), `"email_verified":true`)

	_, err = db.NewUpdate().Table("iam_principals").Set("email = ''").Set("email_verified = ?", false).Where("id = ?", principalID).Exec(t.Context())
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, api.Post("/authn/email/verification/start", "Cookie: "+cookie, "Origin: https://app.example").Code)

	startOperation := api.OpenAPI().Paths["/authn/email/verification/start"].Post
	completeOperation := api.OpenAPI().Paths["/authn/email/verification/complete"].Post
	require.NotNil(t, startOperation)
	require.NotNil(t, completeOperation)
	assert.Equal(t, "authn-start-email-verification", startOperation.OperationID)
	assert.Equal(t, "authn-complete-email-verification", completeOperation.OperationID)
	assert.Contains(t, startOperation.Responses, "202")
}

func TestRegistrationHTTPFlow(t *testing.T) {
	db, web, _ := newRecoveryAuthenticationService(t, "https://notify.example.test/events", true)
	_, api := humatest.New(t)
	authnmod.RegisterREST(api, web, web)

	capabilities := api.Get("/authn/capabilities")
	require.Equal(t, http.StatusOK, capabilities.Code, capabilities.Body.String())
	assert.Contains(t, capabilities.Body.String(), `"registration":true`)
	assert.Contains(t, capabilities.Body.String(), `"password_recovery":true`)

	assert.Equal(t, http.StatusForbidden, api.Post("/authn/register", map[string]any{
		"email": "registered@example.com", "password": "correct registration password", "display_name": "Registered User",
	}, "Origin: https://evil.example").Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/authn/register", map[string]any{
		"email": "not-an-email", "password": "short",
	}, "Origin: https://app.example").Code)

	for range 2 {
		response := api.Post("/authn/register", map[string]any{
			"email": "registered@example.com", "password": "correct registration password", "display_name": "Registered User",
		}, "Origin: https://app.example")
		assert.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), `"accepted":true`)
	}
	assert.Equal(t, 1, authnAPIRowCount(t, db, "iam_principals", "email = ?", "registered@example.com"))
	assert.Equal(t, 1, authnAPIRowCount(t, db, "iam_notification_outbox", "recipient = ?", "registered@example.com"))
	assert.Zero(t, authnAPIRowCount(t, db, "iam_tenant_members", "user_subject IN (SELECT id FROM iam_principals WHERE email = ?)", "registered@example.com"))

	registration := api.OpenAPI().Paths["/authn/register"].Post
	capabilityOperation := api.OpenAPI().Paths["/authn/capabilities"].Get
	require.NotNil(t, registration)
	require.NotNil(t, capabilityOperation)
	assert.Equal(t, "authn-register", registration.OperationID)
	assert.Equal(t, "authn-capabilities", capabilityOperation.OperationID)
	assert.Empty(t, registration.Security)
	assert.Contains(t, registration.Responses, "202")
	assert.Contains(t, registration.Responses, "422")

	_, disabled, _ := newAuthenticationService(t)
	_, disabledAPI := humatest.New(t)
	authnmod.RegisterREST(disabledAPI, disabled, disabled)
	assert.Contains(t, disabledAPI.Get("/authn/capabilities").Body.String(), `"registration":false`)
	assert.Equal(t, http.StatusServiceUnavailable, disabledAPI.Post("/authn/register", map[string]any{
		"email": "disabled@example.com", "password": "correct registration password",
	}, "Origin: https://app.example").Code)
}

func TestDisabledPasswordRecoveryHTTPFlow(t *testing.T) {
	_, web, _ := newAuthenticationService(t)
	_, api := humatest.New(t)
	authnmod.RegisterREST(api, web, web)

	start := api.Post("/authn/password/recovery/start", map[string]any{"identifier": "alice@example.com"}, "Origin: https://app.example")
	assert.Equal(t, http.StatusServiceUnavailable, start.Code, start.Body.String())
	complete := api.Post("/authn/password/recovery/complete", map[string]any{
		"token": "cpr1_" + strings.Repeat("a", 43), "new_password": "new correct horse battery staple",
	}, "Origin: https://app.example")
	assert.Equal(t, http.StatusServiceUnavailable, complete.Code, complete.Body.String())
}

func TestAuthenticationMFAHTTPFlow(t *testing.T) {
	_, web, _ := newAuthenticationService(t)
	_, api := humatest.New(t)
	authnmod.RegisterREST(api, web, web)

	login := api.Post("/authn/login", map[string]any{
		"login_name": "alice", "password": "correct horse battery staple", "return_url": "https://app.example/",
	}, "Origin: https://app.example")
	require.Equal(t, http.StatusOK, login.Code, login.Body.String())
	cookie := strings.Split(login.Header().Get("Set-Cookie"), ";")[0]
	require.NotEmpty(t, cookie)
	assert.Equal(t, http.StatusUnauthorized, api.Get("/authn/mfa").Code)
	assert.Equal(t, http.StatusForbidden, api.Post("/authn/mfa/totp/enroll", "Cookie: "+cookie, "Origin: https://evil.example", map[string]any{
		"current_password": "correct horse battery staple",
	}).Code)

	enroll := api.Post("/authn/mfa/totp/enroll", "Cookie: "+cookie, "Origin: https://app.example", map[string]any{
		"current_password": "correct horse battery staple",
	})
	require.Equal(t, http.StatusOK, enroll.Code, enroll.Body.String())
	var enrollmentEnvelope struct {
		Data authn.MFAEnrollment `json:"data"`
	}
	require.NoError(t, json.Unmarshal(enroll.Body.Bytes(), &enrollmentEnvelope))
	assert.NotEmpty(t, enrollmentEnvelope.Data.ProvisioningURI)
	code, err := totp.GenerateCode(enrollmentEnvelope.Data.Secret, time.Now())
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/authn/mfa/totp/confirm", "Cookie: "+cookie, "Origin: https://app.example", map[string]any{"code": differentTOTP(code)}).Code)
	confirm := api.Post("/authn/mfa/totp/confirm", "Cookie: "+cookie, "Origin: https://app.example", map[string]any{"code": code})
	require.Equal(t, http.StatusOK, confirm.Code, confirm.Body.String())
	var confirmationEnvelope struct {
		Data authn.MFAConfirmation `json:"data"`
	}
	require.NoError(t, json.Unmarshal(confirm.Body.Bytes(), &confirmationEnvelope))
	require.Len(t, confirmationEnvelope.Data.RecoveryCodes, 10)

	challengeResponse := api.Post("/authn/login", map[string]any{
		"login_name": "alice", "password": "correct horse battery staple", "return_url": "https://app.example/",
	}, "Origin: https://app.example")
	require.Equal(t, http.StatusOK, challengeResponse.Code, challengeResponse.Body.String())
	assert.Empty(t, challengeResponse.Header().Get("Set-Cookie"))
	var challengeEnvelope struct {
		Data authn.LoginResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(challengeResponse.Body.Bytes(), &challengeEnvelope))
	assert.Equal(t, "mfa_required", challengeEnvelope.Data.Status)
	assert.Equal(t, []string{"totp", "recovery_code"}, challengeEnvelope.Data.Methods)

	verifyBody := map[string]any{"challenge_id": challengeEnvelope.Data.ChallengeID, "code": confirmationEnvelope.Data.RecoveryCodes[0]}
	assert.Equal(t, http.StatusForbidden, api.Post("/authn/login/mfa", verifyBody, "Origin: https://evil.example").Code)
	assert.Equal(t, http.StatusUnauthorized, api.Post("/authn/login/mfa", map[string]any{"challenge_id": challengeEnvelope.Data.ChallengeID, "code": differentTOTP(code)}, "Origin: https://app.example").Code)
	verified := api.Post("/authn/login/mfa", verifyBody, "Origin: https://app.example")
	require.Equal(t, http.StatusOK, verified.Code, verified.Body.String())
	mfaCookie := strings.Split(verified.Header().Get("Set-Cookie"), ";")[0]
	require.NotEmpty(t, mfaCookie)

	status := api.Get("/authn/mfa", "Cookie: "+mfaCookie)
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	var statusEnvelope struct {
		Data authn.MFAStatus `json:"data"`
	}
	require.NoError(t, json.Unmarshal(status.Body.Bytes(), &statusEnvelope))
	assert.True(t, statusEnvelope.Data.TOTPEnabled)
	assert.Equal(t, 9, statusEnvelope.Data.RecoveryCodesRemaining)
	assert.Equal(t, http.StatusConflict, api.Post("/authn/mfa/totp/enroll", "Cookie: "+mfaCookie, "Origin: https://app.example", map[string]any{
		"current_password": "correct horse battery staple",
	}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/authn/mfa/totp/confirm", "Cookie: "+mfaCookie, "Origin: https://app.example", map[string]any{
		"code": code,
	}).Code)
	assert.Equal(t, http.StatusForbidden, api.Post("/authn/mfa/recovery-codes/regenerate", "Cookie: "+mfaCookie, "Origin: https://evil.example", map[string]any{
		"current_password": "correct horse battery staple", "code": confirmationEnvelope.Data.RecoveryCodes[1],
	}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/authn/mfa/recovery-codes/regenerate", "Cookie: "+mfaCookie, "Origin: https://app.example", map[string]any{
		"current_password": "correct horse battery staple", "code": "invalid-recovery-code",
	}).Code)
	regenerated := api.Post("/authn/mfa/recovery-codes/regenerate", "Cookie: "+mfaCookie, "Origin: https://app.example", map[string]any{
		"current_password": "correct horse battery staple", "code": confirmationEnvelope.Data.RecoveryCodes[1],
	})
	require.Equal(t, http.StatusOK, regenerated.Code, regenerated.Body.String())
	var regeneratedEnvelope struct {
		Data authn.MFAConfirmation `json:"data"`
	}
	require.NoError(t, json.Unmarshal(regenerated.Body.Bytes(), &regeneratedEnvelope))
	require.Len(t, regeneratedEnvelope.Data.RecoveryCodes, 10)
	assert.Equal(t, http.StatusForbidden, api.Delete("/authn/mfa/totp", "Cookie: "+mfaCookie, "Origin: https://evil.example", map[string]any{
		"current_password": "correct horse battery staple", "code": regeneratedEnvelope.Data.RecoveryCodes[0],
	}).Code)

	disabled := api.Delete("/authn/mfa/totp", "Cookie: "+mfaCookie, "Origin: https://app.example", map[string]any{
		"current_password": "correct horse battery staple", "code": regeneratedEnvelope.Data.RecoveryCodes[0],
	})
	require.Equal(t, http.StatusOK, disabled.Code, disabled.Body.String())
	status = api.Get("/authn/mfa", "Cookie: "+mfaCookie)
	assert.Equal(t, http.StatusOK, status.Code, status.Body.String())
	require.NoError(t, json.Unmarshal(status.Body.Bytes(), &statusEnvelope))
	assert.False(t, statusEnvelope.Data.TOTPEnabled)
	assert.Equal(t, http.StatusConflict, api.Delete("/authn/mfa/totp", "Cookie: "+mfaCookie, "Origin: https://app.example", map[string]any{
		"current_password": "correct horse battery staple", "code": regeneratedEnvelope.Data.RecoveryCodes[1],
	}).Code)

	for _, path := range []string{"/authn/login/mfa", "/authn/mfa", "/authn/mfa/totp/enroll", "/authn/mfa/totp/confirm", "/authn/mfa/recovery-codes/regenerate", "/authn/mfa/totp"} {
		assert.Contains(t, api.OpenAPI().Paths, path)
	}
	for _, operation := range []*huma.Operation{
		api.OpenAPI().Paths["/authn/mfa"].Get,
		api.OpenAPI().Paths["/authn/mfa/totp/enroll"].Post,
		api.OpenAPI().Paths["/authn/mfa/totp/confirm"].Post,
		api.OpenAPI().Paths["/authn/mfa/recovery-codes/regenerate"].Post,
		api.OpenAPI().Paths["/authn/mfa/totp"].Delete,
	} {
		assert.Contains(t, operation.Responses, "500")
	}
}

func TestMFAAuditFailureHTTPRollsBackEnrollment(t *testing.T) {
	db, web, principalID := newAuthenticationService(t)
	router := chi.NewMux()
	api := humachi.New(router, huma.DefaultConfig("Chaosplus API", "test"))
	authnmod.RegisterREST(api, web, web)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	loginRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/authn/login", strings.NewReader(`{"login_name":"alice","password":"correct horse battery staple","return_url":"https://app.example/"}`))
	require.NoError(t, err)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set("Origin", "https://app.example")
	loginResponse, err := server.Client().Do(loginRequest)
	require.NoError(t, err)
	loginBody, err := io.ReadAll(loginResponse.Body)
	require.NoError(t, err)
	require.NoError(t, loginResponse.Body.Close())
	require.Equal(t, http.StatusOK, loginResponse.StatusCode, string(loginBody))
	require.NotEmpty(t, loginResponse.Cookies())

	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER deny_mfa_enrollment_audit BEFORE INSERT ON iam_audit_events WHEN NEW.event_type = 'mfa_enrollment_started' BEGIN SELECT RAISE(ABORT, 'audit denied'); END`)
	require.NoError(t, err)
	enrollmentRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/authn/mfa/totp/enroll", strings.NewReader(`{"current_password":"correct horse battery staple"}`))
	require.NoError(t, err)
	enrollmentRequest.Header.Set("Content-Type", "application/json")
	enrollmentRequest.Header.Set("Origin", "https://app.example")
	enrollmentRequest.AddCookie(loginResponse.Cookies()[0])
	enrollmentResponse, err := server.Client().Do(enrollmentRequest)
	require.NoError(t, err)
	enrollmentBody, err := io.ReadAll(enrollmentResponse.Body)
	require.NoError(t, err)
	require.NoError(t, enrollmentResponse.Body.Close())
	assert.Equal(t, http.StatusInternalServerError, enrollmentResponse.StatusCode, string(enrollmentBody))

	enrollmentCount, err := db.NewSelect().Table("iam_mfa_enrollments").Where("principal_id = ?", principalID).Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, enrollmentCount)
	auditCount, err := db.NewSelect().Table("iam_audit_events").Where("event_type = ?", "mfa_enrollment_started").Count(t.Context())
	require.NoError(t, err)
	assert.Zero(t, auditCount)
}

func TestAuthenticationPasskeyHTTPContract(t *testing.T) {
	_, web, _ := newAuthenticationService(t)
	_, api := humatest.New(t)
	authnmod.RegisterREST(api, web, web)
	login := api.Post("/authn/login", map[string]any{
		"login_name": "alice", "password": "correct horse battery staple", "return_url": "https://app.example/",
	}, "Origin: https://app.example")
	require.Equal(t, http.StatusOK, login.Code, login.Body.String())
	cookie := strings.Split(login.Header().Get("Set-Cookie"), ";")[0]

	list := api.Get("/authn/passkeys", "Cookie: "+cookie)
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	assert.Contains(t, list.Body.String(), `"data":[]`)
	assert.Equal(t, http.StatusForbidden, api.Post("/authn/passkeys/registration/options", "Cookie: "+cookie, "Origin: https://evil.example", map[string]any{
		"current_password": "correct horse battery staple",
	}).Code)
	registration := api.Post("/authn/passkeys/registration/options", "Cookie: "+cookie, "Origin: https://app.example", map[string]any{
		"current_password": "correct horse battery staple",
	})
	require.Equal(t, http.StatusOK, registration.Code, registration.Body.String())
	var options struct {
		Data authn.PasskeyOptions `json:"data"`
	}
	require.NoError(t, json.Unmarshal(registration.Body.Bytes(), &options))
	assert.NotEmpty(t, options.Data.ChallengeID)
	assert.Contains(t, string(options.Data.Options), `"residentKey":"required"`)
	invalidRegistration := map[string]any{"challenge_id": options.Data.ChallengeID, "name": "Laptop", "credential": map[string]any{"invalid": true}}
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/authn/passkeys/registration/verify", "Cookie: "+cookie, "Origin: https://app.example", invalidRegistration).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/authn/passkeys/registration/verify", "Cookie: "+cookie, "Origin: https://app.example", invalidRegistration).Code)

	assert.Equal(t, http.StatusForbidden, api.Post("/authn/passkey/login/options", map[string]any{"return_url": "https://app.example/"}, "Origin: https://evil.example").Code)
	assert.Equal(t, http.StatusUnprocessableEntity, api.Post("/authn/passkey/login/options", map[string]any{"return_url": "https://evil.example/"}, "Origin: https://app.example").Code)
	loginOptions := api.Post("/authn/passkey/login/options", map[string]any{"return_url": "https://app.example/"}, "Origin: https://app.example")
	require.Equal(t, http.StatusOK, loginOptions.Code, loginOptions.Body.String())
	require.NoError(t, json.Unmarshal(loginOptions.Body.Bytes(), &options))
	invalidLogin := map[string]any{"challenge_id": options.Data.ChallengeID, "credential": map[string]any{"invalid": true}}
	assert.Equal(t, http.StatusUnauthorized, api.Post("/authn/passkey/login/verify", invalidLogin, "Origin: https://app.example").Code)
	assert.Equal(t, http.StatusUnauthorized, api.Post("/authn/passkey/login/verify", invalidLogin, "Origin: https://app.example").Code)

	missingID := strings.Repeat("a", 64)
	assert.Equal(t, http.StatusNotFound, api.Patch("/authn/passkeys/"+missingID, "Cookie: "+cookie, "Origin: https://app.example", map[string]any{"name": "Missing"}).Code)
	assert.Equal(t, http.StatusUnauthorized, api.Delete("/authn/passkeys/"+missingID, "Cookie: "+cookie, "Origin: https://app.example", map[string]any{"current_password": "wrong"}).Code)
	assert.Equal(t, http.StatusNotFound, api.Delete("/authn/passkeys/"+missingID, "Cookie: "+cookie, "Origin: https://app.example", map[string]any{"current_password": "correct horse battery staple"}).Code)

	for _, path := range []string{"/authn/passkeys", "/authn/passkeys/registration/options", "/authn/passkeys/registration/verify", "/authn/passkey/login/options", "/authn/passkey/login/verify", "/authn/passkeys/{id}"} {
		assert.Contains(t, api.OpenAPI().Paths, path)
	}
	assert.Equal(t, "authn-finish-passkey-login", api.OpenAPI().Paths["/authn/passkey/login/verify"].Post.OperationID)
	assert.Contains(t, api.OpenAPI().Paths["/authn/passkey/login/verify"].Post.Responses, "401")
	for _, operation := range []*huma.Operation{
		api.OpenAPI().Paths["/authn/passkey/login/options"].Post,
		api.OpenAPI().Paths["/authn/passkey/login/verify"].Post,
		api.OpenAPI().Paths["/authn/passkeys"].Get,
		api.OpenAPI().Paths["/authn/passkeys/registration/options"].Post,
		api.OpenAPI().Paths["/authn/passkeys/registration/verify"].Post,
		api.OpenAPI().Paths["/authn/passkeys/{id}"].Patch,
		api.OpenAPI().Paths["/authn/passkeys/{id}"].Delete,
	} {
		assert.Contains(t, operation.Responses, "500")
	}
}

func differentTOTP(code string) string {
	if code[0] == '0' {
		return "1" + code[1:]
	}
	return "0" + code[1:]
}

func TestAuthenticationHTTPUnavailableAndDisabledWeb(t *testing.T) {
	db, web, _ := newAuthenticationService(t)
	_, api := humatest.New(t)
	authnmod.RegisterREST(api, web, web)
	require.NoError(t, db.Close())
	assert.Equal(t, http.StatusInternalServerError, api.Post("/authn/login", map[string]any{
		"login_name": "alice", "password": "correct horse battery staple",
	}, "Origin: https://app.example").Code)
	assert.Equal(t, http.StatusInternalServerError, api.Post("/authn/passkey/login/options", map[string]any{
		"return_url": "https://app.example/",
	}, "Origin: https://app.example").Code)

	_, declaration := humatest.New(t)
	authnmod.RegisterREST(declaration, web, nil)
	assert.Contains(t, declaration.OpenAPI().Paths, "/authn/me")
	assert.NotContains(t, declaration.OpenAPI().Paths, "/authn/login")
}

func newAuthenticationService(t *testing.T) (*bun.DB, *authnmod.WebService, string) {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = byte(index + 1)
	}
	web, err := authnmod.NewWebService(authn.Config{
		Enabled: true, Issuer: "https://iam.example", Audience: []string{"api"},
		SigningKey: base64.RawStdEncoding.EncodeToString(seed), AccessTokenTTL: time.Hour,
		MFA:     authn.MFAConfig{EncryptionKey: base64.RawStdEncoding.EncodeToString(seed)},
		Passkey: authn.PasskeyConfig{Enabled: true, RPID: "app.example", DisplayName: "Chaosplus", Origins: []string{"https://app.example"}},
		Web: authn.WebConfig{
			Enabled: true, CookieName: "cp_session", SessionTTL: time.Hour, IdleTTL: 10 * time.Minute,
			PostLoginURL: "https://app.example/", PostLogoutURL: "https://app.example/login",
			AllowedReturnURLs: []string{"https://app.example/"}, AllowedOrigins: []string{"https://app.example"},
		},
	}, db)
	require.NoError(t, err)
	principalID, err := authnmod.EnsureBootstrapPrincipal(context.Background(), db, authnmod.BootstrapPrincipal{
		LoginName: "alice", Password: "correct horse battery staple", DisplayName: "Alice", Email: "alice@example.com",
	})
	require.NoError(t, err)
	return db, web, principalID
}

func newRecoveryAuthenticationService(t *testing.T, notificationURL string, registration ...bool) (*bun.DB, *authnmod.WebService, string) {
	t.Helper()
	db, err := bunxtest.Memory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, iam.Migrate(context.Background(), db))
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = byte(index + 1)
	}
	registrationEnabled := len(registration) > 0 && registration[0]
	options := []authnmod.WebOption{}
	if registrationEnabled {
		options = append(options, authnmod.WithRegistrationPrincipalCreator(func(ctx context.Context, db bun.IDB, email, passwordHash, displayName string, now time.Time) (string, error) {
			id, createErr := identity.CreatePendingPrincipal(ctx, db, email, passwordHash, displayName, now)
			switch {
			case errors.Is(createErr, identity.ErrLoginConflict):
				return "", authn.ErrRegistrationConflict
			case errors.Is(createErr, identity.ErrInvalid):
				return "", authn.ErrInvalidRegistration
			default:
				return id, createErr
			}
		}))
	}
	web, err := authnmod.NewWebService(authn.Config{
		Enabled: true, Issuer: "https://iam.example", Audience: []string{"api"},
		SigningKey: base64.RawStdEncoding.EncodeToString(seed), AccessTokenTTL: time.Hour,
		MFA: authn.MFAConfig{EncryptionKey: base64.RawStdEncoding.EncodeToString(seed)},
		Recovery: authn.RecoveryConfig{
			Enabled: true, TokenTTL: 15 * time.Minute, Cooldown: 24 * time.Hour,
			ResetURL: "https://app.example/recover",
		},
		EmailVerification: authn.EmailVerificationConfig{Enabled: true, TokenTTL: time.Hour, VerifyURL: "https://app.example/verify-email"},
		Registration:      authn.RegistrationConfig{Enabled: registrationEnabled},
		Notification:      authn.NotificationConfig{URL: notificationURL, PollInterval: 10 * time.Millisecond, RequestTimeout: time.Second, MaxAttempts: 3},
		Web: authn.WebConfig{
			Enabled: true, CookieName: "cp_session", SessionTTL: time.Hour, IdleTTL: 10 * time.Minute,
			PostLoginURL: "https://app.example/", PostLogoutURL: "https://app.example/login",
			AllowedReturnURLs: []string{"https://app.example/"}, AllowedOrigins: []string{"https://app.example"},
		},
	}, db, options...)
	require.NoError(t, err)
	principalID, err := authnmod.EnsureBootstrapPrincipal(context.Background(), db, authnmod.BootstrapPrincipal{
		LoginName: "alice", Password: "correct horse battery staple", DisplayName: "Alice", Email: "alice@example.com",
	})
	require.NoError(t, err)
	return db, web, principalID
}

func authnAPIRowCount(t *testing.T, db *bun.DB, table, condition string, args ...any) int {
	t.Helper()
	count, err := db.NewSelect().Table(table).Where(condition, args...).Count(t.Context())
	require.NoError(t, err)
	return count
}
