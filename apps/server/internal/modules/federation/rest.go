package federation

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
)

func parseFederationID(value string) (guid.ID, error) {
	id, err := guid.Parse(strings.TrimSpace(value))
	if err != nil {
		return 0, huma.Error422UnprocessableEntity("invalid_id")
	}
	return id, nil
}

type providerListInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
}

type providerIDInput struct {
	TenantID   string `header:"X-Tenant-Id" maxLength:"128"`
	ProviderID string `path:"provider_id" maxLength:"128"`
}

type providerBody struct {
	Name          string `json:"name" minLength:"1" maxLength:"128"`
	ProviderType  string `json:"provider_type,omitempty" enum:"oidc" default:"oidc"`
	Issuer        string `json:"issuer" minLength:"1" maxLength:"255"`
	ClientID      string `json:"client_id" minLength:"1" maxLength:"128"`
	ClientSecret  string `json:"client_secret,omitempty" maxLength:"2048" doc:"Upstream client secret; an empty value keeps the stored secret on update"`
	Scopes        string `json:"scopes,omitempty" maxLength:"255"`
	AutoProvision bool   `json:"auto_provision,omitempty" default:"true"`
	DefaultRoleID string `json:"default_role_id,omitempty" maxLength:"32" doc:"Role granted to just-in-time provisioned members"`
	Status        string `json:"status,omitempty" enum:"active,disabled" default:"active"`
}

type providerCreateInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     providerBody
}

type providerReplaceInput struct {
	TenantID   string `header:"X-Tenant-Id" maxLength:"128"`
	ProviderID string `path:"provider_id" maxLength:"128"`
	Body       providerBody
}

// RegisterREST mounts the tenant management API and the public browser login
// flow. The management endpoints are tenant-authorized; the browser endpoints
// are intentionally public because their security is carried by the sealed
// state cookie and PKCE verifier.
func RegisterREST(api huma.API, service *Service, registrar *authz.Registrar) {
	RegisterAdminREST(api, service, registrar)
	RegisterBrowserREST(api, service)
}

