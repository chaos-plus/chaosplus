package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
)

type meOutput struct {
	Subject           string `json:"subject"`
	SpiceDBSubject    string `json:"spicedb_subject"`
	Issuer            string `json:"issuer"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Email             string `json:"email,omitempty"`
	OrganizationID    string `json:"organization_id,omitempty"`
}

type meInput struct {
	Authorization string `header:"Authorization" doc:"Bearer access token issued by Zitadel"`
	Cookie        string `header:"Cookie" hidden:"true"`
}

type Authenticator interface {
	Authenticate(context.Context, string, string) (*authnext.Claims, error)
}

type WebService interface {
	Enabled() bool
	DirectLoginEnabled() bool
	Begin(context.Context, string, string) (string, string, error)
	Callback(context.Context, string, string, string) (string, string, error)
	Login(context.Context, string, string, string) (string, string, error)
	Authenticate(context.Context, string, string) (*authnext.Claims, error)
	ValidateCSRF(string, string, string, string) error
	ValidateLoginOrigin(string) error
	Logout(context.Context, string) string
	SessionCookie(string) string
	FlowCookie(string) string
	ClearCookie() string
	FlowState(string) (string, error)
	PostLogoutURL() string
}

type oidcStartInput struct {
	Mode      string `query:"mode" enum:"login,register" default:"login"`
	ReturnURL string `query:"return_url" maxLength:"2048"`
}

type oidcCallbackInput struct {
	Code   string `query:"code" maxLength:"4096"`
	State  string `query:"state" maxLength:"256"`
	Error  string `query:"error" maxLength:"256"`
	Cookie string `header:"Cookie" hidden:"true"`
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

type redirectOutput struct {
	Status    int    `status:""`
	Location  string `header:"Location"`
	SetCookie string `header:"Set-Cookie"`
}

type logoutData struct {
	LogoutURL string `json:"logout_url" doc:"IdP end-session URL the browser must visit to finish logout"`
}

type logoutOutput struct {
	SetCookie string `header:"Set-Cookie"`
	Body      respx.Envelope[logoutData]
}

type loginData struct {
	ReturnURL string `json:"return_url"`
}

type loginOutput struct {
	SetCookie string `header:"Set-Cookie"`
	Body      respx.Envelope[loginData]
}

func RegisterREST(a huma.API, authenticator Authenticator, web WebService) {
	authz.RegisterPublic(a, huma.Operation{
		OperationID: "authn-me",
		Method:      http.MethodGet,
		Path:        "/authn/me",
		Summary:     "Return the authenticated Zitadel subject",
		Tags:        []string{"authn"},
	}, func(ctx context.Context, in *meInput) (*respx.Body[meOutput], error) {
		claims, err := authenticator.Authenticate(ctx, in.Authorization, in.Cookie)
		if err != nil {
			return nil, huma.Error401Unauthorized("unauthorized", err)
		}
		return respx.OK(ctx, meOutput{
			Subject:           claims.Subject,
			SpiceDBSubject:    claims.SubjectRef().String(),
			Issuer:            claims.Issuer,
			PreferredUsername: claims.PreferredUsername,
			Email:             claims.Email,
			OrganizationID:    claims.OrganizationID,
		}), nil
	})

	if web == nil || !web.Enabled() {
		return
	}
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-oidc-start", Method: http.MethodGet, Path: "/authn/oidc/start", Summary: "Start browser OIDC login or registration", Tags: []string{"authn"}}, func(ctx context.Context, in *oidcStartInput) (*redirectOutput, error) {
		location, state, err := web.Begin(ctx, in.Mode, in.ReturnURL)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid_oidc_request")
		}
		return &redirectOutput{Status: http.StatusFound, Location: location, SetCookie: web.FlowCookie(state)}, nil
	})
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-oidc-callback", Method: http.MethodGet, Path: "/authn/oidc/callback", Summary: "Complete browser OIDC login", Tags: []string{"authn"}}, func(ctx context.Context, in *oidcCallbackInput) (*redirectOutput, error) {
		if in.Error != "" {
			return nil, huma.Error401Unauthorized("oidc_authorization_failed")
		}
		flowState, err := web.FlowState(in.Cookie)
		if err != nil {
			return nil, huma.Error401Unauthorized("invalid_oidc_flow")
		}
		sessionID, returnURL, err := web.Callback(ctx, in.Code, in.State, flowState)
		if err != nil {
			return nil, huma.Error401Unauthorized("invalid_oidc_flow")
		}
		return &redirectOutput{Status: http.StatusFound, Location: returnURL, SetCookie: web.SessionCookie(sessionID)}, nil
	})
	if web.DirectLoginEnabled() {
		authz.RegisterPublic(a, huma.Operation{OperationID: "authn-login", Method: http.MethodPost, Path: "/authn/login", Summary: "Log in with a Zitadel username and password", Tags: []string{"authn"}}, func(ctx context.Context, in *loginInput) (*loginOutput, error) {
			if err := web.ValidateLoginOrigin(in.Origin); err != nil {
				return nil, huma.Error403Forbidden("login_request_rejected")
			}
			sessionID, returnURL, err := web.Login(ctx, in.Body.LoginName, in.Body.Password, in.Body.ReturnURL)
			if err != nil {
				switch {
				case errors.Is(err, authnext.ErrAdditionalVerification):
					return nil, huma.Error409Conflict("additional_verification_required")
				case errors.Is(err, authnext.ErrInvalidCredentials):
					return nil, huma.Error401Unauthorized("invalid_login")
				default:
					return nil, huma.NewError(http.StatusBadGateway, "identity_provider_unavailable")
				}
			}
			return &loginOutput{SetCookie: web.SessionCookie(sessionID), Body: respx.OK(ctx, loginData{ReturnURL: returnURL}).Body}, nil
		})
	}
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-session", Method: http.MethodGet, Path: "/authn/session", Summary: "Return the browser session", Tags: []string{"authn"}}, func(ctx context.Context, in *meInput) (*respx.Body[meOutput], error) {
		claims, err := web.Authenticate(ctx, in.Authorization, in.Cookie)
		if err != nil {
			return nil, huma.Error401Unauthorized("unauthorized")
		}
		return respx.OK(ctx, meOutput{Subject: claims.Subject, SpiceDBSubject: claims.SubjectRef().String(), Issuer: claims.Issuer, PreferredUsername: claims.PreferredUsername, Email: claims.Email, OrganizationID: claims.OrganizationID}), nil
	})
	authz.RegisterPublic(a, huma.Operation{OperationID: "authn-logout", Method: http.MethodPost, Path: "/authn/logout", Summary: "Destroy the browser session", Tags: []string{"authn"}}, func(ctx context.Context, in *logoutInput) (*logoutOutput, error) {
		if err := web.ValidateCSRF(http.MethodPost, in.Origin, in.Cookie, in.Authorization); err != nil {
			return nil, huma.Error403Forbidden("csrf_rejected")
		}
		logoutURL := web.Logout(ctx, in.Cookie)
		return &logoutOutput{SetCookie: web.ClearCookie(), Body: respx.OK(ctx, logoutData{LogoutURL: logoutURL}).Body}, nil
	})
}
