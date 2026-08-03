package provisioning

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	iamdomain "github.com/chaos-plus/chaosplus/internal/modules/iam/domain"
	"github.com/chaos-plus/chaosplus/internal/modules/identity"
	"github.com/chaos-plus/chaosplus/internal/modules/organization"
	"github.com/chaos-plus/chaosplus/pkg/i18n"
)

const (
	SCIMContentType = "application/scim+json"
	maxPageSize     = 200
	maxBulkOps      = 100
	maxBodyBytes    = 1024 * 1024
)

type ResourceMeta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created"`
	LastModified time.Time `json:"lastModified"`
	Location     string    `json:"location"`
	Version      string    `json:"version"`
}

type UserName struct {
	Formatted string `json:"formatted,omitempty"`
}

type UserEmail struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

type UserInput struct {
	Schemas     []string    `json:"schemas"`
	ExternalID  string      `json:"externalId,omitempty"`
	UserName    string      `json:"userName"`
	Name        UserName    `json:"name,omitempty"`
	DisplayName string      `json:"displayName,omitempty"`
	Active      *bool       `json:"active,omitempty"`
	Emails      []UserEmail `json:"emails,omitempty"`
}

type UserResource struct {
	Schemas     []string     `json:"schemas"`
	ID          string       `json:"id"`
	ExternalID  string       `json:"externalId,omitempty"`
	UserName    string       `json:"userName"`
	Name        UserName     `json:"name,omitempty"`
	DisplayName string       `json:"displayName,omitempty"`
	Active      bool         `json:"active"`
	Emails      []UserEmail  `json:"emails,omitempty"`
	Meta        ResourceMeta `json:"meta"`
}

type SCIMGroupMember struct {
	Value string `json:"value"`
	Ref   string `json:"$ref,omitempty"`
}

type GroupInput struct {
	Schemas     []string          `json:"schemas"`
	ExternalID  string            `json:"externalId,omitempty"`
	DisplayName string            `json:"displayName"`
	Members     []SCIMGroupMember `json:"members,omitempty"`
}

type GroupResource struct {
	Schemas     []string          `json:"schemas"`
	ID          string            `json:"id"`
	ExternalID  string            `json:"externalId,omitempty"`
	DisplayName string            `json:"displayName"`
	Members     []SCIMGroupMember `json:"members"`
	Meta        ResourceMeta      `json:"meta"`
}

type ListResponse[T any] struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    []T      `json:"Resources"`
}

type ListRequest struct {
	Filter     string
	StartIndex int
	Count      int
}

type PatchRequest struct {
	Schemas    []string         `json:"schemas"`
	Operations []PatchOperation `json:"Operations"`
}

type PatchOperation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

type BulkRequest struct {
	Schemas      []string        `json:"schemas"`
	FailOnErrors int             `json:"failOnErrors,omitempty"`
	Operations   []BulkOperation `json:"Operations"`
}

