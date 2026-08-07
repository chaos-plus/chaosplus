package provisioning

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (s *Service) Bulk(ctx context.Context, auth AuthContext, request BulkRequest) (BulkResponse, error) {
	if len(request.Operations) > maxBulkOps || request.FailOnErrors > maxBulkOps {
		return BulkResponse{}, ErrTooMany
	}
	if len(request.Schemas) != 1 || request.Schemas[0] != BulkSchema || len(request.Operations) == 0 || request.FailOnErrors < 0 {
		return BulkResponse{}, ErrInvalidSCIM
	}
	response := BulkResponse{Schemas: []string{BulkReply}, Operations: make([]BulkResponseOperation, 0, len(request.Operations))}
	resolved := make(map[string]string)
	seen := make(map[string]struct{})
	failures := 0
	for _, operation := range request.Operations {
		result, resourceID, err := s.bulkOperation(ctx, auth, operation, resolved, seen)
		if err != nil {
			protocolError := mapSCIMError(ctx, err)
			body, _ := json.Marshal(protocolError)
			result = BulkResponseOperation{Method: strings.ToUpper(strings.TrimSpace(operation.Method)), BulkID: operation.BulkID, Status: strconv.Itoa(protocolError.status), Response: body}
			failures++
		} else if operation.BulkID != "" {
			resolved[operation.BulkID] = resourceID
		}
		response.Operations = append(response.Operations, result)
		if request.FailOnErrors > 0 && failures >= request.FailOnErrors {
			break
		}
	}
	return response, nil
}

func (s *Service) bulkOperation(ctx context.Context, auth AuthContext, operation BulkOperation, resolved map[string]string, seen map[string]struct{}) (BulkResponseOperation, string, error) {
	method := strings.ToUpper(strings.TrimSpace(operation.Method))
	bulkID := strings.TrimSpace(operation.BulkID)
	resourceType, resourceID, err := bulkPath(operation.Path)
	if err != nil {
		return BulkResponseOperation{}, "", err
	}
	if method == http.MethodPost {
		if resourceID != "" || bulkID == "" || len(bulkID) > 128 {
			return BulkResponseOperation{}, "", ErrInvalidSCIM
		}
		if _, exists := seen[bulkID]; exists {
			return BulkResponseOperation{}, "", ErrInvalidSCIM
		}
		seen[bulkID] = struct{}{}
	} else if bulkID != "" || resourceID == "" {
		return BulkResponseOperation{}, "", ErrInvalidSCIM
	}
	data, err := resolveBulkData(operation.Data, resolved)
	if err != nil {
		return BulkResponseOperation{}, "", err
	}
	version, err := parseETag(operation.Version)
	if err != nil {
		return BulkResponseOperation{}, "", err
	}
	result := BulkResponseOperation{Method: method, BulkID: bulkID}
	switch resourceType + " " + method {
	case ResourceUser + " " + http.MethodPost:
		var input UserInput
		if strictDecode(data, &input) != nil {
			return result, "", ErrInvalidSCIM
		}
		created, err := s.CreateUser(ctx, auth, input)
		if err != nil {
			return result, "", err
		}
		result.Location, result.Version, result.Status = created.Meta.Location, created.Meta.Version, strconv.Itoa(http.StatusCreated)
		return result, created.ID, nil
	case ResourceUser + " " + http.MethodPut:
		var input UserInput
		if strictDecode(data, &input) != nil {
			return result, "", ErrInvalidSCIM
		}
		updated, err := s.ReplaceUser(ctx, auth, resourceID, input, version)
		if err != nil {
			return result, "", err
		}
		result.Location, result.Version, result.Status = updated.Meta.Location, updated.Meta.Version, strconv.Itoa(http.StatusOK)
		return result, resourceID, nil
	case ResourceUser + " " + http.MethodPatch:
		var input PatchRequest
		if strictDecode(data, &input) != nil {
			return result, "", ErrInvalidSCIM
		}
		updated, err := s.PatchUser(ctx, auth, resourceID, input, version)
		if err != nil {
			return result, "", err
		}
		result.Location, result.Version, result.Status = updated.Meta.Location, updated.Meta.Version, strconv.Itoa(http.StatusOK)
		return result, resourceID, nil
	case ResourceUser + " " + http.MethodDelete:
		if len(data) != 0 {
			return result, "", ErrInvalidSCIM
		}
		if err := s.DeleteUser(ctx, auth, resourceID, version); err != nil {
			return result, "", err
		}
		result.Status = strconv.Itoa(http.StatusNoContent)
		return result, resourceID, nil
	case ResourceGroup + " " + http.MethodPost:
		var input GroupInput
		if strictDecode(data, &input) != nil {
			return result, "", ErrInvalidSCIM
		}
		created, err := s.CreateGroup(ctx, auth, input)
		if err != nil {
			return result, "", err
		}
		result.Location, result.Version, result.Status = created.Meta.Location, created.Meta.Version, strconv.Itoa(http.StatusCreated)
		return result, created.ID, nil
	case ResourceGroup + " " + http.MethodPut:
		var input GroupInput
		if strictDecode(data, &input) != nil {
			return result, "", ErrInvalidSCIM
		}
		updated, err := s.ReplaceGroup(ctx, auth, resourceID, input, version)
		if err != nil {
			return result, "", err
		}
		result.Location, result.Version, result.Status = updated.Meta.Location, updated.Meta.Version, strconv.Itoa(http.StatusOK)
		return result, resourceID, nil
	case ResourceGroup + " " + http.MethodPatch:
		var input PatchRequest
		if strictDecode(data, &input) != nil {
			return result, "", ErrInvalidSCIM
		}
		updated, err := s.PatchGroup(ctx, auth, resourceID, input, version)
		if err != nil {
			return result, "", err
		}
		result.Location, result.Version, result.Status = updated.Meta.Location, updated.Meta.Version, strconv.Itoa(http.StatusOK)
		return result, resourceID, nil
	case ResourceGroup + " " + http.MethodDelete:
		if len(data) != 0 {
			return result, "", ErrInvalidSCIM
		}
		if err := s.DeleteGroup(ctx, auth, resourceID, version); err != nil {
			return result, "", err
		}
		result.Status = strconv.Itoa(http.StatusNoContent)
		return result, resourceID, nil
	default:
		return result, "", ErrInvalidSCIM
	}
}

