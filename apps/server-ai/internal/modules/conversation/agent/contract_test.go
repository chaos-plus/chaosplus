package agent

import (
	"reflect"
	"sort"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// The create body's optional fields are only optional at the HTTP boundary:
// huma derives `required` from the struct tags, and service-level tests never
// cross that boundary. Dropping `required:"false"` from a field silently makes
// every client form that omits it fail with 422, which is exactly the
// regression this locks down.
func TestCreateInputRequiresOnlyMandatoryFields(t *testing.T) {
	registry := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	schema := registry.Schema(reflect.TypeOf(AgentCreateInput{}), false, "AgentCreateInput")
	got := append([]string(nil), schema.Required...)
	want := []string{"name", "kind", "runtime", "machineId"}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("required = %v, want %v", got, want)
	}
}
