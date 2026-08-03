package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
	"github.com/danielgtaylor/huma/v2"
)

type listInput struct {
	TenantID    string `header:"X-Tenant-Id" maxLength:"128"`
	PrincipalID string `query:"principal_id" maxLength:"64"`
	EventType   string `query:"event_type" maxLength:"64"`
	Outcome     string `query:"outcome" enum:"success,denied,failure"`
	TargetType  string `query:"target_type" maxLength:"64"`
	TargetID    string `query:"target_id" maxLength:"128"`
	From        string `query:"from" maxLength:"40" doc:"inclusive RFC3339 timestamp"`
	To          string `query:"to" maxLength:"40" doc:"exclusive RFC3339 timestamp"`
	Offset      int    `query:"offset" minimum:"0" default:"0"`
	Limit       int    `query:"limit" minimum:"1" maximum:"200" default:"50"`
}

type eventInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
	ID       string `path:"id" maxLength:"64"`
}

type tenantInput struct {
	TenantID string `header:"X-Tenant-Id" maxLength:"128"`
}

type exportInput struct {
	TenantID    string `header:"X-Tenant-Id" maxLength:"128"`
	PrincipalID string `query:"principal_id" maxLength:"64"`
	EventType   string `query:"event_type" maxLength:"64"`
	Outcome     string `query:"outcome" enum:"success,denied,failure"`
	TargetType  string `query:"target_type" maxLength:"64"`
	TargetID    string `query:"target_id" maxLength:"128"`
	From        string `query:"from" maxLength:"40" doc:"inclusive RFC3339 timestamp"`
	To          string `query:"to" maxLength:"40" doc:"exclusive RFC3339 timestamp"`
}

func RegisterREST(api huma.API, service *Service, registrar *authz.Registrar) {
	authz.Register(registrar, api, huma.Operation{OperationID: "audit-list-events", Method: http.MethodGet, Path: "/iam/audit-events", Summary: "List tenant audit events", Tags: []string{"audit"}, Errors: []int{http.StatusUnprocessableEntity, http.StatusInternalServerError}}, authz.Guard{Resource: "audit_event", Verb: "view"}, func(ctx context.Context, in *listInput) (*respx.Body[[]Event], error) {
		filter := Filter{TenantID: in.TenantID, PrincipalID: in.PrincipalID, EventType: in.EventType, Outcome: in.Outcome, TargetType: in.TargetType, TargetID: in.TargetID, Offset: in.Offset, Limit: in.Limit}
		var err error
		if filter.From, err = optionalTime(in.From); err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid_audit_time")
		}
		if filter.To, err = optionalTime(in.To); err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid_audit_time")
		}
		events, total, err := service.List(ctx, filter)
		if err != nil {
			return nil, auditAPIError(err)
		}
		return respx.List(ctx, events, respx.Page{Offset: in.Offset, Limit: in.Limit, Count: len(events), Total: total}), nil
	})
	authz.Register(registrar, api, exportOperation(), authz.Guard{Resource: "audit_event", Verb: "export"}, func(ctx context.Context, in *exportInput) (*huma.StreamResponse, error) {
		filter, err := exportFilter(in)
		if err != nil || validateFilter(filter, false) != nil {
			return nil, huma.Error422UnprocessableEntity("invalid_audit_time")
		}
		if _, err = service.PrepareExport(ctx, filter); err != nil {
			return nil, auditAPIError(err)
		}
		principalID, _ := authnext.SubjectFromContext(ctx)
		if _, err = service.Append(ctx, EventInput{
			TenantID: in.TenantID, PrincipalID: principalID, EventType: "audit_export_requested",
			TargetType: "audit_event", Outcome: "success", Detail: map[string]any{"filter": exportCriteriaOf(filter)},
		}); err != nil {
			return nil, auditAPIError(err)
		}
		snapshot, err := service.PrepareExport(ctx, filter)
		if err != nil {
			return nil, auditAPIError(err)
		}
		filename := fmt.Sprintf("chaosplus-audit-%s.ndjson", snapshot.GeneratedAt.Format("20060102T150405Z"))
		return &huma.StreamResponse{Body: func(stream huma.Context) {
			stream.SetHeader("Content-Type", "application/x-ndjson")
			stream.SetHeader("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
			stream.SetHeader("Cache-Control", "no-store")
			if err := service.WriteExport(stream.Context(), snapshot, stream.BodyWriter()); err != nil {
				slog.Error("audit export stream failed", "tenant_id", in.TenantID, "err", err)
			}
		}}, nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "audit-get-event", Method: http.MethodGet, Path: "/iam/audit-events/{id}", Summary: "Get a tenant audit event", Tags: []string{"audit"}, Errors: []int{http.StatusNotFound, http.StatusInternalServerError}}, authz.Guard{Resource: "audit_event", Verb: "view"}, func(ctx context.Context, in *eventInput) (*respx.Body[Event], error) {
		event, err := service.Get(ctx, in.TenantID, in.ID)
		if err != nil {
			return nil, auditAPIError(err)
		}
		return respx.OK(ctx, event), nil
	})
	authz.Register(registrar, api, huma.Operation{OperationID: "audit-verify-integrity", Method: http.MethodGet, Path: "/iam/audit-integrity", Summary: "Verify the tenant audit hash chain", Tags: []string{"audit"}, Errors: []int{http.StatusInternalServerError}}, authz.Guard{Resource: "audit_event", Verb: "view"}, func(ctx context.Context, in *tenantInput) (*respx.Body[Integrity], error) {
		result, err := service.Verify(ctx, in.TenantID)
		if err != nil {
			return nil, auditAPIError(err)
		}
		return respx.OK(ctx, result), nil
	})
}

func exportOperation() huma.Operation {
	return huma.Operation{
		OperationID: "audit-export-events", Method: http.MethodGet, Path: "/iam/audit-events/export",
		Summary: "Export a verified tenant audit snapshot", Tags: []string{"audit"},
		Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity, http.StatusInternalServerError},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "A newline-delimited JSON audit snapshot ending with a completion record",
				Content: map[string]*huma.MediaType{
					"application/x-ndjson": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
				},
			},
		},
	}
}

func exportFilter(in *exportInput) (Filter, error) {
	filter := Filter{TenantID: in.TenantID, PrincipalID: in.PrincipalID, EventType: in.EventType, Outcome: in.Outcome, TargetType: in.TargetType, TargetID: in.TargetID}
	var err error
	if filter.From, err = optionalTime(in.From); err != nil {
		return Filter{}, err
	}
	if filter.To, err = optionalTime(in.To); err != nil {
		return Filter{}, err
	}
	return filter, nil
}

func optionalTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, value)
}

func auditAPIError(err error) error {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return huma.Error404NotFound("audit_event_not_found")
	case errors.Is(err, ErrInvalidEvent):
		return huma.Error422UnprocessableEntity("invalid_audit_query")
	case errors.Is(err, ErrInvalidFilter):
		return huma.Error422UnprocessableEntity("invalid_audit_query")
	case errors.Is(err, ErrIntegrity):
		return huma.Error409Conflict("audit_integrity_failed")
	default:
		return huma.Error500InternalServerError("audit_unavailable")
	}
}
