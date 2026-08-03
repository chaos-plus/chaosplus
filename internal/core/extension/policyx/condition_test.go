package policyx

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConditionCanonicalizationAndEvaluation(t *testing.T) {
	raw := json.RawMessage(`{
      "all": [
        {"gte": [{"context": "auth.acr"}, {"value": 2}]},
        {"contains": [{"context": "auth.amr"}, {"value": "mfa"}]},
        {"in": [{"context": "client.id"}, {"value": ["console", "cli"]}]},
        {"eq": [{"context": "network.zone"}, {"value": "corporate"}]},
        {"between_time": ["08:00", "20:00", "Asia/Shanghai"]}
      ],
      "version": 1
    }`)
	canonical, err := CanonicalCondition(raw)
	require.NoError(t, err)
	assert.Equal(t, `{"all":[{"gte":[{"context":"auth.acr"},{"value":2}]},{"contains":[{"context":"auth.amr"},{"value":"mfa"}]},{"in":[{"context":"client.id"},{"value":["console","cli"]}]},{"eq":[{"context":"network.zone"},{"value":"corporate"}]},{"between_time":["08:00","20:00","Asia/Shanghai"]}],"version":1}`, string(canonical))

	trusted := TrustedContext{
		Time: time.Date(2026, time.August, 3, 3, 0, 0, 0, time.UTC), ACR: 2,
		AMR: []string{"pwd", "mfa"}, ClientID: "console", NetworkZone: "corporate",
	}
	matched, err := EvaluateCondition(canonical, trusted)
	require.NoError(t, err)
	assert.True(t, matched)
	trusted.ACR = 1
	matched, err = EvaluateCondition(canonical, trusted)
	require.NoError(t, err)
	assert.False(t, matched)
}

func TestConditionBooleanAndOvernightOperators(t *testing.T) {
	condition := json.RawMessage(`{"version":1,"any":[{"not":{"eq":[{"context":"client.id"},{"value":"blocked"}]}},{"between_time":["22:00","06:00","UTC"]}]}`)
	matched, err := EvaluateCondition(condition, TrustedContext{Time: time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC), ClientID: "blocked"})
	require.NoError(t, err)
	assert.True(t, matched)

	matched, err = EvaluateCondition(json.RawMessage(`{"version":1,"all":[{"gt":[{"context":"auth.acr"},{"value":1}]},{"lte":[{"context":"auth.acr"},{"value":2}]},{"neq":[{"context":"network.zone"},{"value":"public"}]}]}`), TrustedContext{ACR: 2, NetworkZone: "corporate"})
	require.NoError(t, err)
	assert.True(t, matched)

	matched, err = EvaluateCondition(nil, TrustedContext{})
	require.NoError(t, err)
	assert.True(t, matched)

	amr := []string{"pwd"}
	ctx := WithTrustedContext(context.Background(), TrustedContext{AMR: amr, ClientID: "console"})
	amr[0] = "changed"
	now := time.Date(2026, 1, 1, 8, 0, 0, 0, time.FixedZone("test", 3600))
	trusted := TrustedFromContext(ctx, now)
	assert.Equal(t, []string{"pwd"}, trusted.AMR)
	assert.Equal(t, now.UTC(), trusted.Time)
	trusted.AMR[0] = "changed"
	assert.Equal(t, []string{"pwd"}, TrustedFromContext(ctx, now).AMR)

	matched, err = EvaluateCondition(json.RawMessage(`{"version":1,"any":[{"eq":[{"context":"client.id"},{"value":"other"}]}]}`), trusted)
	require.NoError(t, err)
	assert.False(t, matched)
	matched, err = EvaluateCondition(json.RawMessage(`{"version":1,"all":[{"lt":[{"context":"auth.acr"},{"value":2}]},{"in":[{"context":"network.zone"},{"value":["private"]}]}]}`), TrustedContext{ACR: 1, NetworkZone: "public"})
	require.NoError(t, err)
	assert.False(t, matched)
}

func TestConditionRejectsUntrustedOrUnboundedInput(t *testing.T) {
	invalid := []json.RawMessage{
		json.RawMessage(`{`),
		json.RawMessage(`[]`),
		json.RawMessage(`{}`),
		json.RawMessage(`{"version":2,"eq":[{"context":"auth.acr"},{"value":1}]}`),
		json.RawMessage(`{"version":1,"not":{"version":1,"eq":[{"context":"auth.acr"},{"value":1}]}}`),
		json.RawMessage(`{"version":1}`),
		json.RawMessage(`{"version":1,"eq":{},"neq":{}}`),
		json.RawMessage(`{"version":1,"eq":{}}`),
		json.RawMessage(`{"version":1,"eq":["auth.acr",{"value":1}]}`),
		json.RawMessage(`{"version":1,"eq":[{"unknown":"auth.acr"},{"value":1}]}`),
		json.RawMessage(`{"version":1,"eq":[{"context":"auth.acr"},1]}`),
		json.RawMessage(`{"version":1,"eq":[{"context":"auth.acr"},{"unknown":1}]}`),
		json.RawMessage(`{"version":1,"eq":[{"context":"auth.acr"},{"value":11}]}`),
		json.RawMessage(`{"version":1,"eq":[{"context":"client.id"},{"value":" "}]}`),
		json.RawMessage(`{"version":1,"sql":"tenant_id = 'other'"}`),
		json.RawMessage(`{"version":1,"eq":[{"context":"request.header.x-zone"},{"value":"trusted"}]}`),
		json.RawMessage(`{"version":1,"gte":[{"context":"client.id"},{"value":1}]}`),
		json.RawMessage(`{"version":1,"in":[{"context":"auth.acr"},{"value":["1"]}]}`),
		json.RawMessage(`{"version":1,"in":[{"context":"client.id"},{"value":[]}]}`),
		json.RawMessage(`{"version":1,"in":[{"context":"client.id"},{"value":[""]}]}`),
		json.RawMessage(`{"version":1,"contains":[{"context":"client.id"},{"value":"mfa"}]}`),
		json.RawMessage(`{"version":1,"contains":[{"context":"auth.amr"},{"value":1}]}`),
		json.RawMessage(`{"version":1,"between_time":[]}`),
		json.RawMessage(`{"version":1,"between_time":["08:00","08:00","UTC"]}`),
		json.RawMessage(`{"version":1,"all":[]}`),
		json.RawMessage(`{"version":1,"eq":[{"context":"client.id"},{"value":"client"}]} trailing`),
		json.RawMessage(strings.Repeat(" ", MaxConditionBytes+1)),
	}
	for _, condition := range invalid {
		_, err := CanonicalCondition(condition)
		assert.Error(t, err, string(condition))
		matched, evaluateErr := EvaluateCondition(condition, TrustedContext{})
		assert.False(t, matched)
		assert.Error(t, evaluateErr, string(condition))
	}

	deep := json.RawMessage(`{"version":1,"not":{"not":{"not":{"not":{"not":{"not":{"not":{"not":{"eq":[{"context":"auth.acr"},{"value":1}]}}}}}}}}}`)
	_, err := CanonicalCondition(deep)
	assert.Error(t, err)
}
