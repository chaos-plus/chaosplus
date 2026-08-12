package workflow

import (
	"encoding/json"
	"testing"
)

func TestApproversJSONRoundTrip(t *testing.T) {
	for _, original := range []Approvers{{Any: true}, {Member: []string{"alice", "bob"}}} {
		body, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var restored Approvers
		if err := json.Unmarshal(body, &restored); err != nil {
			t.Fatalf("round-trip %s: %v", body, err)
		}
		if restored.Any != original.Any || len(restored.Member) != len(original.Member) {
			t.Fatalf("round-trip = %+v, want %+v", restored, original)
		}
	}
}