func bulkPath(raw string) (string, string, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.IsAbs() {
		return "", "", ErrInvalidSCIM
	}
	path := strings.TrimPrefix(parsed.Path, "/scim/v2")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 1 || len(parts) > 2 || parts[0] != "Users" && parts[0] != "Groups" {
		return "", "", ErrInvalidSCIM
	}
	resourceType := strings.TrimSuffix(parts[0], "s")
	if len(parts) == 1 {
		return resourceType, "", nil
	}
	id, err := url.PathUnescape(parts[1])
	if err != nil || strings.TrimSpace(id) == "" || len(id) > 128 {
		return "", "", ErrInvalidSCIM
	}
	return resourceType, id, nil
}

func resolveBulkData(data json.RawMessage, resolved map[string]string) (json.RawMessage, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var value any
	if strictDecode(data, &value) != nil {
		return nil, ErrInvalidSCIM
	}
	resolvedValue, err := resolveBulkValue(value, resolved)
	if err != nil {
		return nil, err
	}
	result, err := json.Marshal(resolvedValue)
	if err != nil {
		return nil, ErrInvalidSCIM
	}
	return result, nil
}

func resolveBulkValue(value any, resolved map[string]string) (any, error) {
	switch typed := value.(type) {
	case string:
		id, ok := strings.CutPrefix(typed, "bulkId:")
		if !ok {
			return typed, nil
		}
		resolvedID, exists := resolved[id]
		if !exists {
			return nil, ErrInvalidPath
		}
		return resolvedID, nil
	case []any:
		for index, item := range typed {
			resolvedItem, err := resolveBulkValue(item, resolved)
			if err != nil {
				return nil, err
			}
			typed[index] = resolvedItem
		}
	case map[string]any:
		for key, item := range typed {
			resolvedItem, err := resolveBulkValue(item, resolved)
			if err != nil {
				return nil, err
			}
			typed[key] = resolvedItem
		}
	}
	return value, nil
}
