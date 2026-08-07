package policyx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	MaxMemberRuleBytes      = 4096
	maxMemberRuleConditions = 16
	maxMemberRuleValues     = 16
)

type MemberRule struct {
	Version    int               `json:"version"`
	Match      string            `json:"match"`
	Conditions []MemberCondition `json:"conditions"`
}

type MemberCondition struct {
	Field    string   `json:"field"`
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
}

type MemberFacts struct {
	Subject      string
	Email        string
	DepartmentID string
	Status       string
}

func CanonicalMemberRule(raw json.RawMessage) (json.RawMessage, error) {
	rule, err := parseMemberRule(raw)
	if err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("encode member rule: %w", err)
	}
	return canonical, nil
}

func EvaluateMemberRule(raw json.RawMessage, facts MemberFacts) (bool, error) {
	rule, err := parseMemberRule(raw)
	if err != nil {
		return false, err
	}
	matched := rule.Match == "all"
	for _, condition := range rule.Conditions {
		conditionMatched := memberConditionMatches(condition, facts)
		if rule.Match == "all" && !conditionMatched {
			return false, nil
		}
		if rule.Match == "any" && conditionMatched {
			return true, nil
		}
	}
	return matched, nil
}

func parseMemberRule(raw json.RawMessage) (MemberRule, error) {
	if len(raw) > MaxMemberRuleBytes {
		return MemberRule{}, fmt.Errorf("member rule exceeds %d bytes", MaxMemberRuleBytes)
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return MemberRule{}, fmt.Errorf("member rule is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var rule MemberRule
	if err := decoder.Decode(&rule); err != nil {
		return MemberRule{}, fmt.Errorf("decode member rule: %w", err)
	}
	if err := rejectMemberRuleTrailingJSON(decoder); err != nil {
		return MemberRule{}, err
	}
	if rule.Version != 1 {
		return MemberRule{}, fmt.Errorf("member rule version must be 1")
	}
	if rule.Match != "all" && rule.Match != "any" {
		return MemberRule{}, fmt.Errorf("member rule match must be all or any")
	}
	if len(rule.Conditions) == 0 || len(rule.Conditions) > maxMemberRuleConditions {
		return MemberRule{}, fmt.Errorf("member rule requires 1 to %d conditions", maxMemberRuleConditions)
	}
	for index := range rule.Conditions {
		condition := &rule.Conditions[index]
		if !validMemberRuleField(condition.Field) {
			return MemberRule{}, fmt.Errorf("unknown member rule field %q", condition.Field)
		}
		if condition.Operator != "in" && condition.Operator != "not_in" {
			return MemberRule{}, fmt.Errorf("unknown member rule operator %q", condition.Operator)
		}
		if len(condition.Values) == 0 || len(condition.Values) > maxMemberRuleValues {
			return MemberRule{}, fmt.Errorf("member rule condition requires 1 to %d values", maxMemberRuleValues)
		}
		seen := make(map[string]struct{}, len(condition.Values))
		values := condition.Values[:0]
		for _, value := range condition.Values {
			value = strings.TrimSpace(value)
			if condition.Field == "member.email" || condition.Field == "member.email_domain" || condition.Field == "member.status" {
				value = strings.ToLower(value)
			}
			if value == "" || len(value) > 320 {
				return MemberRule{}, fmt.Errorf("member rule values must be non-empty and at most 320 characters")
			}
			if _, exists := seen[value]; !exists {
				seen[value] = struct{}{}
				values = append(values, value)
			}
		}
		condition.Values = values
	}
	return rule, nil
}

func rejectMemberRuleTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("member rule contains trailing JSON")
		}
		return fmt.Errorf("decode member rule: %w", err)
	}
	return nil
}

func validMemberRuleField(field string) bool {
	switch field {
	case "member.subject", "member.email", "member.email_domain", "member.department_id", "member.status":
		return true
	default:
		return false
	}
}

func memberConditionMatches(condition MemberCondition, facts MemberFacts) bool {
	actual := facts.Subject
	switch condition.Field {
	case "member.email":
		actual = strings.ToLower(facts.Email)
	case "member.email_domain":
		actual = emailDomain(facts.Email)
	case "member.department_id":
		actual = facts.DepartmentID
	case "member.status":
		actual = strings.ToLower(facts.Status)
	}
	found := false
	for _, value := range condition.Values {
		if actual == value {
			found = true
			break
		}
	}
	if condition.Operator == "not_in" {
		return !found
	}
	return found
}

func emailDomain(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	index := strings.LastIndexByte(email, '@')
	if index < 0 || index == len(email)-1 {
		return ""
	}
	return email[index+1:]
}
