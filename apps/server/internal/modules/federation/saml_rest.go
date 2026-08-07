package federation

import (
	"context"
	"net/http"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/danielgtaylor/huma/v2"
)

type samlSPListInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
}

type samlSPIDInput struct {
	TenantID          string `header:"X-Tenant-Id" maxLength:"128"`
	ServiceProviderID string `path:"sp_id" maxLength:"128"`
}

type samlSPBody struct {
	Name        string `json:"name" minLength:"1" maxLength:"128"`
	EntityID    string `json:"entity_id" minLength:"1" maxLength:"255"`
	MetadataXML string `json:"metadata_xml" minLength:"1" maxLength:"65536"`
	Status      string `json:"status,omitempty" enum:"active,disabled" default:"active"`
}

type samlSPCreateInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     samlSPBody
}

type samlSPReplaceInput struct {
	TenantID          string `header:"X-Tenant-Id" maxLength:"128"`
	ServiceProviderID string `path:"sp_id" maxLength:"128"`
	Body              samlSPBody
}

type samlKeyRotateInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
}

// RegisterSAMLREST mounts the tenant SAML service provider management API and
// the public SAML browser endpoints. The browser endpoints are public by
// design; the protocol carries their security (registered SP entity IDs,
// destination checks, and the authenticated browser session).
func RegisterSAMLREST(api huma.API, service *Service, registrar *authz.Registrar) {
	RegisterSAMLAdminREST(api, service, registrar)
	RegisterSAMLBrowserREST(api, service)
}

func RegisterSAMLAdminREST(api huma.API, service *Service, registrar *authz.Registrar) {
	list := huma.Operation{OperationID: "federation-list-saml-service-providers", Method: http.MethodGet, Path: "/iam/saml/service-providers", Summary: "List tenant SAML service providers", Tags: []string{"federation"}}
	authz.Register(registrar, api, list, authz.Guard{Resource: "identity_provider", Verb: "view"}, func(ctx context.Context, in *samlSPListInput) (*respx.Body[[]SAMLServiceProvider], error) {
		items, err := service.ListSAMLServiceProviders(ctx, in.TenantID)
		if err != nil {
			return nil, samlError(err)
		}
		return respx.OK(ctx, items), nil
	})

	create := huma.Operation{OperationID: "federation-create-saml-service-provider", Method: http.MethodPost, Path: "/iam/saml/service-providers", Summary: "Register a SAML service provider from its metadata", Tags: []string{"federation"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity}}
	authz.Register(registrar, api, create, authz.Guard{Resource: "identity_provider", Verb: "create"}, func(ctx context.Context, in *samlSPCreateInput) (*respx.Body[SAMLServiceProvider], error) {
		item, err := service.CreateSAMLServiceProvider(ctx, in.TenantID, samlSPInputFromBody(in.Body))
		if err != nil {
			return nil, samlError(err)
		}
		return respx.OK(ctx, item), nil
	})

	replace := huma.Operation{OperationID: "federation-update-saml-service-provider", Method: http.MethodPut, Path: "/iam/saml/service-providers/{sp_id}", Summary: "Replace a SAML service provider", Tags: []string{"federation"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}
	authz.Register(registrar, api, replace, authz.Guard{Resource: "identity_provider", Verb: "update"}, func(ctx context.Context, in *samlSPReplaceInput) (*respx.Body[SAMLServiceProvider], error) {
		item, err := service.UpdateSAMLServiceProvider(ctx, in.TenantID, in.ServiceProviderID, samlSPInputFromBody(in.Body))
		if err != nil {
			return nil, samlError(err)
		}
		return respx.OK(ctx, item), nil
	})

	remove := huma.Operation{OperationID: "federation-delete-saml-service-provider", Method: http.MethodDelete, Path: "/iam/saml/service-providers/{sp_id}", Summary: "Delete a SAML service provider", Tags: []string{"federation"}, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity}}
	authz.Register(registrar, api, remove, authz.Guard{Resource: "identity_provider", Verb: "delete"}, func(ctx context.Context, in *samlSPIDInput) (*respx.Body[map[string]bool], error) {
		if err := service.DeleteSAMLServiceProvider(ctx, in.TenantID, in.ServiceProviderID); err != nil {
			return nil, samlError(err)
		}
		return respx.OK(ctx, map[string]bool{"deleted": true}), nil
	})

	rotate := huma.Operation{OperationID: "federation-rotate-saml-signing-key", Method: http.MethodPost, Path: "/iam/saml/signing-key/rotate", Summary: "Rotate the SAML identity provider signing key", Tags: []string{"federation"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict}}
	authz.Register(registrar, api, rotate, authz.Guard{Resource: "identity_provider", Verb: "update"}, func(ctx context.Context, _ *samlKeyRotateInput) (*respx.Body[SAMLKeyInfo], error) {
		item, err := service.RotateSAMLSigningKey(ctx)
		if err != nil {
			return nil, samlError(err)
		}
		return respx.OK(ctx, item), nil
	})
}

