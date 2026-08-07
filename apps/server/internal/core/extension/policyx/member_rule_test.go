package policyx

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemberRuleCanonicalizationAndEvaluation(t *testing.T) {
	raw := json.RawMessage(`{
      "conditions": [
        {"field":"member.department_id","operator":"in","values":[" engineering ","engineering"]},
        {"field":"member.email_domain","operator":"not_in","values":["EXAMPLE.NET"]}
      ],
      "match":"all",
      "version":1
    }`)
	canonical, err := CanonicalMemberRule(raw)
	require.NoError(t, err)
	assert.Equal(t, `{"version":1,"match":"all","conditions":[{"field":"member.department_id","operator":"in","values":["engineering"]},{"field":"member.email_domain","operator":"not_in","values":["example.net"]}]}`, string(canonical))

	matched, err := EvaluateMemberRule(canonical, MemberFacts{Subject: "alice", Email: "Alice@Example.COM", DepartmentID: "engineering", Status: "active"})
	require.NoError(t, err)
	assert.True(t, matched)
	matched, err = EvaluateMemberRule(canonical, MemberFacts{Subject: "bob", Email: "bob@example.net", DepartmentID: "engineering", Status: "active"})
	require.NoError(t, err)
	assert.False(t, matched)

	matched, err = EvaluateMemberRule(json.RawMessage(`{"version":1,"match":"any","conditions":[{"field":"member.subject","operator":"in","values":["alice"]},{"field":"member.status","operator":"in","values":["DISABLED"]}]}`), MemberFacts{Subject: "bob", Status: "disabled"})
	require.NoError(t, err)
	assert.True(t, matched)
}

func TestMemberRuleRejectsInvalidOrUnboundedInput(t *testing.T) {
	invalid := []json.RawMessage{
		nil,
		json.RawMessage(`null`),
		json.RawMessage(`{`),
		json.RawMessage(`{"version":2,"match":"all","conditions":[{"field":"member.status","operator":"in","values":["active"]}]}`),
		json.RawMessage(`{"version":1,"match":"none","conditions":[{"field":"member.status","operator":"in","values":["active"]}]}`),
		json.RawMessage(`{"version":1,"match":"all","conditions":[]}`),
		json.RawMessage(`{"version":1,"match":"all","conditions":[{"field":"request.ip","operator":"in","values":["127.0.0.1"]}]}`),
		json.RawMessage(`{"version":1,"match":"all","conditions":[{"field":"member.status","operator":"eq","values":["active"]}]}`),
		json.RawMessage(`{"version":1,"match":"all","conditions":[{"field":"member.status","operator":"in","values":[]}]}`),
		json.RawMessage(`{"version":1,"match":"all","conditions":[{"field":"member.status","operator":"in","values":[""]}]}`),
		json.RawMessage(`{"version":1,"match":"all","conditions":[{"field":"member.status","operator":"in","values":["active"]}],"script":"true"}`),
		json.RawMessage(`{"version":1,"match":"all","conditions":[{"field":"member.status","operator":"in","values":["active"]}]} trailing`),
		json.RawMessage(strings.Repeat(" ", MaxMemberRuleBytes+1)),
	}
	for _, raw := range invalid {
		_, err := CanonicalMemberRule(raw)
		assert.Error(t, err, string(raw))
		matched, evaluateErr := EvaluateMemberRule(raw, MemberFacts{})
		assert.False(t, matched)
		assert.Error(t, evaluateErr, string(raw))
	}

	conditions := make([]MemberCondition, maxMemberRuleConditions+1)
	for index := range conditions {
		conditions[index] = MemberCondition{Field: "member.status", Operator: "in", Values: []string{"active"}}
	}
	raw, err := json.Marshal(MemberRule{Version: 1, Match: "all", Conditions: conditions})
	require.NoError(t, err)
	_, err = CanonicalMemberRule(raw)
	assert.Error(t, err)
}
