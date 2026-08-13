package attachment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/danielgtaylor/huma/v2"
)

var Actions = []authz.Action{
	{Resource: "workspace_attachment", Verb: "create", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
	{Resource: "workspace_attachment", Verb: "view", Scope: "entity", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true},
	{Resource: "workspace_attachment", Verb: "delete", Scope: "entity", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
}

type listInput struct {
	ResourceType ResourceType `query:"resourceType"`
	ResourceID   guid.ParamID `query:"resourceId"`
}
type idInput struct {
	ID guid.ParamID `path:"id"`
}
type deleteInput struct {
	ID      guid.ParamID `path:"id"`
	Version int64        `query:"version" minimum:"1"`
}
type uploadData struct {
	ResourceType ResourceType  `form:"resourceType" required:"true"`
	ResourceID   string        `form:"resourceId" required:"true" pattern:"^[1-9][0-9]*$"`
	File         huma.FormFile `form:"file" contentType:"application/octet-stream" required:"true"`
}
type uploadInput struct {
	RawBody huma.MultipartFormFiles[uploadData]
}
type body[T any] struct{ Body T }
type ok struct {
	OK bool `json:"ok"`
}

func (m *Module) RegisterREST(api huma.API) {
	register(m, api, huma.Operation{OperationID: "workspace-attachment-list", Method: http.MethodGet, Path: "/api/attachments", Summary: "List attachments", Tags: []string{"workspace-attachment"}}, "view", m.list)
	register(m, api, huma.Operation{OperationID: "workspace-attachment-upload", Method: http.MethodPost, Path: "/api/attachments", Summary: "Upload an attachment", Tags: []string{"workspace-attachment"}, DefaultStatus: http.StatusCreated, MaxBodyBytes: MaxUploadBytes + (1 << 20)}, "create", m.upload)
	register(m, api, huma.Operation{OperationID: "workspace-attachment-download", Method: http.MethodGet, Path: "/api/attachments/{id}/content", Summary: "Download attachment content", Tags: []string{"workspace-attachment"}}, "view", m.download)
	register(m, api, huma.Operation{OperationID: "workspace-attachment-delete", Method: http.MethodDelete, Path: "/api/attachments/{id}", Summary: "Delete an attachment", Tags: []string{"workspace-attachment"}}, "delete", m.delete)
}

func register[I, O any](m *Module, api huma.API, operation huma.Operation, verb string, handler func(context.Context, *I) (*O, error)) {
	authz.RegisterEntity(m.registrar, api, operation, authz.Guard{Resource: "workspace_attachment", Verb: verb}, handler)
}

func (m *Module) list(ctx context.Context, input *listInput) (*body[[]Attachment], error) {
	items, err := m.service.List(ctx, input.ResourceType, guid.ID(input.ResourceID))
	if err != nil {
		return nil, apiError(err)
	}
	return &body[[]Attachment]{Body: items}, nil
}

func (m *Module) upload(ctx context.Context, input *uploadInput) (*body[Attachment], error) {
	data := input.RawBody.Data()
	if data == nil || !data.File.IsSet {
		return nil, apiError(ErrInvalid)
	}
	defer func() { _ = data.File.Close() }()
	resourceID, err := guid.Parse(data.ResourceID)
	if err != nil {
		return nil, apiError(ErrInvalid)
	}
	value, err := m.service.Upload(ctx, UploadInput{ResourceType: data.ResourceType, ResourceID: resourceID, Filename: data.File.Filename, ContentType: data.File.ContentType, SizeBytes: data.File.Size, Content: data.File})
	if err != nil {
		return nil, apiError(err)
	}
	return &body[Attachment]{Body: *value}, nil
}

func (m *Module) download(ctx context.Context, input *idInput) (*huma.StreamResponse, error) {
	value, reader, err := m.service.Download(ctx, guid.ID(input.ID))
	if err != nil {
		return nil, apiError(err)
	}
	return &huma.StreamResponse{Body: func(stream huma.Context) {
		defer func() { _ = reader.Close() }()
		stream.SetHeader("Content-Type", value.ContentType)
		stream.SetHeader("Content-Length", strconv.FormatInt(value.SizeBytes, 10))
		stream.SetHeader("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": value.Filename}))
		stream.SetHeader("Cache-Control", "private, no-store")
		stream.SetHeader("X-Content-Type-Options", "nosniff")
		if _, copyErr := io.Copy(stream.BodyWriter(), reader); copyErr != nil {
			slog.Error("attachment stream failed", "attachment_id", value.ID.String(), "error", copyErr)
		}
	}}, nil
}

func (m *Module) delete(ctx context.Context, input *deleteInput) (*body[ok], error) {
	if err := m.service.Delete(ctx, guid.ID(input.ID), input.Version); err != nil {
		return nil, apiError(err)
	}
	return &body[ok]{Body: ok{OK: true}}, nil
}

func apiError(err error) error {
	switch {
	case errors.Is(err, ErrInvalid):
		return huma.Error422UnprocessableEntity("workspace.attachment.invalid")
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound("workspace.attachment.not_found")
	case errors.Is(err, ErrVersionConflict):
		return huma.Error409Conflict("workspace.attachment.version_conflict")
	case errors.Is(err, ErrUnavailable):
		return huma.Error503ServiceUnavailable("workspace.attachment.unavailable")
	default:
		slog.Error("attachment operation failed", "error", fmt.Sprintf("%T", err))
		return huma.Error503ServiceUnavailable("workspace.attachment.unavailable")
	}
}
