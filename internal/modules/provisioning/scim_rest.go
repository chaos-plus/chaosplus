package provisioning

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"reflect"
	"strconv"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/danielgtaylor/huma/v2"
)

const scimBearerScheme = "scimBearer"

func RegisterSCIMREST(api huma.API, service *Service) {
	registerDiscovery(api)
	registerUserSCIM(api, service)
	registerGroupSCIM(api, service)
	registerSCIM(api, huma.Operation{OperationID: "scim-bulk", Method: http.MethodPost, Path: "/scim/v2/Bulk", Summary: "Execute SCIM bulk operations", Tags: []string{"SCIM 2.0"}}, reflect.TypeFor[BulkRequest](), reflect.TypeFor[BulkResponse](), true, func(ctx huma.Context) {
		auth, ok := authenticateSCIM(ctx, service)
		if !ok {
			return
		}
		var input BulkRequest
		if err := decodeSCIMBody(ctx, &input); err != nil {
			writeSCIMError(ctx, err)
			return
		}
		result, err := service.Bulk(ctx.Context(), auth, input)
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		writeSCIM(ctx, http.StatusOK, result)
	})
}

func registerUserSCIM(api huma.API, service *Service) {
	registerSCIM(api, huma.Operation{OperationID: "scim-list-users", Method: http.MethodGet, Path: "/scim/v2/Users", Summary: "List SCIM users", Tags: []string{"SCIM 2.0"}, Parameters: listParameters()}, nil, reflect.TypeFor[ListResponse[UserResource]](), true, func(ctx huma.Context) {
		auth, ok := authenticateSCIM(ctx, service)
		if !ok {
			return
		}
		request, err := listRequest(ctx)
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		result, err := service.ListUsers(ctx.Context(), auth, request)
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		writeSCIM(ctx, http.StatusOK, result)
	})
	registerSCIM(api, huma.Operation{OperationID: "scim-create-user", Method: http.MethodPost, Path: "/scim/v2/Users", Summary: "Create a SCIM user", Tags: []string{"SCIM 2.0"}}, reflect.TypeFor[UserInput](), reflect.TypeFor[UserResource](), true, func(ctx huma.Context) {
		auth, ok := authenticateSCIM(ctx, service)
		if !ok {
			return
		}
		var input UserInput
		if err := decodeSCIMBody(ctx, &input); err != nil {
			writeSCIMError(ctx, err)
			return
		}
		result, err := service.CreateUser(ctx.Context(), auth, input)
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		ctx.SetHeader("Location", result.Meta.Location)
		ctx.SetHeader("ETag", result.Meta.Version)
		writeSCIM(ctx, http.StatusCreated, result)
	})
	registerUserItemSCIM(api, service)
}

func registerUserItemSCIM(api huma.API, service *Service) {
	path := "/scim/v2/Users/{id}"
	registerSCIM(api, itemOperation("scim-get-user", http.MethodGet, path, "Get a SCIM user"), nil, reflect.TypeFor[UserResource](), true, func(ctx huma.Context) {
		auth, ok := authenticateSCIM(ctx, service)
		if !ok {
			return
		}
		result, err := service.GetUser(ctx.Context(), auth, ctx.Param("id"))
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		ctx.SetHeader("ETag", result.Meta.Version)
		writeSCIM(ctx, http.StatusOK, result)
	})
	registerSCIM(api, itemOperation("scim-replace-user", http.MethodPut, path, "Replace a SCIM user"), reflect.TypeFor[UserInput](), reflect.TypeFor[UserResource](), true, func(ctx huma.Context) {
		auth, ok := authenticateSCIM(ctx, service)
		if !ok {
			return
		}
		version, err := parseETag(ctx.Header("If-Match"))
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		var input UserInput
		if err := decodeSCIMBody(ctx, &input); err != nil {
			writeSCIMError(ctx, err)
			return
		}
		result, err := service.ReplaceUser(ctx.Context(), auth, ctx.Param("id"), input, version)
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		ctx.SetHeader("ETag", result.Meta.Version)
		writeSCIM(ctx, http.StatusOK, result)
	})
	registerSCIM(api, itemOperation("scim-patch-user", http.MethodPatch, path, "Patch a SCIM user"), reflect.TypeFor[PatchRequest](), reflect.TypeFor[UserResource](), true, func(ctx huma.Context) {
		auth, ok := authenticateSCIM(ctx, service)
		if !ok {
			return
		}
		version, err := parseETag(ctx.Header("If-Match"))
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		var input PatchRequest
		if err := decodeSCIMBody(ctx, &input); err != nil {
			writeSCIMError(ctx, err)
			return
		}
		result, err := service.PatchUser(ctx.Context(), auth, ctx.Param("id"), input, version)
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		ctx.SetHeader("ETag", result.Meta.Version)
		writeSCIM(ctx, http.StatusOK, result)
	})
	registerSCIM(api, itemOperation("scim-delete-user", http.MethodDelete, path, "Delete a SCIM user"), nil, nil, true, func(ctx huma.Context) {
		auth, ok := authenticateSCIM(ctx, service)
		if !ok {
			return
		}
		version, err := parseETag(ctx.Header("If-Match"))
		if err == nil {
			err = service.DeleteUser(ctx.Context(), auth, ctx.Param("id"), version)
		}
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		writeSCIM(ctx, http.StatusNoContent, nil)
	})
}

