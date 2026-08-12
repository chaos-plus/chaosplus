package organization

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
)

type invitationListInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
}

type invitationIDInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"32"`
}

type createInvitationInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		Email          string   `json:"email" format:"email" maxLength:"320"`
		DepartmentID   string   `json:"department_id,omitempty" maxLength:"128"`
		RoleIDs        []string `json:"role_ids,omitempty" maxItems:"50"`
		ExpiresInHours int      `json:"expires_in_hours,omitempty" minimum:"1" maximum:"720" default:"72"`
	}
}

type resendInvitationInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"32"`
	Body     struct {
		ExpiresInHours int `json:"expires_in_hours,omitempty" minimum:"1" maximum:"720" default:"72"`
	}
}

type acceptInvitationInput struct {
	Body struct {
		Token       string `json:"token" minLength:"40" maxLength:"256"`
		LoginName   string `json:"login_name" minLength:"1" maxLength:"200"`
		Password    string `json:"password" minLength:"12" maxLength:"1024"`
		DisplayName string `json:"display_name,omitempty" maxLength:"128"`
	}
}

type issuedInvitation struct {
	Invitation Invitation `json:"invitation"`
	Token      string     `json:"token" doc:"single-display bearer credential; never stored in plaintext"`
}

type revokedInvitation struct {
	Revoked bool `json:"revoked"`
}

func RegisterInvitationREST(api huma.API, service *InvitationService, registrar *authz.Registrar) {
	authz.Register(registrar, api, huma.Operation{OperationID: "organization-list-invitations", Method: http.MethodGet, Path: "/iam/invitations", Summary: "List tenant invitations", Tags: []string{"organization"}, Errors: []int{http.StatusUnprocessableEntity, http.StatusInternalServerError}}, authz.Guard{Resource: "invitation", Verb: "view"}, func(ctx context.Context, in *invitationListInput) (*respx.Body[[]Invitation], error) {
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		items, err := service.List(ctx, tenantID)
		if err != nil {
			return nil, invitationError(err)
		}
		return respx.OK(ctx, items), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-create-invitation", Method: http.MethodPost, Path: "/iam/invitations", Summary: "Create a tenant invitation and return its credential once", Tags: []string{"organization"}, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusInternalServerError}}, authz.Guard{Resource: "invitation", Verb: "create"}, func(ctx context.Context, in *createInvitationInput) (*respx.Body[issuedInvitation], error) {
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		departmentID, err := parseOptionalOrganizationID(in.Body.DepartmentID)
		if err != nil {
			return nil, err
		}
		roleIDs := make([]guid.ID, 0, len(in.Body.RoleIDs))
		for _, roleID := range in.Body.RoleIDs {
			id, err := parseOrganizationID(roleID)
			if err != nil {
				return nil, err
			}
			roleIDs = append(roleIDs, id)
		}
		item, token, err := service.Create(ctx, tenantID, CreateInvitation{Email: in.Body.Email, DepartmentID: departmentID, RoleIDs: roleIDs, TTL: time.Duration(in.Body.ExpiresInHours) * time.Hour})
		if err != nil {
			return nil, invitationError(err)
		}
		return respx.OK(ctx, issuedInvitation{Invitation: item, Token: token}), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-revoke-invitation", Method: http.MethodDelete, Path: "/iam/invitations/{id}", Summary: "Revoke a pending tenant invitation", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusUnprocessableEntity, http.StatusInternalServerError}}, authz.Guard{Resource: "invitation", Verb: "revoke"}, func(ctx context.Context, in *invitationIDInput) (*respx.Body[revokedInvitation], error) {
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseOrganizationID(in.ID)
		if err != nil {
			return nil, err
		}
		if err := service.Revoke(ctx, tenantID, id); err != nil {
			return nil, invitationError(err)
		}
		return respx.OK(ctx, revokedInvitation{Revoked: true}), nil
	})

	authz.Register(registrar, api, huma.Operation{OperationID: "organization-resend-invitation", Method: http.MethodPost, Path: "/iam/invitations/{id}/resend", Summary: "Rotate and return a pending invitation credential once", Tags: []string{"organization"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusInternalServerError}}, authz.Guard{Resource: "invitation", Verb: "resend"}, func(ctx context.Context, in *resendInvitationInput) (*respx.Body[issuedInvitation], error) {
		tenantID, err := parseOrganizationID(in.TenantID)
		if err != nil {
			return nil, err
		}
		id, err := parseOrganizationID(in.ID)
		if err != nil {
			return nil, err
		}
		item, token, err := service.Resend(ctx, tenantID, id, time.Duration(in.Body.ExpiresInHours)*time.Hour)
		if err != nil {
			return nil, invitationError(err)
		}
		return respx.OK(ctx, issuedInvitation{Invitation: item, Token: token}), nil
	})

	authz.RegisterPublic(api, huma.Operation{OperationID: "organization-accept-invitation", Method: http.MethodPost, Path: "/iam/invitations/accept", Summary: "Accept a tenant invitation and create the local principal", Tags: []string{"organization"}, Security: []map[string][]string{}, Errors: []int{http.StatusBadRequest, http.StatusConflict, http.StatusGone, http.StatusInternalServerError}}, func(ctx context.Context, in *acceptInvitationInput) (*respx.Body[InvitationAcceptance], error) {
		accepted, err := service.Accept(ctx, AcceptInvitation{Token: in.Body.Token, LoginName: in.Body.LoginName, Password: in.Body.Password, DisplayName: in.Body.DisplayName})
		if err != nil {
			return nil, invitationError(err)
		}
		return respx.OK(ctx, accepted), nil
	})
}

func invitationError(err error) error {
	switch {
	case errors.Is(err, ErrInvitationNotFound):
		return huma.Error404NotFound("invitation_not_found")
	case errors.Is(err, ErrInvitationCredential):
		return huma.Error400BadRequest("invalid_invitation_credential")
	case errors.Is(err, ErrInvitationExpired):
		return huma.NewError(http.StatusGone, "invitation_expired")
	case errors.Is(err, ErrInvitationState):
		return huma.Error409Conflict("invitation_state_conflict")
	case errors.Is(err, ErrInvitationLoginConflict):
		return huma.Error409Conflict("invitation_login_name_exists")
	case errors.Is(err, ErrInvitationBindingMissing):
		return huma.Error404NotFound("invitation_binding_not_found")
	case errors.Is(err, ErrInvitationBindingInactive):
		return huma.Error409Conflict("invitation_binding_inactive")
	case errors.Is(err, ErrInvitationInvalid):
		return huma.Error422UnprocessableEntity("invalid_invitation")
	default:
		return huma.Error500InternalServerError("organization_unavailable")
	}
}
