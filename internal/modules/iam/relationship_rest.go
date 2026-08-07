package iam

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/danielgtaylor/huma/v2"
)

type Relationship struct {
	EntityID        string          `json:"entity_id,omitempty"`
	SubjectType     string          `json:"subject_type" enum:"principal,group,position,entity"`
	SubjectID       string          `json:"subject_id"`
	SubjectRelation string          `json:"subject_relation,omitempty"`
	Relation        string          `json:"relation" enum:"owner,editor,viewer"`
	ResourceType    string          `json:"resource_type"`
	ResourceID      string          `json:"resource_id"`
	StartsAt        *time.Time      `json:"starts_at,omitempty"`
	EndsAt          *time.Time      `json:"ends_at,omitempty"`
	Condition       policyCondition `json:"condition,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

type relationshipFields struct {
	EntityID        string          `json:"entity_id,omitempty" maxLength:"64"`
	SubjectType     string          `json:"subject_type" enum:"principal,group,position,entity"`
	SubjectID       string          `json:"subject_id" maxLength:"128"`
	SubjectRelation string          `json:"subject_relation,omitempty" maxLength:"16"`
	Relation        string          `json:"relation" enum:"owner,editor,viewer"`
	ResourceType    string          `json:"resource_type" maxLength:"64"`
	ResourceID      string          `json:"resource_id" maxLength:"255"`
	StartsAt        *time.Time      `json:"starts_at,omitempty"`
	EndsAt          *time.Time      `json:"ends_at,omitempty"`
	Condition       policyCondition `json:"condition,omitempty"`
}

type policyCondition json.RawMessage

func (policyCondition) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{
		Type:                 "object",
		Description:          "Versioned restricted relationship condition AST evaluated only against trusted authentication context",
		AdditionalProperties: true,
	}
}

func (condition policyCondition) MarshalJSON() ([]byte, error) {
	return json.RawMessage(condition).MarshalJSON()
}

func (condition *policyCondition) UnmarshalJSON(data []byte) error {
	var raw json.RawMessage
	if err := raw.UnmarshalJSON(data); err != nil {
		return err
	}
	*condition = policyCondition(raw)
	return nil
}

type listRelationshipsInput struct {
	TenantID     string `header:"X-Tenant-Id" maxLength:"128"`
	EntityID     string `query:"entity_id" maxLength:"64"`
	ResourceType string `query:"resource_type" maxLength:"64"`
	ResourceID   string `query:"resource_id" maxLength:"255"`
}

type putRelationshipInput struct {
	TenantID string             `header:"X-Tenant-Id" maxLength:"128"`
	Body     relationshipFields `json:"body"`
}

type deleteRelationshipInput struct {
	TenantID        string `header:"X-Tenant-Id" maxLength:"128"`
	EntityID        string `query:"entity_id" maxLength:"64"`
	SubjectType     string `query:"subject_type" enum:"principal,group,position,entity"`
	SubjectID       string `query:"subject_id" maxLength:"128"`
	SubjectRelation string `query:"subject_relation" maxLength:"16"`
	Relation        string `query:"relation" enum:"owner,editor,viewer"`
	ResourceType    string `query:"resource_type" maxLength:"64"`
	ResourceID      string `query:"resource_id" maxLength:"255"`
}

type authorizationCheckInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	Body     struct {
		EntityID       string `json:"entity_id" maxLength:"64"`
		ResourceType   string `json:"resource_type,omitempty" maxLength:"64"`
		ResourceID     string `json:"resource_id,omitempty" maxLength:"255"`
		PermissionCode string `json:"permission_code" maxLength:"128"`
		Subject        string `json:"subject" maxLength:"128"`
	}
}

type AuthorizationDecision struct {
	Allowed  bool   `json:"allowed"`
	Reason   string `json:"reason"`
	Revision int64  `json:"revision"`
}

func registerRelationshipREST(a huma.API, svc *Service, registrar *authz.Registrar) {
	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-list-relationships", Method: http.MethodGet, Path: "/iam/relationships",
		Summary: "List tenant entity or business-resource relationship grants", Tags: []string{"iam"}, Errors: []int{http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "entity", Verb: "view"}, func(ctx context.Context, in *listRelationshipsInput) (*respx.Body[[]Relationship], error) {
		items, err := svc.ListRelationships(ctx, in.TenantID, iamdomain.RelationshipFilter{EntityID: in.EntityID, ResourceType: in.ResourceType, ResourceID: in.ResourceID})
		if err != nil {
			return nil, apiError("list relationships", err)
		}
		return respx.OK(ctx, relationshipsFromDomain(items)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-put-relationship", Method: http.MethodPost, Path: "/iam/relationships",
		Summary: "Create or update a constrained relationship grant", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "entity", Verb: "manage_binding"}, func(ctx context.Context, in *putRelationshipInput) (*respx.Body[Relationship], error) {
		item, _, err := svc.PutRelationship(ctx, relationshipToDomain(in.TenantID, in.Body))
		if err != nil {
			return nil, apiError("put relationship", err)
		}
		return respx.OK(ctx, relationshipFromDomain(item)), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-delete-relationship", Method: http.MethodDelete, Path: "/iam/relationships",
		Summary: "Revoke a relationship grant immediately", Tags: []string{"iam"}, Errors: []int{http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "entity", Verb: "manage_binding"}, func(ctx context.Context, in *deleteRelationshipInput) (*respx.Body[MutationResult], error) {
		changed, err := svc.DeleteRelationship(ctx, iamdomain.Relationship{
			TenantID: in.TenantID, EntityID: in.EntityID, SubjectType: in.SubjectType, SubjectID: in.SubjectID, SubjectRelation: in.SubjectRelation,
			Relation: in.Relation, ResourceType: in.ResourceType, ResourceID: in.ResourceID,
		})
		if err != nil {
			return nil, apiError("delete relationship", err)
		}
		return respx.OK(ctx, MutationResult{Changed: changed, SyncStatus: "applied"}), nil
	})

	authz.Register(registrar, a, huma.Operation{
		OperationID: "iam-check-authorization", Method: http.MethodPost, Path: "/iam/authorization/check",
		Summary: "Check an entity or business-resource authorization decision", Tags: []string{"iam"}, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, authz.Guard{Resource: "role", Verb: "view"}, func(ctx context.Context, in *authorizationCheckInput) (*respx.Body[AuthorizationDecision], error) {
		var explanation authz.Explanation
		var err error
		if in.Body.ResourceType != "" || in.Body.ResourceID != "" {
			explanation, err = svc.CheckResourceAuthorization(ctx, in.TenantID, in.Body.EntityID, in.Body.ResourceType, in.Body.ResourceID, in.Body.PermissionCode, in.Body.Subject)
		} else {
			explanation, err = svc.CheckEntityAuthorization(ctx, in.TenantID, in.Body.EntityID, in.Body.PermissionCode, in.Body.Subject)
		}
		if err != nil {
			return nil, apiError("check entity authorization", err)
		}
		return respx.OK(ctx, AuthorizationDecision{Allowed: explanation.Allowed, Reason: explanation.Reason, Revision: explanation.Revision}), nil
	})
}

func relationshipToDomain(tenantID string, fields relationshipFields) iamdomain.Relationship {
	return iamdomain.Relationship{
		TenantID: tenantID, EntityID: fields.EntityID, SubjectType: fields.SubjectType, SubjectID: fields.SubjectID, SubjectRelation: fields.SubjectRelation,
		Relation: fields.Relation, ResourceType: fields.ResourceType, ResourceID: fields.ResourceID, StartsAt: fields.StartsAt, EndsAt: fields.EndsAt, Condition: json.RawMessage(fields.Condition),
	}
}

func relationshipFromDomain(item iamdomain.Relationship) Relationship {
	return Relationship{
		EntityID: item.EntityID, SubjectType: item.SubjectType, SubjectID: item.SubjectID, SubjectRelation: item.SubjectRelation,
		Relation: item.Relation, ResourceType: item.ResourceType, ResourceID: item.ResourceID,
		StartsAt: item.StartsAt, EndsAt: item.EndsAt, Condition: policyCondition(item.Condition), CreatedAt: item.CreatedAt,
	}
}

func relationshipsFromDomain(items []iamdomain.Relationship) []Relationship {
	result := make([]Relationship, 0, len(items))
	for _, item := range items {
		result = append(result, relationshipFromDomain(item))
	}
	return result
}
