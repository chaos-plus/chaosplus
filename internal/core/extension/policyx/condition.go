package policyx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

const (
	MaxConditionBytes = 4096
	maxConditionDepth = 8
	maxConditionNodes = 32
)

type TrustedContext struct {
	Time        time.Time
	ACR         int
	AMR         []string
	ClientID    string
	NetworkZone string
	Resource    ResourceContext
}

// ResourceContext carries server-side facts about the business resource being
// authorized. Business layers provide Owner and Attributes from their own
// resource store; the authorizer fills Type and ID from the request arguments.
// Absent facts fail closed: a resource.* condition never matches on empty data.
type ResourceContext struct {
	Type  string
	ID    string
	Owner string
	Attrs map[string]string
}

type trustedContextKey struct{}

func WithTrustedContext(ctx context.Context, trusted TrustedContext) context.Context {
	trusted.AMR = append([]string(nil), trusted.AMR...)
	trusted.Resource.Attrs = cloneAttrs(trusted.Resource.Attrs)
	return context.WithValue(ctx, trustedContextKey{}, trusted)
}

func TrustedFromContext(ctx context.Context, now time.Time) TrustedContext {
	trusted, _ := ctx.Value(trustedContextKey{}).(TrustedContext)
	trusted.Time = now.UTC()
	trusted.AMR = append([]string(nil), trusted.AMR...)
	trusted.Resource.Attrs = cloneAttrs(trusted.Resource.Attrs)
	return trusted
}

// WithResourceContext attaches resource facts to the trusted context while
// preserving the authentication fields already present.
func WithResourceContext(ctx context.Context, resource ResourceContext) context.Context {
	trusted, _ := ctx.Value(trustedContextKey{}).(TrustedContext)
	trusted.Resource = resource
	return WithTrustedContext(ctx, trusted)
}

func CanonicalCondition(raw json.RawMessage) (json.RawMessage, error) {
	value, err := parseCondition(raw)
	if err != nil || value == nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode authorization condition: %w", err)
	}
	return canonical, nil
}

func EvaluateCondition(raw json.RawMessage, trusted TrustedContext) (bool, error) {
	value, err := parseCondition(raw)
	if err != nil || value == nil {
		return value == nil && err == nil, err
	}
	return evaluateExpression(value, trusted, true)
}

func parseCondition(raw json.RawMessage) (any, error) {
	if len(raw) > MaxConditionBytes {
		return nil, fmt.Errorf("authorization condition exceeds %d bytes", MaxConditionBytes)
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode authorization condition: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return nil, err
	}
	nodes := 0
	if err := validateExpression(value, 1, &nodes, true); err != nil {
		return nil, err
	}
	return value, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("authorization condition contains trailing JSON")
		}
		return fmt.Errorf("decode authorization condition: %w", err)
	}
	return nil
}

func validateExpression(value any, depth int, nodes *int, root bool) error {
	if depth > maxConditionDepth {
		return fmt.Errorf("authorization condition exceeds depth %d", maxConditionDepth)
	}
	*nodes++
	if *nodes > maxConditionNodes {
		return fmt.Errorf("authorization condition exceeds %d nodes", maxConditionNodes)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("authorization condition expression must be an object")
	}
	if root {
		version, ok := object["version"].(json.Number)
		if !ok || version.String() != "1" {
			return fmt.Errorf("authorization condition version must be 1")
		}
	} else if _, exists := object["version"]; exists {
		return fmt.Errorf("authorization condition version is only allowed at the root")
	}
	operator, argument, err := conditionOperator(object, root)
	if err != nil {
		return err
	}
	switch operator {
	case "all", "any":
		items, ok := argument.([]any)
		if !ok || len(items) == 0 || len(items) > maxConditionNodes {
			return fmt.Errorf("authorization condition %s requires 1 to %d expressions", operator, maxConditionNodes)
		}
		for _, item := range items {
			if err := validateExpression(item, depth+1, nodes, false); err != nil {
				return err
			}
		}
	case "not":
		return validateExpression(argument, depth+1, nodes, false)
	case "eq", "neq":
		field, literal, err := comparisonOperands(argument)
		if err != nil {
			return err
		}
		if err := validateScalar(field, literal); err != nil {
			return err
		}
	case "gt", "gte", "lt", "lte":
		field, literal, err := comparisonOperands(argument)
		if err != nil {
			return err
		}
		if field != "auth.acr" || integerValue(literal) < 0 {
			return fmt.Errorf("authorization condition %s requires auth.acr and a non-negative integer", operator)
		}
	case "in":
		field, literal, err := comparisonOperands(argument)
		if err != nil {
			return err
		}
		if !isContextListField(field) {
			return fmt.Errorf("authorization condition in only supports client.id, network.zone, or resource.* fields")
		}
		if err := validateStringList(literal); err != nil {
			return err
		}
	case "contains":
		field, literal, err := comparisonOperands(argument)
		if err != nil {
			return err
		}
		if field != "auth.amr" {
			return fmt.Errorf("authorization condition contains only supports auth.amr")
		}
		if _, ok := literal.(string); !ok {
			return fmt.Errorf("authorization condition auth.amr value must be a string")
		}
	case "between_time":
		_, _, _, err := timeArguments(argument)
		return err
	default:
		return fmt.Errorf("unknown authorization condition operator %q", operator)
	}
	return nil
}

