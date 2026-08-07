package provisioning

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/danielgtaylor/huma/v2"
)

type directoryListInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
}

type directoryIDInput struct {
	TenantID    string `header:"X-Tenant-Id" maxLength:"128"`
	DirectoryID string `path:"directory_id" maxLength:"128"`
}

type directoryCreateInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		Name string `json:"name" minLength:"1" maxLength:"128"`
	}
}

type directoryReplaceInput struct {
	TenantID    string `header:"X-Tenant-Id" maxLength:"128"`
	DirectoryID string `path:"directory_id" maxLength:"128"`
	Body        struct {
		Name    string `json:"name" minLength:"1" maxLength:"128"`
		Status  string `json:"status" enum:"active,disabled"`
		Version int64  `json:"version" minimum:"1"`
	}
}

type credentialCreateInput struct {
	TenantID    string `header:"X-Tenant-Id" maxLength:"128"`
	DirectoryID string `path:"directory_id" maxLength:"128"`
	Body        struct {
		Name      string     `json:"name" minLength:"1" maxLength:"128"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
}

type credentialIDInput struct {
	TenantID     string `header:"X-Tenant-Id" maxLength:"128"`
	DirectoryID  string `path:"directory_id" maxLength:"128"`
	CredentialID string `path:"credential_id" maxLength:"128"`
}

func RegisterAdminREST(api huma.API, service *Service, registrar *authz.Registrar) {
	guard := authz.Guard{Resource: "tenant", Verb: "administer"}
	authz.Register(registrar, api, huma.Operation{OperationID: "provisioning-list-scim-directories", Method: http.MethodGet, Path: "/iam/scim/directories", Summary: "List tenant SCIM directories", Tags: []string{"provisioning"}}, guard, func(ctx context.Context, in *directoryListInput) (*respx.Body[[]Directory], error) {
		items, err := service.ListDirectories(ctx, in.TenantID)
		if err != nil {
			return nil, provisioningError(err)
		}
		return respx.OK(ctx, items), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "provisioning-create-scim-directory", Method: http.MethodPost, Path: "/iam/scim/directories", Summary: "Create a tenant SCIM directory", Tags: []string{"provisioning"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity}}, guard, func(ctx context.Context, in *directoryCreateInput) (*respx.Body[Directory], error) {
		item, err := service.CreateDirectory(ctx, in.TenantID, in.Body.Name)
		if err != nil {
			return nil, provisioningError(err)
		}
		return respx.OK(ctx, item), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "provisioning-replace-scim-directory", Method: http.MethodPut, Path: "/iam/scim/directories/{directory_id}", Summary: "Replace a tenant SCIM directory", Tags: []string{"provisioning"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, guard, func(ctx context.Context, in *directoryReplaceInput) (*respx.Body[Directory], error) {
		item, err := service.ReplaceDirectory(ctx, in.TenantID, in.DirectoryID, in.Body.Name, in.Body.Status, in.Body.Version)
		if err != nil {
			return nil, provisioningError(err)
		}
		return respx.OK(ctx, item), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "provisioning-list-scim-credentials", Method: http.MethodGet, Path: "/iam/scim/directories/{directory_id}/credentials", Summary: "List SCIM directory credentials", Tags: []string{"provisioning"}, Errors: []int{http.StatusNotFound}}, guard, func(ctx context.Context, in *directoryIDInput) (*respx.Body[[]Credential], error) {
		items, err := service.ListCredentials(ctx, in.TenantID, in.DirectoryID)
		if err != nil {
			return nil, provisioningError(err)
		}
		return respx.OK(ctx, items), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "provisioning-create-scim-credential", Method: http.MethodPost, Path: "/iam/scim/directories/{directory_id}/credentials", Summary: "Create a one-display SCIM bearer credential", Tags: []string{"provisioning"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity}}, guard, func(ctx context.Context, in *credentialCreateInput) (*respx.Body[CredentialSecret], error) {
		item, err := service.CreateCredential(ctx, in.TenantID, in.DirectoryID, in.Body.Name, in.Body.ExpiresAt)
		if err != nil {
			return nil, provisioningError(err)
		}
		return respx.OK(ctx, item), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "provisioning-revoke-scim-credential", Method: http.MethodDelete, Path: "/iam/scim/directories/{directory_id}/credentials/{credential_id}", Summary: "Revoke a SCIM bearer credential", Tags: []string{"provisioning"}, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity}}, guard, func(ctx context.Context, in *credentialIDInput) (*respx.Body[map[string]bool], error) {
		if err := service.RevokeCredential(ctx, in.TenantID, in.DirectoryID, in.CredentialID); err != nil {
			return nil, provisioningError(err)
		}
		return respx.OK(ctx, map[string]bool{"revoked": true}), nil
	})
}

func provisioningError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidDirectory):
		return huma.Error422UnprocessableEntity("invalid_scim_directory")
	case errors.Is(err, ErrDirectoryMissing):
		return huma.Error404NotFound("scim_directory_not_found")
	case errors.Is(err, ErrDirectoryName):
		return huma.Error409Conflict("scim_directory_name_exists")
	case errors.Is(err, ErrDirectoryVersion):
		return huma.Error409Conflict("scim_directory_version_conflict")
	case errors.Is(err, ErrCredentialMissing):
		return huma.Error404NotFound("scim_credential_not_found")
	case errors.Is(err, ErrCredentialLimit):
		return huma.Error409Conflict("scim_credential_limit")
	case errors.Is(err, ErrInvalidTarget):
		return huma.Error422UnprocessableEntity("invalid_scim_target")
	case errors.Is(err, ErrTargetMissing):
		return huma.Error404NotFound("scim_target_not_found")
	case errors.Is(err, ErrTargetName):
		return huma.Error409Conflict("scim_target_name_exists")
	case errors.Is(err, ErrTargetVersion):
		return huma.Error409Conflict("scim_target_version_conflict")
	case errors.Is(err, ErrTargetKeyMissing):
		return huma.Error422UnprocessableEntity("scim_target_key_missing")
	case errors.Is(err, ErrTargetDisabled):
		return huma.Error409Conflict("scim_target_disabled")
	case errors.Is(err, ErrDeprovisionMissing):
		return huma.Error404NotFound("scim_target_resource_not_mapped")
	case errors.Is(err, ErrResourceMissing):
		return huma.Error404NotFound("scim_resource_not_found")
	case errors.Is(err, ErrRemoteUnavailable), errors.Is(err, ErrRemoteResponse):
		return huma.Error502BadGateway("scim_target_remote_failed")
	default:
		return huma.Error500InternalServerError("provisioning_unavailable")
	}
}
