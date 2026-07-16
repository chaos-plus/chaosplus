package authz

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// ValidateOperations enforces the route declaration contract. Every HTTP
// operation must carry a valid Guard or an explicit Public marker.
func ValidateOperations(api huma.API, registry *Registry) error {
	if api == nil || registry == nil {
		return fmt.Errorf("authz operation validation requires api and registry")
	}
	var violations []string
	for path, item := range api.OpenAPI().Paths {
		for method, op := range operations(item) {
			if op == nil || isPublic(op) {
				continue
			}
			code, guarded := guardOf(op)
			if !guarded {
				violations = append(violations, fmt.Sprintf("%s %s (%s) has no authz guard", method, path, operationName(op)))
				continue
			}
			if _, ok := registry.Find(code); !ok {
				violations = append(violations, fmt.Sprintf("%s %s: %s", method, path, invalidGuardMessage(op, code)))
			}
		}
	}
	if len(violations) == 0 {
		return nil
	}
	sort.Strings(violations)
	return fmt.Errorf("authz route declaration gate failed: %s", strings.Join(violations, "; "))
}

func operations(item *huma.PathItem) map[string]*huma.Operation {
	if item == nil {
		return nil
	}
	return map[string]*huma.Operation{
		http.MethodGet:     item.Get,
		http.MethodPost:    item.Post,
		http.MethodPut:     item.Put,
		http.MethodPatch:   item.Patch,
		http.MethodDelete:  item.Delete,
		http.MethodOptions: item.Options,
		http.MethodHead:    item.Head,
		http.MethodTrace:   item.Trace,
	}
}

func operationName(op *huma.Operation) string {
	if op == nil || op.OperationID == "" {
		return "unnamed"
	}
	return op.OperationID
}
