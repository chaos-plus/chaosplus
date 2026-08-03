package provisioning

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	scimfilter "github.com/scim2/filter-parser/v2"
)

const maxPatchOps = 100

type userPatchValue struct {
	UserName    *string      `json:"userName,omitempty"`
	DisplayName *string      `json:"displayName,omitempty"`
	Name        *UserName    `json:"name,omitempty"`
	Active      *bool        `json:"active,omitempty"`
	ExternalID  *string      `json:"externalId,omitempty"`
	Emails      *[]UserEmail `json:"emails,omitempty"`
}

type groupPatchValue struct {
	DisplayName *string            `json:"displayName,omitempty"`
	ExternalID  *string            `json:"externalId,omitempty"`
	Members     *[]SCIMGroupMember `json:"members,omitempty"`
}

func (s *Service) PatchUser(ctx context.Context, auth AuthContext, id string, patch PatchRequest, expectedVersion int64) (UserResource, error) {
	current, err := s.GetUser(ctx, auth, id)
	if err != nil {
		return UserResource{}, err
	}
	if !validPatch(patch) {
		return UserResource{}, ErrInvalidPath
	}
	active := current.Active
	input := UserInput{Schemas: []string{UserSchema}, ExternalID: current.ExternalID, UserName: current.UserName, Name: current.Name, DisplayName: current.DisplayName, Active: &active, Emails: current.Emails}
	for _, operation := range patch.Operations {
		if err := applyUserPatch(&input, operation); err != nil {
			return UserResource{}, err
		}
	}
	return s.ReplaceUser(ctx, auth, id, input, expectedVersion)
}

func (s *Service) PatchGroup(ctx context.Context, auth AuthContext, id string, patch PatchRequest, expectedVersion int64) (GroupResource, error) {
	current, err := s.GetGroup(ctx, auth, id)
	if err != nil {
		return GroupResource{}, err
	}
	if !validPatch(patch) {
		return GroupResource{}, ErrInvalidPath
	}
	input := GroupInput{Schemas: []string{GroupSchema}, ExternalID: current.ExternalID, DisplayName: current.DisplayName, Members: current.Members}
	for _, operation := range patch.Operations {
		if err := applyGroupPatch(&input, operation); err != nil {
			return GroupResource{}, err
		}
	}
	return s.ReplaceGroup(ctx, auth, id, input, expectedVersion)
}

func validPatch(patch PatchRequest) bool {
	return len(patch.Schemas) == 1 && patch.Schemas[0] == PatchSchema && len(patch.Operations) > 0 && len(patch.Operations) <= maxPatchOps
}

func applyUserPatch(input *UserInput, operation PatchOperation) error {
	op := strings.ToLower(strings.TrimSpace(operation.Op))
	if op != "add" && op != "replace" && op != "remove" {
		return ErrInvalidPath
	}
	if strings.TrimSpace(operation.Path) == "" {
		if op == "remove" {
			return ErrInvalidPath
		}
		var value userPatchValue
		if strictDecode(operation.Value, &value) != nil {
			return ErrInvalidPath
		}
		mergeUserPatch(input, value)
		return nil
	}
	path, err := scimfilter.ParsePath([]byte(operation.Path))
	if err != nil || !patchSchema(path.AttributePath, UserSchema) || path.ValueExpression != nil {
		return ErrInvalidPath
	}
	switch patchName(path) {
	case "username":
		if op == "remove" {
			return ErrInvalidPath
		}
		return decodePatchValue(operation.Value, &input.UserName)
	case "displayname":
		if op == "remove" {
			input.DisplayName = ""
			return nil
		}
		return decodePatchValue(operation.Value, &input.DisplayName)
	case "name.formatted":
		if op == "remove" {
			input.Name.Formatted = ""
			return nil
		}
		return decodePatchValue(operation.Value, &input.Name.Formatted)
	case "active":
		if op == "remove" {
			return ErrInvalidPath
		}
		var active bool
		if err := decodePatchValue(operation.Value, &active); err != nil {
			return err
		}
		input.Active = &active
		return nil
	case "externalid":
		if op == "remove" {
			input.ExternalID = ""
			return nil
		}
		return decodePatchValue(operation.Value, &input.ExternalID)
	case "emails":
		if op == "remove" {
			input.Emails = nil
			return nil
		}
		emails, err := decodeEmails(operation.Value)
		if err != nil {
			return err
		}
		if op == "add" {
			input.Emails = append(input.Emails, emails...)
		} else {
			input.Emails = emails
		}
		return nil
	default:
		return ErrInvalidPath
	}
}

