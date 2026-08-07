package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
	"github.com/danielgtaylor/huma/v2"
)

type discoveryOutput struct{ Body map[string]any }
type jwksOutput struct{ Body map[string]any }

type authorizeInput struct {
	ClientID            string `query:"client_id" required:"true" maxLength:"128"`
	RedirectURI         string `query:"redirect_uri" required:"true" maxLength:"2048"`
	ResponseType        string `query:"response_type" required:"true" enum:"code"`
	Scope               string `query:"scope"`
	State               string `query:"state" maxLength:"1024"`
	CodeChallenge       string `query:"code_challenge" required:"true" minLength:"43" maxLength:"128"`
	CodeChallengeMethod string `query:"code_challenge_method" required:"true" enum:"S256"`
	Nonce               string `query:"nonce"`
	Prompt              string `query:"prompt" enum:"consent,none"`
	Cookie              string `header:"Cookie" hidden:"true"`
}

type redirectOutput struct {
	Status   int    `status:""`
	Location string `header:"Location"`
}

type formInput struct {
	Authorization string `header:"Authorization" hidden:"true"`
	RawBody       []byte `contentType:"application/x-www-form-urlencoded"`
}

type tokenOutput struct {
	Status int `status:""`
	Body   TokenResponse
}
type revokeOutput struct {
	Status int `status:""`
}
type userInfoInput struct {
	Authorization string `header:"Authorization"`
}
type userInfoOutput struct{ Body map[string]any }
type introspectOutput struct{ Body map[string]any }

type tokenForm struct {
	GrantType    string `json:"grant_type" enum:"authorization_code,refresh_token,client_credentials"`
	ClientID     string `json:"client_id,omitempty" maxLength:"128"`
	ClientSecret string `json:"client_secret,omitempty" maxLength:"1024"`
	Code         string `json:"code,omitempty" maxLength:"1024"`
	RedirectURI  string `json:"redirect_uri,omitempty" maxLength:"2048"`
	CodeVerifier string `json:"code_verifier,omitempty" minLength:"43" maxLength:"128"`
	RefreshToken string `json:"refresh_token,omitempty" maxLength:"2048"`
	Scope        string `json:"scope,omitempty" maxLength:"2048"`
}

type tokenReferenceForm struct {
	Token        string `json:"token"`
	Hint         string `json:"token_type_hint,omitempty" enum:"access_token,refresh_token"`
	ClientID     string `json:"client_id,omitempty" maxLength:"128"`
	ClientSecret string `json:"client_secret,omitempty" maxLength:"1024"`
}

type protocolError struct {
	Status      int    `json:"-"`
	Code        string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

func (e *protocolError) Error() string  { return e.Code }
func (e *protocolError) GetStatus() int { return e.Status }

type clientListInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
}
type clientCreateInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		Name         string   `json:"name" minLength:"1" maxLength:"128"`
		RedirectURIs []string `json:"redirect_uris,omitempty" maxItems:"32"`
		GrantTypes   []string `json:"grant_types" minItems:"1" maxItems:"3"`
		Scopes       []string `json:"scopes" minItems:"1" maxItems:"128"`
		PublicClient bool     `json:"public_client"`
	}
}
type clientCreateData struct {
	Client       Client `json:"client"`
	ClientSecret string `json:"client_secret,omitempty"`
}
type clientUpdateInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"128"`
	Body     struct {
		Name         string   `json:"name" minLength:"1" maxLength:"128"`
		RedirectURIs []string `json:"redirect_uris,omitempty" maxItems:"32"`
		GrantTypes   []string `json:"grant_types" minItems:"1" maxItems:"3"`
		Scopes       []string `json:"scopes" minItems:"1" maxItems:"128"`
		PublicClient bool     `json:"public_client"`
		Status       string   `json:"status" enum:"active,disabled"`
	}
}
type clientIDInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"128"`
}