func registerGroupSCIM(api huma.API, service *Service) {
	registerSCIM(api, huma.Operation{OperationID: "scim-list-groups", Method: http.MethodGet, Path: "/scim/v2/Groups", Summary: "List SCIM groups", Tags: []string{"SCIM 2.0"}, Parameters: listParameters()}, nil, reflect.TypeFor[ListResponse[GroupResource]](), true, func(ctx huma.Context) {
		auth, ok := authenticateSCIM(ctx, service)
		if !ok {
			return
		}
		request, err := listRequest(ctx)
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		result, err := service.ListGroups(ctx.Context(), auth, request)
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		writeSCIM(ctx, http.StatusOK, result)
	})
	registerSCIM(api, huma.Operation{OperationID: "scim-create-group", Method: http.MethodPost, Path: "/scim/v2/Groups", Summary: "Create a SCIM group", Tags: []string{"SCIM 2.0"}}, reflect.TypeFor[GroupInput](), reflect.TypeFor[GroupResource](), true, func(ctx huma.Context) {
		auth, ok := authenticateSCIM(ctx, service)
		if !ok {
			return
		}
		var input GroupInput
		if err := decodeSCIMBody(ctx, &input); err != nil {
			writeSCIMError(ctx, err)
			return
		}
		result, err := service.CreateGroup(ctx.Context(), auth, input)
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		ctx.SetHeader("Location", result.Meta.Location)
		ctx.SetHeader("ETag", result.Meta.Version)
		writeSCIM(ctx, http.StatusCreated, result)
	})
	registerGroupItemSCIM(api, service)
}

func registerGroupItemSCIM(api huma.API, service *Service) {
	path := "/scim/v2/Groups/{id}"
	registerSCIM(api, itemOperation("scim-get-group", http.MethodGet, path, "Get a SCIM group"), nil, reflect.TypeFor[GroupResource](), true, func(ctx huma.Context) {
		auth, ok := authenticateSCIM(ctx, service)
		if !ok {
			return
		}
		result, err := service.GetGroup(ctx.Context(), auth, ctx.Param("id"))
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		ctx.SetHeader("ETag", result.Meta.Version)
		writeSCIM(ctx, http.StatusOK, result)
	})
	registerSCIM(api, itemOperation("scim-replace-group", http.MethodPut, path, "Replace a SCIM group"), reflect.TypeFor[GroupInput](), reflect.TypeFor[GroupResource](), true, func(ctx huma.Context) {
		auth, ok := authenticateSCIM(ctx, service)
		if !ok {
			return
		}
		version, err := parseETag(ctx.Header("If-Match"))
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		var input GroupInput
		if err := decodeSCIMBody(ctx, &input); err != nil {
			writeSCIMError(ctx, err)
			return
		}
		result, err := service.ReplaceGroup(ctx.Context(), auth, ctx.Param("id"), input, version)
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		ctx.SetHeader("ETag", result.Meta.Version)
		writeSCIM(ctx, http.StatusOK, result)
	})
	registerSCIM(api, itemOperation("scim-patch-group", http.MethodPatch, path, "Patch a SCIM group"), reflect.TypeFor[PatchRequest](), reflect.TypeFor[GroupResource](), true, func(ctx huma.Context) {
		auth, ok := authenticateSCIM(ctx, service)
		if !ok {
			return
		}
		version, err := parseETag(ctx.Header("If-Match"))
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		var input PatchRequest
		if err := decodeSCIMBody(ctx, &input); err != nil {
			writeSCIMError(ctx, err)
			return
		}
		result, err := service.PatchGroup(ctx.Context(), auth, ctx.Param("id"), input, version)
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		ctx.SetHeader("ETag", result.Meta.Version)
		writeSCIM(ctx, http.StatusOK, result)
	})
	registerSCIM(api, itemOperation("scim-delete-group", http.MethodDelete, path, "Delete a SCIM group"), nil, nil, true, func(ctx huma.Context) {
		auth, ok := authenticateSCIM(ctx, service)
		if !ok {
			return
		}
		version, err := parseETag(ctx.Header("If-Match"))
		if err == nil {
			err = service.DeleteGroup(ctx.Context(), auth, ctx.Param("id"), version)
		}
		if err != nil {
			writeSCIMError(ctx, err)
			return
		}
		writeSCIM(ctx, http.StatusNoContent, nil)
	})
}