func RegisterAdminREST(api huma.API, service *Service, registrar *authz.Registrar) {
	guard := authz.Guard{Resource: "identity_provider", Verb: "view"}
	authz.Register(registrar, api, huma.Operation{OperationID: "federation-list-identity-providers", Method: http.MethodGet, Path: "/iam/identity-providers", Summary: "List tenant identity providers", Tags: []string{"federation"}}, guard, func(ctx context.Context, in *providerListInput) (*respx.Body[[]Provider], error) {
		tenantID, err := parseFederationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		items, err := service.ListProviders(ctx, tenantID)
		if err != nil {
			return nil, federationError(err)
		}
		return respx.OK(ctx, items), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "federation-create-identity-provider", Method: http.MethodPost, Path: "/iam/identity-providers", Summary: "Create an OIDC identity provider", Tags: []string{"federation"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "identity_provider", Verb: "create"}, func(ctx context.Context, in *providerCreateInput) (*respx.Body[Provider], error) {
		tenantID, err := parseFederationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		input, err := providerInputFromBody(in.Body)
		if err != nil {
			return nil, err
		}
		item, err := service.CreateProvider(ctx, tenantID, input)
		if err != nil {
			return nil, federationError(err)
		}
		return respx.OK(ctx, item), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "federation-update-identity-provider", Method: http.MethodPut, Path: "/iam/identity-providers/{provider_id}", Summary: "Replace an OIDC identity provider", Tags: []string{"federation"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "identity_provider", Verb: "update"}, func(ctx context.Context, in *providerReplaceInput) (*respx.Body[Provider], error) {
		tenantID, err := parseFederationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		providerID, err := parseFederationID(in.ProviderID)
		if err != nil {
			return nil, err
		}
		input, err := providerInputFromBody(in.Body)
		if err != nil {
			return nil, err
		}
		item, err := service.UpdateProvider(ctx, tenantID, providerID, input)
		if err != nil {
			return nil, federationError(err)
		}
		return respx.OK(ctx, item), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "federation-delete-identity-provider", Method: http.MethodDelete, Path: "/iam/identity-providers/{provider_id}", Summary: "Delete an OIDC identity provider and its identity links", Tags: []string{"federation"}, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity}}, authz.Guard{Resource: "identity_provider", Verb: "delete"}, func(ctx context.Context, in *providerIDInput) (*respx.Body[map[string]bool], error) {
		tenantID, err := parseFederationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		providerID, err := parseFederationID(in.ProviderID)
		if err != nil {
			return nil, err
		}
		if err := service.DeleteProvider(ctx, tenantID, providerID); err != nil {
			return nil, federationError(err)
		}
		return respx.OK(ctx, map[string]bool{"deleted": true}), nil
	})
}

// RegisterBrowserREST mounts the public browser endpoints. They are
// unauthenticated by design; the sealed state cookie and PKCE verifier carry
// the flow's security. The raw adapter pattern gives the handlers direct
// access to request metadata and response cookies.
func RegisterBrowserREST(api huma.API, service *Service) {
	start := huma.Operation{
		OperationID: "federation-start-login", Method: http.MethodGet, Path: "/federation/{provider_id}/start",
		Summary: "Start an OIDC login for a tenant identity provider", Tags: []string{"federation"}, DefaultStatus: http.StatusFound,
		Parameters: []*huma.Param{
			{Name: "provider_id", In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(128)}},
			{Name: "return_url", In: "query", Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(2048), Description: "Allowlisted application URL to return to after login"}},
		},
	}
	authz.Public(&start)
	start.Responses = browserResponses(api.OpenAPI().Components.Schemas)
	api.OpenAPI().AddOperation(&start)
	api.Adapter().Handle(&start, func(ctx huma.Context) {
		providerID, err := parseFederationID(ctx.Param("provider_id"))
		if err != nil {
			writeFederationError(api, ctx, err)
			return
		}
		result, err := service.StartLogin(ctx.Context(), providerID, ctx.Query("return_url"), requestCallbackURL(ctx, providerID))
		if err != nil {
			writeFederationError(api, ctx, err)
			return
		}
		ctx.AppendHeader("Set-Cookie", result.StateCookie)
		ctx.SetHeader("Location", result.AuthorizationURL)
		ctx.SetStatus(http.StatusFound)
	})

	callback := huma.Operation{
		OperationID: "federation-callback-login", Method: http.MethodGet, Path: "/federation/{provider_id}/callback",
		Summary: "Complete the OIDC authorization code flow and set the browser session", Tags: []string{"federation"}, DefaultStatus: http.StatusFound,
		Parameters: []*huma.Param{
			{Name: "provider_id", In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(128)}},
			{Name: "code", In: "query", Required: true, Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(4096)}},
			{Name: "state", In: "query", Required: true, Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(256)}},
		},
	}
	authz.Public(&callback)
	callback.Responses = browserResponses(api.OpenAPI().Components.Schemas)
	api.OpenAPI().AddOperation(&callback)
	api.Adapter().Handle(&callback, func(ctx huma.Context) {
		providerID, err := parseFederationID(ctx.Param("provider_id"))
		if err != nil {
			writeFederationError(api, ctx, err)
			return
		}
		result, err := service.CompleteLogin(ctx.Context(), providerID, ctx.Query("code"), ctx.Query("state"), ctx.Header("Cookie"), requestCallbackURL(ctx, providerID))
		if err != nil {
			ctx.AppendHeader("Set-Cookie", service.StateClearCookie())
			writeFederationError(api, ctx, err)
			return
		}
		ctx.AppendHeader("Set-Cookie", service.StateClearCookie())
		ctx.AppendHeader("Set-Cookie", service.authn.SessionCookie(result.SessionToken))
		ctx.SetHeader("Location", result.ReturnURL)
		ctx.SetStatus(http.StatusFound)
	})

	samlCallback := huma.Operation{
		OperationID: "federation-callback-saml-login", Method: http.MethodPost, Path: "/federation/{provider_id}/callback",
		Summary: "Complete the SAML SP-initiated assertion flow and set the browser session", Tags: []string{"federation"}, DefaultStatus: http.StatusFound,
		Parameters: []*huma.Param{
			{Name: "provider_id", In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(128)}},
		},
	}
	authz.Public(&samlCallback)
	samlCallback.Responses = browserResponses(api.OpenAPI().Components.Schemas)
	api.OpenAPI().AddOperation(&samlCallback)
	api.Adapter().Handle(&samlCallback, func(ctx huma.Context) {
		providerID, err := parseFederationID(ctx.Param("provider_id"))
		if err != nil {
			writeFederationError(api, ctx, err)
			return
		}
		body, err := io.ReadAll(io.LimitReader(ctx.BodyReader(), samlFlateLimit))
		if err != nil {
			writeFederationError(api, ctx, ErrOIDCState)
			return
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			writeFederationError(api, ctx, ErrOIDCState)
			return
		}
		sessionToken, returnURL, err := service.CompleteSAMLLogin(ctx.Context(), providerID, values.Get("SAMLResponse"), ctx.Header("Cookie"), requestCallbackURL(ctx, providerID))
		if err != nil {
			ctx.AppendHeader("Set-Cookie", service.StateClearCookie())
			writeFederationError(api, ctx, err)
			return
		}
		ctx.AppendHeader("Set-Cookie", service.StateClearCookie())
		ctx.AppendHeader("Set-Cookie", service.authn.SessionCookie(sessionToken))
		ctx.SetHeader("Location", returnURL)
		ctx.SetStatus(http.StatusFound)
	})
}