func conditionOperator(object map[string]any, root bool) (string, any, error) {
	expected := 1
	if root {
		expected = 2
	}
	if len(object) != expected {
		return "", nil, fmt.Errorf("authorization condition must contain exactly one operator")
	}
	for key, value := range object {
		if key != "version" {
			return key, value, nil
		}
	}
	return "", nil, fmt.Errorf("authorization condition operator is required")
}

func comparisonOperands(argument any) (string, any, error) {
	items, ok := argument.([]any)
	if !ok || len(items) != 2 {
		return "", nil, fmt.Errorf("authorization comparison requires context and value operands")
	}
	contextOperand, ok := items[0].(map[string]any)
	if !ok || len(contextOperand) != 1 {
		return "", nil, fmt.Errorf("authorization comparison context operand is invalid")
	}
	field, ok := contextOperand["context"].(string)
	if !ok || field == "" {
		return "", nil, fmt.Errorf("authorization comparison context field is required")
	}
	valueOperand, ok := items[1].(map[string]any)
	if !ok || len(valueOperand) != 1 {
		return "", nil, fmt.Errorf("authorization comparison value operand is invalid")
	}
	literal, ok := valueOperand["value"]
	if !ok || literal == nil {
		return "", nil, fmt.Errorf("authorization comparison value is required")
	}
	return field, literal, nil
}

func validateScalar(field string, value any) error {
	switch field {
	case "auth.acr":
		if integerValue(value) < 0 {
			return fmt.Errorf("authorization condition auth.acr value must be a non-negative integer")
		}
	case "client.id", "network.zone":
		if text, ok := value.(string); !ok || strings.TrimSpace(text) == "" || len(text) > 128 {
			return fmt.Errorf("authorization condition %s value must be a non-empty string of at most 128 characters", field)
		}
	case "resource.type", "resource.id", "resource.owner":
		if text, ok := value.(string); !ok || strings.TrimSpace(text) == "" || len(text) > 128 {
			return fmt.Errorf("authorization condition %s value must be a non-empty string of at most 128 characters", field)
		}
	default:
		if attr, ok := resourceAttrName(field); ok {
			if text, ok := value.(string); !ok || strings.TrimSpace(text) == "" || len(text) > 128 {
				return fmt.Errorf("authorization condition resource.attr.%s value must be a non-empty string of at most 128 characters", attr)
			}
			return nil
		}
		return fmt.Errorf("unknown authorization condition context field %q", field)
	}
	return nil
}

func isContextListField(field string) bool {
	switch field {
	case "client.id", "network.zone", "resource.type", "resource.id", "resource.owner":
		return true
	}
	_, ok := resourceAttrName(field)
	return ok
}

func isResourceContextField(field string) bool {
	switch field {
	case "resource.type", "resource.id", "resource.owner":
		return true
	}
	_, ok := resourceAttrName(field)
	return ok
}

// resourceAttrName validates resource.attr.<name> fields. Names are
// lowercase alphanumeric with _ . - separators, 1 to 64 characters, and never
// client-supplied: values are looked up in the server-provided Attrs map.
func resourceAttrName(field string) (string, bool) {
	const prefix = "resource.attr."
	if !strings.HasPrefix(field, prefix) {
		return "", false
	}
	name := field[len(prefix):]
	if len(name) == 0 || len(name) > 64 || name[0] < 'a' || name[0] > 'z' {
		return "", false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '.' && c != '-' {
			return "", false
		}
	}
	return name, true
}

func resourceFieldValue(field string, trusted TrustedContext) (string, bool) {
	resource := trusted.Resource
	switch field {
	case "resource.type":
		return resource.Type, resource.Type != ""
	case "resource.id":
		return resource.ID, resource.ID != ""
	case "resource.owner":
		return resource.Owner, resource.Owner != ""
	default:
		if attr, ok := resourceAttrName(field); ok {
			value, exists := resource.Attrs[attr]
			return value, exists
		}
		return "", false
	}
}