func registerDiscovery(api huma.API) {
	registerSCIM(api, huma.Operation{OperationID: "scim-service-provider-config", Method: http.MethodGet, Path: "/scim/v2/ServiceProviderConfig", Summary: "Get SCIM service provider configuration", Tags: []string{"SCIM 2.0"}}, nil, reflect.TypeFor[ServiceProviderConfig](), false, func(ctx huma.Context) { writeSCIM(ctx, http.StatusOK, serviceProviderConfig()) })
	registerSCIM(api, huma.Operation{OperationID: "scim-list-schemas", Method: http.MethodGet, Path: "/scim/v2/Schemas", Summary: "List SCIM schemas", Tags: []string{"SCIM 2.0"}}, nil, reflect.TypeFor[ListResponse[SCIMSchemaResource]](), false, func(ctx huma.Context) {
		items := schemaResources()
		writeSCIM(ctx, http.StatusOK, ListResponse[SCIMSchemaResource]{Schemas: []string{ListSchema}, TotalResults: len(items), StartIndex: 1, ItemsPerPage: len(items), Resources: items})
	})
	registerSCIM(api, itemOperation("scim-get-schema", http.MethodGet, "/scim/v2/Schemas/{id}", "Get a SCIM schema"), nil, reflect.TypeFor[SCIMSchemaResource](), false, func(ctx huma.Context) {
		for _, item := range schemaResources() {
			if item.ID == ctx.Param("id") {
				writeSCIM(ctx, http.StatusOK, item)
				return
			}
		}
		writeSCIMError(ctx, ErrResourceMissing)
	})
	registerSCIM(api, huma.Operation{OperationID: "scim-list-resource-types", Method: http.MethodGet, Path: "/scim/v2/ResourceTypes", Summary: "List SCIM resource types", Tags: []string{"SCIM 2.0"}}, nil, reflect.TypeFor[ListResponse[ResourceTypeResource]](), false, func(ctx huma.Context) {
		items := resourceTypes()
		writeSCIM(ctx, http.StatusOK, ListResponse[ResourceTypeResource]{Schemas: []string{ListSchema}, TotalResults: len(items), StartIndex: 1, ItemsPerPage: len(items), Resources: items})
	})
	registerSCIM(api, itemOperation("scim-get-resource-type", http.MethodGet, "/scim/v2/ResourceTypes/{id}", "Get a SCIM resource type"), nil, reflect.TypeFor[ResourceTypeResource](), false, func(ctx huma.Context) {
		for _, item := range resourceTypes() {
			if item.ID == ctx.Param("id") {
				writeSCIM(ctx, http.StatusOK, item)
				return
			}
		}
		writeSCIMError(ctx, ErrResourceMissing)
	})
}