func applyGroupPatch(input *GroupInput, operation PatchOperation) error {
	op := strings.ToLower(strings.TrimSpace(operation.Op))
	if op != "add" && op != "replace" && op != "remove" {
		return ErrInvalidPath
	}
	if strings.TrimSpace(operation.Path) == "" {
		if op == "remove" {
			return ErrInvalidPath
		}
		var value groupPatchValue
		if strictDecode(operation.Value, &value) != nil {
			return ErrInvalidPath
		}
		mergeGroupPatch(input, value, op)
		return nil
	}
	path, err := scimfilter.ParsePath([]byte(operation.Path))
	if err != nil || !patchSchema(path.AttributePath, GroupSchema) {
		return ErrInvalidPath
	}
	switch patchName(path) {
	case "displayname":
		if path.ValueExpression != nil || op == "remove" {
			return ErrInvalidPath
		}
		return decodePatchValue(operation.Value, &input.DisplayName)
	case "externalid":
		if path.ValueExpression != nil {
			return ErrInvalidPath
		}
		if op == "remove" {
			input.ExternalID = ""
			return nil
		}
		return decodePatchValue(operation.Value, &input.ExternalID)
	case "members":
		if path.ValueExpression != nil {
			if op != "remove" {
				return ErrInvalidPath
			}
			id, ok := patchFilterValue(path.ValueExpression)
			if !ok {
				return ErrInvalidPath
			}
			input.Members = removeMember(input.Members, id)
			return nil
		}
		if op == "remove" {
			input.Members = nil
			return nil
		}
		members, err := decodeMembers(operation.Value)
		if err != nil {
			return err
		}
		if op == "add" {
			input.Members = append(input.Members, members...)
		} else {
			input.Members = members
		}
		return nil
	default:
		return ErrInvalidPath
	}
}

func mergeUserPatch(input *UserInput, value userPatchValue) {
	if value.UserName != nil {
		input.UserName = *value.UserName
	}
	if value.DisplayName != nil {
		input.DisplayName = *value.DisplayName
	}
	if value.Name != nil {
		input.Name = *value.Name
	}
	if value.Active != nil {
		input.Active = value.Active
	}
	if value.ExternalID != nil {
		input.ExternalID = *value.ExternalID
	}
	if value.Emails != nil {
		input.Emails = *value.Emails
	}
}

func mergeGroupPatch(input *GroupInput, value groupPatchValue, op string) {
	if value.DisplayName != nil {
		input.DisplayName = *value.DisplayName
	}
	if value.ExternalID != nil {
		input.ExternalID = *value.ExternalID
	}
	if value.Members != nil {
		if op == "add" {
			input.Members = append(input.Members, (*value.Members)...)
		} else {
			input.Members = *value.Members
		}
	}
}

func patchSchema(path scimfilter.AttributePath, expected string) bool {
	return path.URIPrefix == nil || strings.EqualFold(*path.URIPrefix, expected)
}

func patchName(path scimfilter.Path) string {
	name := strings.ToLower(path.AttributePath.AttributeName)
	if path.AttributePath.SubAttribute != nil {
		name += "." + strings.ToLower(*path.AttributePath.SubAttribute)
	}
	if path.SubAttribute != nil {
		name += "." + strings.ToLower(*path.SubAttribute)
	}
	return name
}

func patchFilterValue(expression scimfilter.Expression) (string, bool) {
	attribute, ok := expression.(*scimfilter.AttributeExpression)
	if !ok || attribute.Operator != scimfilter.EQ || !strings.EqualFold(attribute.AttributePath.AttributeName, "value") || attribute.AttributePath.SubAttribute != nil {
		return "", false
	}
	value, ok := attribute.CompareValue.(string)
	return strings.TrimSpace(value), ok && strings.TrimSpace(value) != ""
}

func decodePatchValue(data json.RawMessage, target any) error {
	if len(bytes.TrimSpace(data)) == 0 || strictDecode(data, target) != nil {
		return ErrInvalidPath
	}
	return nil
}

func decodeEmails(data json.RawMessage) ([]UserEmail, error) {
	var values []UserEmail
	if strictDecode(data, &values) == nil {
		return values, nil
	}
	var value UserEmail
	if strictDecode(data, &value) != nil {
		return nil, ErrInvalidPath
	}
	return []UserEmail{value}, nil
}

func decodeMembers(data json.RawMessage) ([]SCIMGroupMember, error) {
	var values []SCIMGroupMember
	if strictDecode(data, &values) == nil {
		return values, nil
	}
	var value SCIMGroupMember
	if strictDecode(data, &value) != nil {
		return nil, ErrInvalidPath
	}
	return []SCIMGroupMember{value}, nil
}

func removeMember(values []SCIMGroupMember, id string) []SCIMGroupMember {
	result := values[:0]
	for _, value := range values {
		if value.Value != id {
			result = append(result, value)
		}
	}
	return result
}