// RegisterSAMLBrowserREST mounts the public SAML endpoints. The raw adapter
// pattern gives the handlers direct access to request metadata, cookies, and
// the response body so protocol responses render as HTML.
func RegisterSAMLBrowserREST(api huma.API, service *Service) {
	sso := huma.Operation{
		OperationID: "federation-saml-sso", Method: http.MethodGet, Path: samlSSORoute,
		Summary: "Process a SAML AuthnRequest and return a signed response", Tags: []string{"federation"},
		Parameters: []*huma.Param{
			{Name: "tenant_id", In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(128)}},
			{Name: "SAMLRequest", In: "query", Required: true, Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(65536)}},
			{Name: "RelayState", In: "query", Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(2048)}},
		},
	}
	authz.Public(&sso)
	api.OpenAPI().AddOperation(&sso)
	api.Adapter().Handle(&sso, func(ctx huma.Context) {
		service.ServeSAMLSSO(api, ctx, ctx.Param("tenant_id"))
	})

	ssoPost := huma.Operation{
		OperationID: "federation-saml-sso-post", Method: http.MethodPost, Path: samlSSORoute,
		Summary: "Process a SAML AuthnRequest (HTTP-POST binding)", Tags: []string{"federation"},
		Parameters: []*huma.Param{
			{Name: "tenant_id", In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(128)}},
		},
	}
	authz.Public(&ssoPost)
	api.OpenAPI().AddOperation(&ssoPost)
	api.Adapter().Handle(&ssoPost, func(ctx huma.Context) {
		service.ServeSAMLSSO(api, ctx, ctx.Param("tenant_id"))
	})

	slo := huma.Operation{
		OperationID: "federation-saml-slo", Method: http.MethodGet, Path: samlSLORoute,
		Summary: "Process a SAML LogoutRequest and revoke the browser session", Tags: []string{"federation"},
		Parameters: []*huma.Param{
			{Name: "tenant_id", In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(128)}},
			{Name: "SAMLRequest", In: "query", Required: true, Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(65536)}},
			{Name: "RelayState", In: "query", Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(2048)}},
		},
	}
	authz.Public(&slo)
	api.OpenAPI().AddOperation(&slo)
	api.Adapter().Handle(&slo, func(ctx huma.Context) {
		service.ServeSAMLSLO(api, ctx, ctx.Param("tenant_id"))
	})

	sloPost := huma.Operation{
		OperationID: "federation-saml-slo-post", Method: http.MethodPost, Path: samlSLORoute,
		Summary: "Process a SAML LogoutRequest (HTTP-POST binding)", Tags: []string{"federation"},
		Parameters: []*huma.Param{
			{Name: "tenant_id", In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(128)}},
		},
	}
	authz.Public(&sloPost)
	api.OpenAPI().AddOperation(&sloPost)
	api.Adapter().Handle(&sloPost, func(ctx huma.Context) {
		service.ServeSAMLSLO(api, ctx, ctx.Param("tenant_id"))
	})

	metadata := huma.Operation{
		OperationID: "federation-saml-metadata", Method: http.MethodGet, Path: samlMetadataRoute,
		Summary: "Return the tenant SAML identity provider metadata", Tags: []string{"federation"},
		Parameters: []*huma.Param{
			{Name: "tenant_id", In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(128)}},
		},
	}
	authz.Public(&metadata)
	api.OpenAPI().AddOperation(&metadata)
	api.Adapter().Handle(&metadata, func(ctx huma.Context) {
		service.ServeSAMLMetadata(api, ctx, ctx.Param("tenant_id"))
	})
}

func samlSPInputFromBody(body samlSPBody) SAMLServiceProviderInput {
	return SAMLServiceProviderInput(body)
}
