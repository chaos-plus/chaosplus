package authn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
)

type meOutput struct {
	Subject           string `json:"subject"`
	Issuer            string `json:"issuer"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Email             string `json:"email,omitempty"`
	EmailVerified     bool   `json:"email_verified"`
	OrganizationID    string `json:"organization_id,omitempty"`
}

type meInput struct {
	Authorization string `header:"Authorization" doc:"Bearer access token issued by Chaosplus IAM"`
	Cookie        string `header:"Cookie" hidden:"true"`
}

type Authenticator interface {
	Authenticate(context.Context, string, string) (*authnext.Claims, error)
}

type logoutInput struct {
	Authorization string `header:"Authorization"`
	Cookie        string `header:"Cookie" hidden:"true"`
	Origin        string `header:"Origin" hidden:"true"`
}

type loginInput struct {
	Origin string `header:"Origin" hidden:"true"`
	Body   struct {
		LoginName string `json:"login_name" minLength:"1" maxLength:"200"`
		Password  string `json:"password" minLength:"1" maxLength:"200"`
		ReturnURL string `json:"return_url,omitempty" maxLength:"2048"`
	}
}

type registrationInput struct {
	Origin string `header:"Origin" hidden:"true"`
	Body   struct {
		Email       string `json:"email" format:"email" maxLength:"320"`
		Password    string `json:"password" minLength:"12" maxLength:"1024"`
		DisplayName string `json:"display_name,omitempty" maxLength:"128"`
	}
}

type sessionListInput struct {
	Authorization string `header:"Authorization"`
	Cookie        string `header:"Cookie" hidden:"true"`
}

type sessionMutationInput struct {
	Authorization string `header:"Authorization"`
	Cookie        string `header:"Cookie" hidden:"true"`
	Origin        string `header:"Origin" hidden:"true"`
	ID            string `path:"id" minLength:"64" maxLength:"64" pattern:"^[a-f0-9]{64}$"`
}

type passwordChangeInput struct {
	Authorization string `header:"Authorization"`
	Cookie        string `header:"Cookie" hidden:"true"`
	Origin        string `header:"Origin" hidden:"true"`
	Body          struct {
		CurrentPassword string `json:"current_password" minLength:"1" maxLength:"1024"`
		NewPassword     string `json:"new_password" minLength:"12" maxLength:"1024"`
	}
}

type passwordRecoveryStartInput struct {
	Origin string `header:"Origin" hidden:"true"`
	Body   struct {
		Identifier string `json:"identifier" minLength:"1" maxLength:"320" doc:"Login name or verified primary email"`
	}
}

type passwordRecoveryCompleteInput struct {
	Origin string `header:"Origin" hidden:"true"`
	Body   struct {
		Token       string `json:"token" minLength:"32" maxLength:"128"`
		NewPassword string `json:"new_password" minLength:"12" maxLength:"1024"`
	}
}

type emailVerificationStartInput struct {
	Authorization string `header:"Authorization"`
	Cookie        string `header:"Cookie" hidden:"true"`
	Origin        string `header:"Origin" hidden:"true"`
}

type emailVerificationCompleteInput struct {
	Origin string `header:"Origin" hidden:"true"`
	Body   struct {
		Token string `json:"token" minLength:"32" maxLength:"128"`
	}
}

type mfaLoginInput struct {
	Origin string `header:"Origin" hidden:"true"`
	Body   struct {
		ChallengeID string `json:"challenge_id" minLength:"32" maxLength:"128"`
		Code        string `json:"code" minLength:"6" maxLength:"64"`
	}
}

type mfaStatusInput struct {
	Authorization string `header:"Authorization"`
	Cookie        string `header:"Cookie" hidden:"true"`
}

type mfaPasswordInput struct {
	Authorization string `header:"Authorization"`
	Cookie        string `header:"Cookie" hidden:"true"`
	Origin        string `header:"Origin" hidden:"true"`
	Body          struct {
		CurrentPassword string `json:"current_password" minLength:"1" maxLength:"1024"`
	}
}

type mfaCodeInput struct {
	Authorization string `header:"Authorization"`
	Cookie        string `header:"Cookie" hidden:"true"`
	Origin        string `header:"Origin" hidden:"true"`
	Body          struct {
		Code string `json:"code" minLength:"6" maxLength:"64"`
	}
}

type mfaVerificationInput struct {
	Authorization string `header:"Authorization"`
	Cookie        string `header:"Cookie" hidden:"true"`
	Origin        string `header:"Origin" hidden:"true"`
	Body          struct {
		CurrentPassword string `json:"current_password" minLength:"1" maxLength:"1024"`
		Code            string `json:"code" minLength:"6" maxLength:"64"`
	}
}

type passkeyLoginBeginInput struct {
	Origin string `header:"Origin" hidden:"true"`
	Body   struct {
		ReturnURL string `json:"return_url,omitempty" maxLength:"2048"`
	}
}

type passkeyFinishInput struct {
	Origin string `header:"Origin" hidden:"true"`
	Body   struct {
		ChallengeID string          `json:"challenge_id" minLength:"32" maxLength:"128"`
		Credential  json.RawMessage `json:"credential"`
	}
}

type passkeyRegistrationFinishInput struct {
	Authorization string `header:"Authorization"`
	Cookie        string `header:"Cookie" hidden:"true"`
	Origin        string `header:"Origin" hidden:"true"`
	Body          struct {
		ChallengeID string          `json:"challenge_id" minLength:"32" maxLength:"128"`
		Name        string          `json:"name" minLength:"1" maxLength:"200"`
		Credential  json.RawMessage `json:"credential"`
	}
}

type passkeyMutationInput struct {
	Authorization string `header:"Authorization"`
	Cookie        string `header:"Cookie" hidden:"true"`
	Origin        string `header:"Origin" hidden:"true"`
	ID            string `path:"id" minLength:"64" maxLength:"64" pattern:"^[a-f0-9]{64}$"`
	Body          struct {
		Name string `json:"name" minLength:"1" maxLength:"200"`
	}
}

type passkeyDeleteInput struct {
	Authorization string `header:"Authorization"`
	Cookie        string `header:"Cookie" hidden:"true"`
	Origin        string `header:"Origin" hidden:"true"`
	ID            string `path:"id" minLength:"64" maxLength:"64" pattern:"^[a-f0-9]{64}$"`
	Body          struct {
		CurrentPassword string `json:"current_password" minLength:"1" maxLength:"1024"`
	}
}

type logoutData struct {
	LogoutURL string `json:"logout_url" doc:"IdP end-session URL the browser must visit to finish logout"`
}

type logoutOutput struct {
	SetCookie string `header:"Set-Cookie"`
	Body      respx.Envelope[logoutData]
}

type loginOutput struct {
	SetCookie string `header:"Set-Cookie"`
	Body      respx.Envelope[authnext.LoginResult]
}

func RegisterREST(a huma.API, authenticator Authenticator, web *WebService) {
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-me",
		Method:      http.MethodGet,
		Path:        "/authn/me",
		Summary:     "Return the authenticated local principal",
		Tags:        []string{"authn"},
		Security:    authz.UserSecurity(),
	}, func(ctx context.Context, in *meInput) (*respx.Body[meOutput], error) {
		claims, err := authenticator.Authenticate(ctx, in.Authorization, in.Cookie)
		if err != nil {
			return nil, huma.Error401Unauthorized("unauthorized")
		}
		return respx.OK(ctx, meOutput{
			Subject:           claims.Subject,
			Issuer:            claims.Issuer,
			PreferredUsername: claims.PreferredUsername,
			Email:             claims.Email,
			EmailVerified:     claims.EmailVerified,
			OrganizationID:    claims.OrganizationID,
		}), nil
	})

	if web == nil || !web.Enabled() {
		return
	}
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-capabilities", Method: http.MethodGet, Path: "/authn/capabilities",
		Summary: "Return enabled public authentication capabilities", Tags: []string{"authn"},
	}, func(ctx context.Context, _ *struct{}) (*respx.Body[authnext.Capabilities], error) {
		return respx.OK(ctx, web.Capabilities()), nil
	})
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-register", Method: http.MethodPost, Path: "/authn/register",
		Summary: "Register a global principal pending email verification", Tags: []string{"authn"}, DefaultStatus: http.StatusAccepted,
		Errors: []int{http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusServiceUnavailable, http.StatusInternalServerError},
	}, func(ctx context.Context, in *registrationInput) (*respx.Body[map[string]bool], error) {
		if err := web.ValidateLoginOrigin(in.Origin); err != nil {
			return nil, huma.Error403Forbidden("registration_request_rejected")
		}
		if err := web.Register(ctx, in.Body.Email, in.Body.Password, in.Body.DisplayName); err != nil {
			switch {
			case errors.Is(err, authnext.ErrInvalidRegistration):
				return nil, huma.Error422UnprocessableEntity("invalid_registration")
			case errors.Is(err, authnext.ErrRegistrationDisabled):
				return nil, huma.Error503ServiceUnavailable("registration_unavailable")
			default:
				return nil, huma.Error500InternalServerError("authentication_unavailable")
			}
		}
		return respx.OK(ctx, map[string]bool{"accepted": true}), nil
	})
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-login", Method: http.MethodPost, Path: "/authn/login",
		Summary: "Log in with a local username and password", Tags: []string{"authn"},
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, func(ctx context.Context, in *loginInput) (*loginOutput, error) {
		if err := web.ValidateLoginOrigin(in.Origin); err != nil {
			return nil, huma.Error403Forbidden("login_request_rejected")
		}
		result, err := web.BeginLogin(ctx, in.Body.LoginName, in.Body.Password, in.Body.ReturnURL)
		if err != nil {
			switch {
			case errors.Is(err, authnext.ErrReturnURL):
				return nil, huma.Error422UnprocessableEntity("return_url_not_allowed")
			case errors.Is(err, authnext.ErrAdditionalVerification):
				return nil, huma.Error409Conflict("additional_verification_required")
			case errors.Is(err, authnext.ErrInvalidCredentials):
				return nil, huma.Error401Unauthorized("invalid_login")
			default:
				return nil, huma.Error500InternalServerError("authentication_unavailable")
			}
		}
		output := &loginOutput{Body: respx.OK(ctx, result).Body}
		if result.SessionID != "" {
			output.SetCookie = web.SessionCookie(result.SessionID)
		}
		return output, nil
	})
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-verify-login-mfa", Method: http.MethodPost, Path: "/authn/login/mfa", Summary: "Complete an MFA login challenge", Tags: []string{"authn"}}, func(ctx context.Context, in *mfaLoginInput) (*loginOutput, error) {
		if err := web.ValidateLoginOrigin(in.Origin); err != nil {
			return nil, huma.Error403Forbidden("login_request_rejected")
		}
		result, err := web.VerifyLoginMFA(ctx, in.Body.ChallengeID, in.Body.Code)
		if err != nil {
			if errors.Is(err, authnext.ErrInvalidMFA) || errors.Is(err, authnext.ErrMFAChallenge) {
				return nil, huma.Error401Unauthorized("invalid_mfa_challenge")
			}
			return nil, huma.Error500InternalServerError("authentication_unavailable")
		}
		return &loginOutput{SetCookie: web.SessionCookie(result.SessionID), Body: respx.OK(ctx, result).Body}, nil
	})
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-begin-passkey-login", Method: http.MethodPost, Path: "/authn/passkey/login/options",
		Summary: "Begin a discoverable passkey login", Tags: []string{"authn"}, Errors: []int{http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusInternalServerError, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *passkeyLoginBeginInput) (*respx.Body[authnext.PasskeyOptions], error) {
		if err := web.ValidateLoginOrigin(in.Origin); err != nil {
			return nil, huma.Error403Forbidden("passkey_request_rejected")
		}
		options, err := web.BeginPasskeyLogin(ctx, in.Origin, in.Body.ReturnURL)
		if err != nil {
			switch {
			case errors.Is(err, authnext.ErrReturnURL):
				return nil, huma.Error422UnprocessableEntity("return_url_not_allowed")
			case errors.Is(err, authnext.ErrPasskeyDisabled):
				return nil, huma.Error503ServiceUnavailable("passkey_unavailable")
			default:
				return nil, huma.Error500InternalServerError("authentication_unavailable")
			}
		}
		return respx.OK(ctx, options), nil
	})
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-finish-passkey-login", Method: http.MethodPost, Path: "/authn/passkey/login/verify",
		Summary: "Verify a passkey assertion and create a browser session", Tags: []string{"authn"}, Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *passkeyFinishInput) (*loginOutput, error) {
		if err := web.ValidateLoginOrigin(in.Origin); err != nil {
			return nil, huma.Error403Forbidden("passkey_request_rejected")
		}
		result, err := web.FinishPasskeyLogin(ctx, in.Origin, in.Body.ChallengeID, in.Body.Credential)
		if err != nil {
			switch {
			case errors.Is(err, authnext.ErrPasskeyChallenge), errors.Is(err, authnext.ErrPasskeyCredential):
				return nil, huma.Error401Unauthorized("invalid_passkey_challenge")
			case errors.Is(err, authnext.ErrPasskeyDisabled):
				return nil, huma.Error503ServiceUnavailable("passkey_unavailable")
			default:
				return nil, huma.Error500InternalServerError("authentication_unavailable")
			}
		}
		return &loginOutput{SetCookie: web.SessionCookie(result.SessionID), Body: respx.OK(ctx, result).Body}, nil
	})
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-session", Method: http.MethodGet, Path: "/authn/session", Summary: "Return the browser session", Tags: []string{"authn"}, Security: authz.UserSecurity()}, func(ctx context.Context, in *meInput) (*respx.Body[meOutput], error) {
		claims, err := web.Authenticate(ctx, in.Authorization, in.Cookie)
		if err != nil {
			return nil, huma.Error401Unauthorized("unauthorized")
		}
		return respx.OK(ctx, meOutput{Subject: claims.Subject, Issuer: claims.Issuer, PreferredUsername: claims.PreferredUsername, Email: claims.Email, EmailVerified: claims.EmailVerified, OrganizationID: claims.OrganizationID}), nil
	})
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-logout", Method: http.MethodPost, Path: "/authn/logout", Summary: "Destroy the browser session", Tags: []string{"authn"}, Security: authz.UserSecurity()}, func(ctx context.Context, in *logoutInput) (*logoutOutput, error) {
		if err := web.ValidateCSRF(http.MethodPost, in.Origin, in.Cookie, in.Authorization); err != nil {
			return nil, huma.Error403Forbidden("csrf_rejected")
		}
		logoutURL := web.Logout(ctx, in.Cookie)
		return &logoutOutput{SetCookie: web.ClearCookie(), Body: respx.OK(ctx, logoutData{LogoutURL: logoutURL}).Body}, nil
	})
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-list-sessions", Method: http.MethodGet, Path: "/authn/sessions", Summary: "List active browser sessions", Tags: []string{"authn"}, Security: authz.UserSecurity()}, func(ctx context.Context, in *sessionListInput) (*respx.Body[[]authnext.BrowserSession], error) {
		sessions, err := web.ListSessions(ctx, in.Authorization, in.Cookie)
		if err != nil {
			return nil, authenticationError(err)
		}
		return respx.OK(ctx, sessions), nil
	})
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-revoke-session", Method: http.MethodDelete, Path: "/authn/sessions/{id}", Summary: "Revoke a browser session", Tags: []string{"authn"}, Security: authz.UserSecurity()}, func(ctx context.Context, in *sessionMutationInput) (*respx.Body[map[string]bool], error) {
		if err := web.ValidateCSRF(http.MethodDelete, in.Origin, in.Cookie, in.Authorization); err != nil {
			return nil, huma.Error403Forbidden("csrf_rejected")
		}
		if err := web.RevokeSession(ctx, in.Authorization, in.Cookie, in.ID); err != nil {
			return nil, authenticationError(err)
		}
		return respx.OK(ctx, map[string]bool{"revoked": true}), nil
	})
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-logout-all", Method: http.MethodPost, Path: "/authn/logout-all", Summary: "Revoke every session and refresh token", Tags: []string{"authn"}, Security: authz.UserSecurity()}, func(ctx context.Context, in *logoutInput) (*logoutOutput, error) {
		if err := web.ValidateCSRF(http.MethodPost, in.Origin, in.Cookie, in.Authorization); err != nil {
			return nil, huma.Error403Forbidden("csrf_rejected")
		}
		if err := web.LogoutAll(ctx, in.Authorization, in.Cookie); err != nil {
			return nil, authenticationError(err)
		}
		return &logoutOutput{SetCookie: web.ClearCookie(), Body: respx.OK(ctx, logoutData{LogoutURL: web.PostLogoutURL()}).Body}, nil
	})
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-change-password", Method: http.MethodPost, Path: "/authn/password/change", Summary: "Change the current principal password", Tags: []string{"authn"}, Security: authz.UserSecurity()}, func(ctx context.Context, in *passwordChangeInput) (*respx.Body[map[string]bool], error) {
		if err := web.ValidateCSRF(http.MethodPost, in.Origin, in.Cookie, in.Authorization); err != nil {
			return nil, huma.Error403Forbidden("csrf_rejected")
		}
		if err := web.ChangePassword(ctx, in.Authorization, in.Cookie, in.Body.CurrentPassword, in.Body.NewPassword); err != nil {
			return nil, authenticationError(err)
		}
		return respx.OK(ctx, map[string]bool{"changed": true}), nil
	})
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-start-password-recovery", Method: http.MethodPost, Path: "/authn/password/recovery/start",
		Summary: "Queue a password recovery notification", Tags: []string{"authn"}, DefaultStatus: http.StatusAccepted,
		Errors: []int{http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *passwordRecoveryStartInput) (*respx.Body[map[string]bool], error) {
		if err := web.ValidateLoginOrigin(in.Origin); err != nil {
			return nil, huma.Error403Forbidden("recovery_request_rejected")
		}
		if err := web.BeginPasswordRecovery(ctx, in.Body.Identifier); err != nil {
			switch {
			case errors.Is(err, authnext.ErrRecoveryDisabled):
				return nil, huma.Error503ServiceUnavailable("password_recovery_unavailable")
			case errors.Is(err, authnext.ErrInvalidRecovery):
				return nil, huma.Error422UnprocessableEntity("invalid_recovery_request")
			default:
				return nil, huma.Error503ServiceUnavailable("password_recovery_unavailable")
			}
		}
		return respx.OK(ctx, map[string]bool{"accepted": true}), nil
	})
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-complete-password-recovery", Method: http.MethodPost, Path: "/authn/password/recovery/complete",
		Summary: "Consume a password recovery credential", Tags: []string{"authn"},
		Errors: []int{http.StatusBadRequest, http.StatusConflict, http.StatusForbidden, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *passwordRecoveryCompleteInput) (*respx.Body[map[string]bool], error) {
		if err := web.ValidateLoginOrigin(in.Origin); err != nil {
			return nil, huma.Error403Forbidden("recovery_request_rejected")
		}
		if err := web.CompletePasswordRecovery(ctx, in.Body.Token, in.Body.NewPassword); err != nil {
			switch {
			case errors.Is(err, authnext.ErrRecoveryDisabled):
				return nil, huma.Error503ServiceUnavailable("password_recovery_unavailable")
			case errors.Is(err, authnext.ErrPasswordReused):
				return nil, huma.Error409Conflict("password_reused")
			case errors.Is(err, authnext.ErrInvalidRecovery):
				return nil, huma.Error400BadRequest("invalid_recovery_credential")
			default:
				return nil, huma.Error503ServiceUnavailable("password_recovery_unavailable")
			}
		}
		return respx.OK(ctx, map[string]bool{"changed": true}), nil
	})
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-start-email-verification", Method: http.MethodPost, Path: "/authn/email/verification/start",
		Summary: "Queue verification for the authenticated principal email", Tags: []string{"authn"},
		Security: authz.UserSecurity(), DefaultStatus: http.StatusAccepted,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *emailVerificationStartInput) (*respx.Body[map[string]bool], error) {
		if err := web.ValidateCSRF(http.MethodPost, in.Origin, in.Cookie, in.Authorization); err != nil {
			return nil, huma.Error403Forbidden("csrf_rejected")
		}
		if err := web.BeginEmailVerification(ctx, in.Authorization, in.Cookie); err != nil {
			switch {
			case errors.Is(err, authnext.ErrInvalidSession), errors.Is(err, authnext.ErrInvalidToken), errors.Is(err, authnext.ErrInvalidBearer):
				return nil, huma.Error401Unauthorized("unauthorized")
			case errors.Is(err, authnext.ErrEmailRequired):
				return nil, huma.Error409Conflict("primary_email_required")
			case errors.Is(err, authnext.ErrEmailVerificationDisabled):
				return nil, huma.Error503ServiceUnavailable("email_verification_unavailable")
			default:
				return nil, huma.Error503ServiceUnavailable("email_verification_unavailable")
			}
		}
		return respx.OK(ctx, map[string]bool{"accepted": true}), nil
	})
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-complete-email-verification", Method: http.MethodPost, Path: "/authn/email/verification/complete",
		Summary: "Consume a primary email verification credential", Tags: []string{"authn"},
		Errors: []int{http.StatusBadRequest, http.StatusForbidden, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *emailVerificationCompleteInput) (*respx.Body[map[string]bool], error) {
		if err := web.ValidateLoginOrigin(in.Origin); err != nil {
			return nil, huma.Error403Forbidden("email_verification_request_rejected")
		}
		if err := web.CompleteEmailVerification(ctx, in.Body.Token); err != nil {
			switch {
			case errors.Is(err, authnext.ErrInvalidEmailVerification):
				return nil, huma.Error400BadRequest("invalid_email_verification_credential")
			case errors.Is(err, authnext.ErrEmailVerificationDisabled):
				return nil, huma.Error503ServiceUnavailable("email_verification_unavailable")
			default:
				return nil, huma.Error503ServiceUnavailable("email_verification_unavailable")
			}
		}
		return respx.OK(ctx, map[string]bool{"verified": true}), nil
	})
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-mfa-status", Method: http.MethodGet, Path: "/authn/mfa", Summary: "Return the current principal MFA status", Tags: []string{"authn"}, Security: authz.UserSecurity(), Errors: []int{http.StatusUnauthorized, http.StatusInternalServerError}}, func(ctx context.Context, in *mfaStatusInput) (*respx.Body[authnext.MFAStatus], error) {
		status, err := web.MFAStatus(ctx, in.Authorization, in.Cookie)
		if err != nil {
			return nil, authenticationError(err)
		}
		return respx.OK(ctx, status), nil
	})
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-begin-totp-enrollment", Method: http.MethodPost, Path: "/authn/mfa/totp/enroll", Summary: "Begin TOTP enrollment after password verification", Tags: []string{"authn"}, Security: authz.UserSecurity(), Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusInternalServerError}}, func(ctx context.Context, in *mfaPasswordInput) (*respx.Body[authnext.MFAEnrollment], error) {
		if err := web.ValidateCSRF(http.MethodPost, in.Origin, in.Cookie, in.Authorization); err != nil {
			return nil, huma.Error403Forbidden("csrf_rejected")
		}
		enrollment, err := web.BeginTOTPEnrollment(ctx, in.Authorization, in.Cookie, in.Body.CurrentPassword)
		if err != nil {
			return nil, authenticationError(err)
		}
		return respx.OK(ctx, enrollment), nil
	})
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-confirm-totp-enrollment", Method: http.MethodPost, Path: "/authn/mfa/totp/confirm", Summary: "Confirm TOTP enrollment and issue recovery codes", Tags: []string{"authn"}, Security: authz.UserSecurity(), Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusInternalServerError}}, func(ctx context.Context, in *mfaCodeInput) (*respx.Body[authnext.MFAConfirmation], error) {
		if err := web.ValidateCSRF(http.MethodPost, in.Origin, in.Cookie, in.Authorization); err != nil {
			return nil, huma.Error403Forbidden("csrf_rejected")
		}
		confirmation, err := web.ConfirmTOTPEnrollment(ctx, in.Authorization, in.Cookie, in.Body.Code)
		if err != nil {
			return nil, authenticationError(err)
		}
		return respx.OK(ctx, confirmation), nil
	})
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-regenerate-recovery-codes", Method: http.MethodPost, Path: "/authn/mfa/recovery-codes/regenerate", Summary: "Replace all MFA recovery codes", Tags: []string{"authn"}, Security: authz.UserSecurity(), Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusInternalServerError}}, func(ctx context.Context, in *mfaVerificationInput) (*respx.Body[authnext.MFAConfirmation], error) {
		if err := web.ValidateCSRF(http.MethodPost, in.Origin, in.Cookie, in.Authorization); err != nil {
			return nil, huma.Error403Forbidden("csrf_rejected")
		}
		confirmation, err := web.RegenerateRecoveryCodes(ctx, in.Authorization, in.Cookie, in.Body.CurrentPassword, in.Body.Code)
		if err != nil {
			return nil, authenticationError(err)
		}
		return respx.OK(ctx, confirmation), nil
	})
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-disable-totp", Method: http.MethodDelete, Path: "/authn/mfa/totp", Summary: "Disable TOTP after password and factor verification", Tags: []string{"authn"}, Security: authz.UserSecurity(), Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusInternalServerError}}, func(ctx context.Context, in *mfaVerificationInput) (*respx.Body[map[string]bool], error) {
		if err := web.ValidateCSRF(http.MethodDelete, in.Origin, in.Cookie, in.Authorization); err != nil {
			return nil, huma.Error403Forbidden("csrf_rejected")
		}
		if err := web.DisableTOTP(ctx, in.Authorization, in.Cookie, in.Body.CurrentPassword, in.Body.Code); err != nil {
			return nil, authenticationError(err)
		}
		return respx.OK(ctx, map[string]bool{"disabled": true}), nil
	})
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-list-passkeys", Method: http.MethodGet, Path: "/authn/passkeys",
		Summary: "List the current principal passkeys", Tags: []string{"authn"}, Security: authz.UserSecurity(), Errors: []int{http.StatusUnauthorized, http.StatusInternalServerError, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *sessionListInput) (*respx.Body[[]authnext.Passkey], error) {
		passkeys, err := web.ListPasskeys(ctx, in.Authorization, in.Cookie)
		if err != nil {
			return nil, authenticationError(err)
		}
		return respx.OK(ctx, passkeys), nil
	})
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-begin-passkey-registration", Method: http.MethodPost, Path: "/authn/passkeys/registration/options",
		Summary: "Begin passkey registration after password verification", Tags: []string{"authn"}, Security: authz.UserSecurity(),
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusInternalServerError, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *mfaPasswordInput) (*respx.Body[authnext.PasskeyOptions], error) {
		if err := web.ValidateCSRF(http.MethodPost, in.Origin, in.Cookie, in.Authorization); err != nil {
			return nil, huma.Error403Forbidden("csrf_rejected")
		}
		options, err := web.BeginPasskeyRegistration(ctx, in.Authorization, in.Cookie, in.Body.CurrentPassword)
		if err != nil {
			return nil, authenticationError(err)
		}
		return respx.OK(ctx, options), nil
	})
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-finish-passkey-registration", Method: http.MethodPost, Path: "/authn/passkeys/registration/verify",
		Summary: "Verify and store a new passkey", Tags: []string{"authn"}, Security: authz.UserSecurity(),
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusInternalServerError, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *passkeyRegistrationFinishInput) (*respx.Body[authnext.Passkey], error) {
		if err := web.ValidateCSRF(http.MethodPost, in.Origin, in.Cookie, in.Authorization); err != nil {
			return nil, huma.Error403Forbidden("csrf_rejected")
		}
		passkey, err := web.FinishPasskeyRegistration(ctx, in.Authorization, in.Cookie, in.Body.ChallengeID, in.Body.Name, in.Body.Credential)
		if err != nil {
			return nil, authenticationError(err)
		}
		return respx.OK(ctx, passkey), nil
	})
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-rename-passkey", Method: http.MethodPatch, Path: "/authn/passkeys/{id}",
		Summary: "Rename a passkey", Tags: []string{"authn"}, Security: authz.UserSecurity(),
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusInternalServerError, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *passkeyMutationInput) (*respx.Body[authnext.Passkey], error) {
		if err := web.ValidateCSRF(http.MethodPatch, in.Origin, in.Cookie, in.Authorization); err != nil {
			return nil, huma.Error403Forbidden("csrf_rejected")
		}
		passkey, err := web.RenamePasskey(ctx, in.Authorization, in.Cookie, in.ID, in.Body.Name)
		if err != nil {
			return nil, authenticationError(err)
		}
		return respx.OK(ctx, passkey), nil
	})
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-delete-passkey", Method: http.MethodDelete, Path: "/authn/passkeys/{id}",
		Summary: "Delete a passkey after password verification", Tags: []string{"authn"}, Security: authz.UserSecurity(),
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *passkeyDeleteInput) (*respx.Body[map[string]bool], error) {
		if err := web.ValidateCSRF(http.MethodDelete, in.Origin, in.Cookie, in.Authorization); err != nil {
			return nil, huma.Error403Forbidden("csrf_rejected")
		}
		if err := web.DeletePasskey(ctx, in.Authorization, in.Cookie, in.ID, in.Body.CurrentPassword); err != nil {
			return nil, authenticationError(err)
		}
		return respx.OK(ctx, map[string]bool{"deleted": true}), nil
	})
}

func authenticationError(err error) error {
	switch {
	case errors.Is(err, authnext.ErrInvalidCredentials), errors.Is(err, authnext.ErrInvalidToken), errors.Is(err, authnext.ErrMissingBearer), errors.Is(err, authnext.ErrInvalidSession):
		return huma.Error401Unauthorized("unauthorized")
	case errors.Is(err, authnext.ErrSessionNotFound):
		return huma.Error404NotFound("session_not_found")
	case errors.Is(err, authnext.ErrInvalidPassword):
		return huma.Error422UnprocessableEntity("invalid_password")
	case errors.Is(err, authnext.ErrPasswordReused):
		return huma.Error422UnprocessableEntity("password_reused")
	case errors.Is(err, authnext.ErrInvalidMFA):
		return huma.Error422UnprocessableEntity("invalid_mfa")
	case errors.Is(err, authnext.ErrMFAEnrollment):
		return huma.Error422UnprocessableEntity("invalid_mfa_enrollment")
	case errors.Is(err, authnext.ErrMFAAlreadyOn):
		return huma.Error409Conflict("mfa_already_enabled")
	case errors.Is(err, authnext.ErrMFANotEnabled):
		return huma.Error409Conflict("mfa_not_enabled")
	case errors.Is(err, authnext.ErrRecoveryCooldown):
		return huma.Error409Conflict("recovery_cooldown_active")
	case errors.Is(err, authnext.ErrPasskeyChallenge):
		return huma.Error422UnprocessableEntity("invalid_passkey_challenge")
	case errors.Is(err, authnext.ErrPasskeyCredential):
		return huma.Error422UnprocessableEntity("invalid_passkey_credential")
	case errors.Is(err, authnext.ErrPasskeyLimit):
		return huma.Error409Conflict("passkey_limit_reached")
	case errors.Is(err, authnext.ErrPasskeyNotFound):
		return huma.Error404NotFound("passkey_not_found")
	case errors.Is(err, authnext.ErrPasskeyDisabled):
		return huma.Error503ServiceUnavailable("passkey_unavailable")
	default:
		return huma.Error500InternalServerError("authentication_unavailable")
	}
}