func cloneAttrs(attrs map[string]string) map[string]string {
	if attrs == nil {
		return nil
	}
	cloned := make(map[string]string, len(attrs))
	for key, value := range attrs {
		cloned[key] = value
	}
	return cloned
}

func validateStringList(value any) error {
	items, ok := value.([]any)
	if !ok || len(items) == 0 || len(items) > 16 {
		return fmt.Errorf("authorization condition value must contain 1 to 16 strings")
	}
	for _, item := range items {
		text, ok := item.(string)
		if !ok || strings.TrimSpace(text) == "" || len(text) > 128 {
			return fmt.Errorf("authorization condition list values must be non-empty strings of at most 128 characters")
		}
	}
	return nil
}

func integerValue(value any) int {
	number, ok := value.(json.Number)
	if !ok {
		return -1
	}
	parsed, err := number.Int64()
	if err != nil || parsed < 0 || parsed > 10 {
		return -1
	}
	return int(parsed)
}

func timeArguments(argument any) (time.Duration, time.Duration, *time.Location, error) {
	items, ok := argument.([]any)
	if !ok || len(items) != 3 {
		return 0, 0, nil, fmt.Errorf("authorization condition between_time requires start, end, and timezone")
	}
	startText, startOK := items[0].(string)
	endText, endOK := items[1].(string)
	zone, zoneOK := items[2].(string)
	start, startErr := time.Parse("15:04", startText)
	end, endErr := time.Parse("15:04", endText)
	location, locationErr := time.LoadLocation(zone)
	if !startOK || !endOK || !zoneOK || startErr != nil || endErr != nil || locationErr != nil || startText == endText {
		return 0, 0, nil, fmt.Errorf("authorization condition between_time requires distinct HH:MM values and a valid IANA timezone")
	}
	return time.Duration(start.Hour())*time.Hour + time.Duration(start.Minute())*time.Minute,
		time.Duration(end.Hour())*time.Hour + time.Duration(end.Minute())*time.Minute, location, nil
}

func evaluateExpression(value any, trusted TrustedContext, root bool) (bool, error) {
	object := value.(map[string]any)
	operator, argument, _ := conditionOperator(object, root)
	switch operator {
	case "all":
		for _, item := range argument.([]any) {
			matched, err := evaluateExpression(item, trusted, false)
			if err != nil || !matched {
				return false, err
			}
		}
		return true, nil
	case "any":
		for _, item := range argument.([]any) {
			matched, err := evaluateExpression(item, trusted, false)
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
		}
		return false, nil
	case "not":
		matched, err := evaluateExpression(argument, trusted, false)
		return !matched, err
	case "eq", "neq":
		field, literal, _ := comparisonOperands(argument)
		matched := scalarEqual(field, literal, trusted)
		if operator == "neq" {
			matched = !matched
		}
		return matched, nil
	case "gt", "gte", "lt", "lte":
		_, literal, _ := comparisonOperands(argument)
		expected := integerValue(literal)
		switch operator {
		case "gt":
			return trusted.ACR > expected, nil
		case "gte":
			return trusted.ACR >= expected, nil
		case "lt":
			return trusted.ACR < expected, nil
		default:
			return trusted.ACR <= expected, nil
		}
	case "in":
		field, literal, _ := comparisonOperands(argument)
		actual := trusted.ClientID
		if field == "network.zone" {
			actual = trusted.NetworkZone
		} else if isResourceContextField(field) {
			actual, _ = resourceFieldValue(field, trusted)
		}
		for _, item := range literal.([]any) {
			if actual == item.(string) {
				return true, nil
			}
		}
		return false, nil
	case "contains":
		_, literal, _ := comparisonOperands(argument)
		return slices.Contains(trusted.AMR, literal.(string)), nil
	case "between_time":
		start, end, location, err := timeArguments(argument)
		if err != nil {
			return false, err
		}
		local := trusted.Time.In(location)
		current := time.Duration(local.Hour())*time.Hour + time.Duration(local.Minute())*time.Minute
		if start < end {
			return current >= start && current < end, nil
		}
		return current >= start || current < end, nil
	default:
		return false, fmt.Errorf("unknown authorization condition operator %q", operator)
	}
}

func scalarEqual(field string, expected any, trusted TrustedContext) bool {
	switch field {
	case "auth.acr":
		return trusted.ACR == integerValue(expected)
	case "client.id":
		return trusted.ClientID == expected.(string)
	case "network.zone":
		return trusted.NetworkZone == expected.(string)
	default:
		if isResourceContextField(field) {
			actual, ok := resourceFieldValue(field, trusted)
			return ok && actual == expected.(string)
		}
		return false
	}
}