func RegisterREST(api huma.API, service *Service, registrar *authz.Registrar) {
	issuer := service.authn.Issuer()
	authz.RegisterPublic(api, huma.Operation{OperationID: "oauth-discovery", Method: http.MethodGet, Path: "/.well-known/openid-configuration", Summary: "OpenID Connect discovery metadata", Tags: []string{"oauth"}}, func(context.Context, *struct{}) (*discoveryOutput, error) {
		return &discoveryOutput{Body: map[string]any{
			"issuer": issuer, "authorization_endpoint": issuer + "/oauth/authorize", "token_endpoint": issuer + "/oauth/token",
			"jwks_uri": issuer + "/.well-known/jwks.json", "userinfo_endpoint": issuer + "/oauth/userinfo",
			"revocation_endpoint": issuer + "/oauth/revoke", "introspection_endpoint": issuer + "/oauth/introspect",
			"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token", "client_credentials"},
			"code_challenge_methods_supported": []string{"S256"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"EdDSA"},
		}}, nil
	})
	authz.RegisterPublic(api, huma.Operation{OperationID: "oauth-jwks", Method: http.MethodGet, Path: "/.well-known/jwks.json", Summary: "JSON Web Key Set", Tags: []string{"oauth"}}, func(context.Context, *struct{}) (*jwksOutput, error) {
		return &jwksOutput{Body: service.authn.JWKS()}, nil
	})
	authz.RegisterPublic(api, huma.Operation{OperationID: "oauth-authorize", Method: http.MethodGet, Path: "/oauth/authorize", Summary: "Start an authorization code flow with PKCE", DefaultStatus: http.StatusFound, Security: []map[string][]string{{authz.SessionScheme: {}}}, Tags: []string{"oauth"}}, func(ctx context.Context, in *authorizeInput) (*redirectOutput, error) {
		location, err := service.Authorize(ctx, in.Cookie, in.ClientID, in.RedirectURI, in.ResponseType, in.Scope, in.State, in.CodeChallenge, in.CodeChallengeMethod, in.Nonce, in.Prompt)
		if err != nil {
			// A consent-required result redirects to the consent endpoint;
			// everything else is a plain authorization error.
			var consentErr *ErrConsentRequired
			if errors.As(err, &consentErr) {
				consentURL := service.authn.Issuer() + "/oauth/consent?client_id=" + url.QueryEscape(in.ClientID) +
					"&scope=" + url.QueryEscape(in.Scope) + "&state=" + url.QueryEscape(in.State) +
					"&redirect_uri=" + url.QueryEscape(in.RedirectURI) +
					"&code_challenge=" + url.QueryEscape(in.CodeChallenge) +
					"&code_challenge_method=" + url.QueryEscape(in.CodeChallengeMethod) +
					"&nonce=" + url.QueryEscape(in.Nonce)
				return &redirectOutput{Status: http.StatusFound, Location: consentURL}, nil
			}
			return nil, oauthError(ctx, http.StatusBadRequest, "invalid_request")
		}
		return &redirectOutput{Status: http.StatusFound, Location: location}, nil
	})
	authz.RegisterPublic(api, huma.Operation{OperationID: "oauth-token", Method: http.MethodPost, Path: "/oauth/token", Summary: "Exchange a grant for OAuth tokens", Security: []map[string][]string{{authz.ClientBasicScheme: {}}, {}}, Tags: []string{"oauth"}}, func(ctx context.Context, in *formInput) (*tokenOutput, error) {
		form, err := url.ParseQuery(string(in.RawBody))
		if err != nil {
			return nil, oauthError(ctx, http.StatusBadRequest, "invalid_request")
		}
		response, err := service.Token(ctx, form, in.Authorization)
		if err != nil {
			if errors.Is(err, ErrInvalidRequest) {
				return nil, oauthError(ctx, http.StatusBadRequest, "invalid_grant")
			}
			return nil, oauthError(ctx, http.StatusInternalServerError, "server_error")
		}
		return &tokenOutput{Status: http.StatusOK, Body: response}, nil
	})
	documentForm[tokenForm](api, "/oauth/token")
	authz.RegisterPublic(api, huma.Operation{OperationID: "oauth-revoke", Method: http.MethodPost, Path: "/oauth/revoke", Summary: "Revoke an OAuth token", Security: []map[string][]string{{authz.ClientBasicScheme: {}}, {}}, Tags: []string{"oauth"}}, func(ctx context.Context, in *formInput) (*revokeOutput, error) {
		form, _ := url.ParseQuery(string(in.RawBody))
		if err := service.Revoke(ctx, form, in.Authorization); err != nil {
			return nil, oauthError(ctx, http.StatusUnauthorized, "invalid_client")
		}
		return &revokeOutput{Status: http.StatusOK}, nil
	})
	documentForm[tokenReferenceForm](api, "/oauth/revoke")
	authz.RegisterPublic(api, huma.Operation{OperationID: "oauth-introspect", Method: http.MethodPost, Path: "/oauth/introspect", Summary: "Inspect an OAuth token", Security: authz.ClientSecurity(), Tags: []string{"oauth"}}, func(ctx context.Context, in *formInput) (*introspectOutput, error) {
		form, _ := url.ParseQuery(string(in.RawBody))
		result, err := service.Introspect(ctx, form, in.Authorization)
		if err != nil {
			return nil, oauthError(ctx, http.StatusUnauthorized, "invalid_client")
		}
		return &introspectOutput{Body: result}, nil
	})
	documentForm[tokenReferenceForm](api, "/oauth/introspect")
	authz.RegisterPublic(api, huma.Operation{OperationID: "oauth-userinfo", Method: http.MethodGet, Path: "/oauth/userinfo", Summary: "Return OpenID Connect claims for an access token", Security: authz.BearerSecurity(), Tags: []string{"oauth"}}, func(ctx context.Context, in *userInfoInput) (*userInfoOutput, error) {
		claims, err := service.authn.Authenticate(ctx, in.Authorization, "")
		if err != nil {
			return nil, oauthError(ctx, http.StatusUnauthorized, "invalid_token")
		}
		return &userInfoOutput{Body: map[string]any{"sub": claims.Subject, "preferred_username": claims.PreferredUsername, "email": claims.Email, "email_verified": claims.EmailVerified}}, nil
	})
	if registrar == nil {
		return
	}
	authz.Register(registrar, api, huma.Operation{OperationID: "oauth-list-clients", Method: http.MethodGet, Path: "/iam/oauth-clients", Summary: "List tenant OAuth clients", Tags: []string{"oauth"}}, authz.Guard{Resource: "oauth_client", Verb: "view"}, func(ctx context.Context, in *clientListInput) (*respx.Body[[]Client], error) {
		clients, err := service.ListClients(ctx, in.TenantID)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid_client")
		}
		return respx.OK(ctx, clients), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "oauth-create-client", Method: http.MethodPost, Path: "/iam/oauth-clients", Summary: "Create a tenant OAuth client", Tags: []string{"oauth"}}, authz.Guard{Resource: "oauth_client", Verb: "create"}, func(ctx context.Context, in *clientCreateInput) (*respx.Body[clientCreateData], error) {
		client, secret, err := service.CreateClient(ctx, in.TenantID, in.Body.Name, in.Body.RedirectURIs, in.Body.GrantTypes, in.Body.Scopes, in.Body.PublicClient)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid_client")
		}
		return respx.OK(ctx, clientCreateData{Client: client, ClientSecret: secret}), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "oauth-update-client", Method: http.MethodPut, Path: "/iam/oauth-clients/{id}", Summary: "Replace a tenant OAuth client", Tags: []string{"oauth"}}, authz.Guard{Resource: "oauth_client", Verb: "update"}, func(ctx context.Context, in *clientUpdateInput) (*respx.Body[Client], error) {
		client, err := service.UpdateClient(ctx, in.TenantID, in.ID, in.Body.Name, in.Body.RedirectURIs, in.Body.GrantTypes, in.Body.Scopes, in.Body.PublicClient, in.Body.Status)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid_client")
		}
		return respx.OK(ctx, client), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "oauth-rotate-client-secret", Method: http.MethodPost, Path: "/iam/oauth-clients/{id}/rotate-secret", Summary: "Rotate a confidential OAuth client secret", Tags: []string{"oauth"}}, authz.Guard{Resource: "oauth_client", Verb: "update"}, func(ctx context.Context, in *clientIDInput) (*respx.Body[map[string]string], error) {
		secret, err := service.RotateClientSecret(ctx, in.TenantID, in.ID)
		if err != nil {
			return nil, huma.Error404NotFound("client_not_found")
		}
		return respx.OK(ctx, map[string]string{"client_secret": secret}), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "oauth-delete-client", Method: http.MethodDelete, Path: "/iam/oauth-clients/{id}", Summary: "Delete a tenant OAuth client", Tags: []string{"oauth"}}, authz.Guard{Resource: "oauth_client", Verb: "delete"}, func(ctx context.Context, in *clientIDInput) (*respx.Body[map[string]bool], error) {
		if err := service.DeleteClient(ctx, in.TenantID, in.ID); err != nil {
			return nil, huma.Error404NotFound("client_not_found")
		}
		return respx.OK(ctx, map[string]bool{"deleted": true}), nil
	})
}

func oauthError(ctx context.Context, status int, code string) error {
	return &protocolError{Status: status, Code: code, Description: i18n.TContext(ctx, "oauth_"+code)}
}

func documentForm[T any](api huma.API, path string) {
	operation := api.OpenAPI().Paths[path].Post
	operation.RequestBody = &huma.RequestBody{
		Required: true,
		Content: map[string]*huma.MediaType{
			"application/x-www-form-urlencoded": {Schema: api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[T](), false, "")},
		},
	}
	errorSchema := api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[protocolError](), false, "")
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusInternalServerError} {
		operation.Responses[httpStatus(status)] = &huma.Response{Description: http.StatusText(status), Content: map[string]*huma.MediaType{"application/json": {Schema: errorSchema}}}
	}
}

func httpStatus(status int) string { return fmt.Sprintf("%d", status) }
