package provisioning

import (
	"context"
	"net/http"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/danielgtaylor/huma/v2"
)

type targetListInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
}

type targetIDInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	TargetID string `path:"target_id" maxLength:"128"`
}

type targetCreateInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		Name        string `json:"name" minLength:"1" maxLength:"128"`
		BaseURL     string `json:"base_url" minLength:"1" maxLength:"1024"`
		BearerToken string `json:"bearer_token" minLength:"1" maxLength:"4096"`
	}
}

type targetReplaceInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	TargetID string `path:"target_id" maxLength:"128"`
	Body     struct {
		Name        string `json:"name" minLength:"1" maxLength:"128"`
		BaseURL     string `json:"base_url" minLength:"1" maxLength:"1024"`
		Status      string `json:"status" enum:"active,disabled"`
		Version     int64  `json:"version" minimum:"1"`
		BearerToken string `json:"bearer_token,omitempty" maxLength:"4096" doc:"Optional new bearer token; empty keeps the existing one."`
	}
}

type targetPushInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	TargetID string `path:"target_id" maxLength:"128"`
	Body     struct {
		ResourceType string `json:"resource_type" enum:"User,Group"`
		ResourceID   string `json:"resource_id" minLength:"1" maxLength:"128"`
	}
}

// RegisterTargetREST exposes the outbound SCIM target management API, guarded
// by the tenant administer permission like the rest of provisioning.
func RegisterTargetREST(api huma.API, service *Service, registrar *authz.Registrar) {
	guard := authz.Guard{Resource: "tenant", Verb: "administer"}
	authz.Register(registrar, api, huma.Operation{OperationID: "provisioning-list-scim-targets", Method: http.MethodGet, Path: "/iam/scim/targets", Summary: "List outbound SCIM targets", Tags: []string{"provisioning"}}, guard, func(ctx context.Context, in *targetListInput) (*respx.Body[[]Target], error) {
		tenantID, err := parseProvisioningID(in.TenantID)
		if err != nil {
			return nil, err
		}
		items, err := service.ListTargets(ctx, tenantID)
		if err != nil {
			return nil, provisioningError(err)
		}
		return respx.OK(ctx, items), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "provisioning-create-scim-target", Method: http.MethodPost, Path: "/iam/scim/targets", Summary: "Create an outbound SCIM target", Tags: []string{"provisioning"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity}}, guard, func(ctx context.Context, in *targetCreateInput) (*respx.Body[TargetSecret], error) {
		tenantID, err := parseProvisioningID(in.TenantID)
		if err != nil {
			return nil, err
		}
		item, err := service.CreateTarget(ctx, tenantID, in.Body.Name, in.Body.BaseURL, in.Body.BearerToken)
		if err != nil {
			return nil, provisioningError(err)
		}
		return respx.OK(ctx, item), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "provisioning-replace-scim-target", Method: http.MethodPut, Path: "/iam/scim/targets/{target_id}", Summary: "Replace an outbound SCIM target", Tags: []string{"provisioning"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, guard, func(ctx context.Context, in *targetReplaceInput) (*respx.Body[Target], error) {
		tenantID, err := parseProvisioningID(in.TenantID)
		if err != nil {
			return nil, err
		}
		targetID, err := parseProvisioningID(in.TargetID)
		if err != nil {
			return nil, err
		}
		item, err := service.ReplaceTarget(ctx, tenantID, targetID, in.Body.Name, in.Body.BaseURL, in.Body.Status, in.Body.BearerToken, in.Body.Version)
		if err != nil {
			return nil, provisioningError(err)
		}
		return respx.OK(ctx, item), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "provisioning-delete-scim-target", Method: http.MethodDelete, Path: "/iam/scim/targets/{target_id}", Summary: "Delete an outbound SCIM target", Tags: []string{"provisioning"}, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity}}, guard, func(ctx context.Context, in *targetIDInput) (*respx.Body[map[string]bool], error) {
		tenantID, err := parseProvisioningID(in.TenantID)
		if err != nil {
			return nil, err
		}
		targetID, err := parseProvisioningID(in.TargetID)
		if err != nil {
			return nil, err
		}
		if err := service.DeleteTarget(ctx, tenantID, targetID); err != nil {
			return nil, provisioningError(err)
		}
		return respx.OK(ctx, map[string]bool{"deleted": true}), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "provisioning-push-scim-resource", Method: http.MethodPost, Path: "/iam/scim/targets/{target_id}/push", Summary: "Push a user or group to an outbound SCIM target", Tags: []string{"provisioning"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusBadGateway}}, guard, func(ctx context.Context, in *targetPushInput) (*respx.Body[PushResult], error) {
		tenantID, err := parseProvisioningID(in.TenantID)
		if err != nil {
			return nil, err
		}
		targetID, err := parseProvisioningID(in.TargetID)
		if err != nil {
			return nil, err
		}
		resourceID, err := parseProvisioningID(in.Body.ResourceID)
		if err != nil {
			return nil, err
		}
		item, err := service.PushResource(ctx, tenantID, targetID, in.Body.ResourceType, resourceID)
		if err != nil {
			return nil, provisioningError(err)
		}
		return respx.OK(ctx, item), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "provisioning-deprovision-scim-resource", Method: http.MethodPost, Path: "/iam/scim/targets/{target_id}/deprovision", Summary: "Remove a user or group from an outbound SCIM target", Tags: []string{"provisioning"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusBadGateway}}, guard, func(ctx context.Context, in *targetPushInput) (*respx.Body[map[string]bool], error) {
		tenantID, err := parseProvisioningID(in.TenantID)
		if err != nil {
			return nil, err
		}
		targetID, err := parseProvisioningID(in.TargetID)
		if err != nil {
			return nil, err
		}
		resourceID, err := parseProvisioningID(in.Body.ResourceID)
		if err != nil {
			return nil, err
		}
		if err := service.DeprovisionResource(ctx, tenantID, targetID, in.Body.ResourceType, resourceID); err != nil {
			return nil, provisioningError(err)
		}
		return respx.OK(ctx, map[string]bool{"deprovisioned": true}), nil
	})
}