func browserResponses(schemas huma.Registry) map[string]*huma.Response {
	redirect := &huma.Response{
		Description: http.StatusText(http.StatusFound),
		Headers: map[string]*huma.Param{
			"Location":   {Name: "Location", In: "header", Required: true, Schema: &huma.Schema{Type: huma.TypeString}},
			"Set-Cookie": {Name: "Set-Cookie", In: "header", Schema: &huma.Schema{Type: huma.TypeString}},
		},
	}
	errorSchema := huma.SchemaFromType(schemas, reflect.TypeFor[huma.ErrorModel]())
	return map[string]*huma.Response{
		"302": redirect,
		"400": {Description: "Bad request", Content: map[string]*huma.MediaType{"application/json": {Schema: errorSchema}}},
		"403": {Description: "Login denied", Content: map[string]*huma.MediaType{"application/json": {Schema: errorSchema}}},
		"404": {Description: "Provider not found", Content: map[string]*huma.MediaType{"application/json": {Schema: errorSchema}}},
		"409": {Description: "Provider conflict", Content: map[string]*huma.MediaType{"application/json": {Schema: errorSchema}}},
		"500": {Description: "Federation unavailable", Content: map[string]*huma.MediaType{"application/json": {Schema: errorSchema}}},
	}
}

func writeFederationError(api huma.API, ctx huma.Context, err error) {
	var mapped huma.StatusError
	if errors.As(federationError(err), &mapped) {
		_ = huma.WriteErr(api, ctx, mapped.GetStatus(), mapped.Error())
		return
	}
	_ = huma.WriteErr(api, ctx, http.StatusInternalServerError, "federation_unavailable")
}

func intPointer(value int) *int { return &value }

func providerInputFromBody(body providerBody) (ProviderInput, error) {
	defaultRoleID, err := parseOptionalFederationID(body.DefaultRoleID)
	if err != nil {
		return ProviderInput{}, err
	}
	return ProviderInput{
		Name: body.Name, ProviderType: body.ProviderType, Issuer: body.Issuer, ClientID: body.ClientID,
		ClientSecret: body.ClientSecret, Scopes: body.Scopes, AutoProvision: body.AutoProvision,
		DefaultRoleID: defaultRoleID, Status: body.Status,
	}, nil
}

func parseOptionalFederationID(value string) (guid.ID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	return parseFederationID(value)
}

// requestCallbackURL reconstructs the externally visible callback URL from the
// request so the redirect_uri sent to the IdP always matches this deployment.
func requestCallbackURL(ctx huma.Context, providerID guid.ID) string {
	scheme := "http"
	if proto := strings.TrimSpace(ctx.Header("X-Forwarded-Proto")); proto == "https" {
		scheme = "https"
	} else if ctx.TLS() != nil {
		scheme = "https"
	}
	return scheme + "://" + ctx.Host() + callbackURL(providerID)
}

func federationError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidProvider), errors.Is(err, ErrOIDCState):
		return huma.Error400BadRequest("federation_invalid_request")
	case errors.Is(err, ErrProviderNotFound):
		return huma.Error404NotFound("federation_provider_not_found")
	case errors.Is(err, ErrProviderIssuerExists):
		return huma.Error409Conflict("federation_provider_issuer_exists")
	case errors.Is(err, ErrProviderRoleMissing):
		return huma.Error409Conflict("federation_provider_role_missing")
	case errors.Is(err, ErrProviderDisabled):
		return huma.Error403Forbidden("federation_provider_disabled")
	case errors.Is(err, ErrProvisioningDisabled):
		return huma.Error403Forbidden("federation_provisioning_disabled")
	case errors.Is(err, ErrPrincipalInactive):
		return huma.Error403Forbidden("federation_identity_unavailable")
	case errors.Is(err, ErrProvisioningUnverified):
		return huma.Error400BadRequest("federation_email_unverified")
	case errors.Is(err, ErrOIDCDiscovery), errors.Is(err, ErrOIDCToken), errors.Is(err, ErrOIDCTokenInvalid):
		return huma.Error400BadRequest("federation_oidc_failed")
	case errors.Is(err, ErrSAMLResponse):
		return huma.Error400BadRequest("federation_saml_invalid_request")
	default:
		return huma.Error500InternalServerError("federation_unavailable")
	}
}