func registerSCIM(api huma.API, op huma.Operation, request, response reflect.Type, secured bool, handler func(huma.Context)) {
	authz.Public(&op)
	if secured {
		components := api.OpenAPI().Components
		if components.SecuritySchemes == nil {
			components.SecuritySchemes = map[string]*huma.SecurityScheme{}
		}
		components.SecuritySchemes[scimBearerScheme] = &huma.SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: "SCIM directory token"}
		op.Security = []map[string][]string{{scimBearerScheme: {}}}
	} else {
		op.Security = []map[string][]string{}
	}
	if request != nil {
		op.RequestBody = &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{SCIMContentType: {Schema: huma.SchemaFromType(api.OpenAPI().Components.Schemas, request)}}}
	}
	op.Responses = map[string]*huma.Response{}
	if response == nil {
		op.Responses["204"] = &huma.Response{Description: "The operation completed successfully"}
	} else {
		status := "200"
		if op.Method == http.MethodPost {
			status = "201"
		}
		op.Responses[status] = scimOpenAPIResponse("SCIM response", huma.SchemaFromType(api.OpenAPI().Components.Schemas, response))
	}
	errorSchema := huma.SchemaFromType(api.OpenAPI().Components.Schemas, reflect.TypeFor[SCIMError]())
	for _, status := range []string{"400", "401", "404", "409", "412", "413", "500"} {
		op.Responses[status] = scimOpenAPIResponse("SCIM error", errorSchema)
	}
	api.OpenAPI().AddOperation(&op)
	api.Adapter().Handle(&op, handler)
}

func scimOpenAPIResponse(description string, schema *huma.Schema) *huma.Response {
	return &huma.Response{Description: description, Content: map[string]*huma.MediaType{SCIMContentType: {Schema: schema}}}
}

func itemOperation(id, method, path, summary string) huma.Operation {
	return huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary, Tags: []string{"SCIM 2.0"}, Parameters: []*huma.Param{{Name: "id", In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(512)}}}}
}

func listParameters() []*huma.Param {
	return []*huma.Param{
		{Name: "filter", In: "query", Schema: &huma.Schema{Type: huma.TypeString, MaxLength: intPointer(maxFilterBytes)}},
		{Name: "startIndex", In: "query", Schema: &huma.Schema{Type: huma.TypeInteger}},
		{Name: "count", In: "query", Schema: &huma.Schema{Type: huma.TypeInteger}},
	}
}

func authenticateSCIM(ctx huma.Context, service *Service) (AuthContext, bool) {
	if service == nil {
		return AuthContext{}, false
	}
	auth, err := service.Authenticate(ctx.Context(), ctx.Header("Authorization"))
	if err != nil {
		writeSCIMError(ctx, err)
		return AuthContext{}, false
	}
	return auth, true
}

func listRequest(ctx huma.Context) (ListRequest, error) {
	start, err := parseQueryInt(ctx.Query("startIndex"))
	if err != nil {
		return ListRequest{}, ErrInvalidSCIM
	}
	count, err := parseQueryInt(ctx.Query("count"))
	if err != nil {
		return ListRequest{}, ErrInvalidSCIM
	}
	requestURL := ctx.URL()
	if requestURL.Query().Has("count") && count == 0 {
		count = -1
	}
	return normalizeListRequest(ctx.Query("filter"), start, count)
}

func parseQueryInt(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}

func decodeSCIMBody(ctx huma.Context, target any) error {
	mediaType, _, err := mime.ParseMediaType(ctx.Header("Content-Type"))
	if err != nil || mediaType != SCIMContentType && mediaType != "application/json" {
		return ErrInvalidSCIM
	}
	data, err := io.ReadAll(io.LimitReader(ctx.BodyReader(), maxBodyBytes+1))
	if err != nil {
		return ErrInvalidSCIM
	}
	if len(data) > maxBodyBytes {
		return ErrTooMany
	}
	return strictDecode(data, target)
}

func writeSCIMError(ctx huma.Context, err error) {
	protocolError := mapSCIMError(ctx.Context(), err)
	if protocolError.status >= http.StatusInternalServerError {
		slog.Error("SCIM request failed", "operation", ctx.Operation().OperationID, "err", err)
	}
	for name, values := range protocolError.headers {
		for _, value := range values {
			ctx.AppendHeader(name, value)
		}
	}
	writeSCIM(ctx, protocolError.status, protocolError)
}

func writeSCIM(ctx huma.Context, status int, value any) {
	ctx.SetHeader("Content-Type", SCIMContentType)
	ctx.SetStatus(status)
	if value == nil {
		return
	}
	if err := json.NewEncoder(ctx.BodyWriter()).Encode(value); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		slog.Error("write SCIM response", "operation", ctx.Operation().OperationID, "err", err)
	}
}

func intPointer(value int) *int { return &value }