type BulkOperation struct {
	Method  string          `json:"method"`
	BulkID  string          `json:"bulkId,omitempty"`
	Path    string          `json:"path"`
	Version string          `json:"version,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type BulkResponse struct {
	Schemas    []string                `json:"schemas"`
	Operations []BulkResponseOperation `json:"Operations"`
}

type BulkResponseOperation struct {
	Method   string          `json:"method"`
	BulkID   string          `json:"bulkId,omitempty"`
	Location string          `json:"location,omitempty"`
	Version  string          `json:"version,omitempty"`
	Status   string          `json:"status"`
	Response json.RawMessage `json:"response,omitempty"`
}

type SCIMError struct {
	status  int
	headers http.Header

	Schemas  []string `json:"schemas"`
	Status   string   `json:"status"`
	ScimType string   `json:"scimType,omitempty"`
	Detail   string   `json:"detail"`
}

func (e *SCIMError) Error() string           { return e.Detail }
func (e *SCIMError) GetStatus() int          { return e.status }
func (e *SCIMError) GetHeaders() http.Header { return e.headers.Clone() }

func newSCIMError(ctx context.Context, status int, scimType, key string) *SCIMError {
	headers := http.Header{"Content-Type": []string{SCIMContentType}}
	if status == http.StatusUnauthorized {
		headers.Set("WWW-Authenticate", `Bearer realm="Chaosplus SCIM"`)
	}
	return &SCIMError{status: status, headers: headers, Schemas: []string{ErrorSchema}, Status: strconv.Itoa(status), ScimType: scimType, Detail: i18n.TContext(ctx, key)}
}

func mapSCIMError(ctx context.Context, err error) *SCIMError {
	if errors.Is(err, identity.ErrLoginConflict) || errors.Is(err, organization.ErrGroupNameConflict) {
		return newSCIMError(ctx, http.StatusConflict, "uniqueness", "scim_uniqueness_conflict")
	}
	if errors.Is(err, iamdomain.ErrLastTenantAdministrator) {
		return newSCIMError(ctx, http.StatusConflict, "mutability", "scim_last_tenant_administrator")
	}
	if errors.Is(err, organization.ErrGroupMemberInactive) {
		return newSCIMError(ctx, http.StatusBadRequest, "invalidValue", "scim_operation_failed")
	}
	if errors.Is(err, identity.ErrInvalid) || errors.Is(err, organization.ErrGroupInvalid) {
		return newSCIMError(ctx, http.StatusBadRequest, "invalidValue", "scim_invalid_syntax")
	}
	switch serviceErrorKind(err) {
	case ErrUnauthorized:
		return newSCIMError(ctx, http.StatusUnauthorized, "", "scim_unauthorized")
	case ErrResourceMissing:
		return newSCIMError(ctx, http.StatusNotFound, "", "scim_resource_not_found")
	case ErrResourceConflict:
		return newSCIMError(ctx, http.StatusConflict, "uniqueness", "scim_uniqueness_conflict")
	case ErrResourceVersion:
		return newSCIMError(ctx, http.StatusPreconditionFailed, "", "scim_version_conflict")
	case ErrInvalidFilter:
		return newSCIMError(ctx, http.StatusBadRequest, "invalidFilter", "scim_invalid_filter")
	case ErrInvalidPath:
		return newSCIMError(ctx, http.StatusBadRequest, "invalidPath", "scim_invalid_path")
	case ErrTooMany:
		return newSCIMError(ctx, http.StatusRequestEntityTooLarge, "tooMany", "scim_too_many")
	case ErrInvalidSCIM, ErrInvalidDirectory:
		return newSCIMError(ctx, http.StatusBadRequest, "invalidSyntax", "scim_invalid_syntax")
	default:
		return newSCIMError(ctx, http.StatusInternalServerError, "", "provisioning_unavailable")
	}
}

func normalizeListRequest(filter string, startIndex, count int) (ListRequest, error) {
	if startIndex == 0 {
		startIndex = 1
	}
	if count == -1 {
		count = 0
	} else if count == 0 {
		count = 100
	}
	if startIndex < 1 || count < 0 || count > maxPageSize || len(filter) > maxFilterBytes {
		return ListRequest{}, ErrInvalidSCIM
	}
	return ListRequest{Filter: strings.TrimSpace(filter), StartIndex: startIndex, Count: count}, nil
}

func normalizeUserInput(input UserInput) (UserInput, bool, error) {
	if !hasSchema(input.Schemas, UserSchema) || len(input.Schemas) != 1 {
		return UserInput{}, false, ErrInvalidSCIM
	}
	input.ExternalID = strings.TrimSpace(input.ExternalID)
	input.UserName = strings.ToLower(strings.TrimSpace(input.UserName))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Name.Formatted = strings.TrimSpace(input.Name.Formatted)
	if input.DisplayName == "" {
		input.DisplayName = input.Name.Formatted
	}
	if input.DisplayName == "" {
		input.DisplayName = input.UserName
	}
	if input.UserName == "" || len(input.UserName) > 200 || input.DisplayName == "" || len(input.DisplayName) > 128 || len(input.ExternalID) > 512 || len(input.Emails) > 10 {
		return UserInput{}, false, ErrInvalidSCIM
	}
	email, primarySeen := "", false
	for index := range input.Emails {
		input.Emails[index].Value = strings.ToLower(strings.TrimSpace(input.Emails[index].Value))
		input.Emails[index].Type = strings.TrimSpace(input.Emails[index].Type)
		parsed, err := mail.ParseAddress(input.Emails[index].Value)
		if err != nil || !strings.EqualFold(parsed.Address, input.Emails[index].Value) || len(input.Emails[index].Value) > 320 || len(input.Emails[index].Type) > 64 {
			return UserInput{}, false, ErrInvalidSCIM
		}
		if input.Emails[index].Primary {
			if primarySeen {
				return UserInput{}, false, ErrInvalidSCIM
			}
			primarySeen, email = true, input.Emails[index].Value
		} else if email == "" {
			email = input.Emails[index].Value
		}
	}
	input.Emails = nil
	if email != "" {
		input.Emails = []UserEmail{{Value: email, Type: "work", Primary: true}}
	}
	active := true
	if input.Active != nil {
		active = *input.Active
	}
	return input, active, nil
}

func normalizeGroupInput(input GroupInput) (GroupInput, []string, error) {
	if !hasSchema(input.Schemas, GroupSchema) || len(input.Schemas) != 1 {
		return GroupInput{}, nil, ErrInvalidSCIM
	}
	input.ExternalID = strings.TrimSpace(input.ExternalID)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.DisplayName == "" || len(input.DisplayName) > 128 || len(input.ExternalID) > 512 || len(input.Members) > 1000 {
		return GroupInput{}, nil, ErrInvalidSCIM
	}
	originalMembers := input.Members
	unique := make(map[string]struct{}, len(originalMembers))
	members := make([]string, 0, len(originalMembers))
	input.Members = input.Members[:0]
	for _, member := range originalMembers {
		member.Value = strings.TrimSpace(member.Value)
		if member.Value == "" || len(member.Value) > 128 {
			return GroupInput{}, nil, ErrInvalidSCIM
		}
		if _, ok := unique[member.Value]; ok {
			continue
		}
		unique[member.Value] = struct{}{}
		members = append(members, member.Value)
		input.Members = append(input.Members, SCIMGroupMember{Value: member.Value})
	}
	return input, members, nil
}

func strictDecode(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return ErrInvalidSCIM
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidSCIM
	}
	return nil
}

func hasSchema(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func weakETag(version int64) string { return fmt.Sprintf("W/\"%d\"", version) }

func parseETag(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	value = strings.TrimPrefix(value, "W/")
	if len(value) < 3 || value[0] != '"' || value[len(value)-1] != '"' {
		return 0, ErrResourceVersion
	}
	value = value[1 : len(value)-1]
	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil || version < 1 {
		return 0, ErrResourceVersion
	}
	return version, nil
}
