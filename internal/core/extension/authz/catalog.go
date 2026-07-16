// Package authz holds the application's authorization catalog.
//
// The catalog is intentionally code-first: route guards, the management UI,
// menu binding, and the SpiceDB schema generator all read the same declarations
// so permission codes cannot drift between enforcement and administration.
package authz

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Action is one interface/menu/data permission exposed by the platform.
type Action struct {
	Resource   string `json:"resource" doc:"resource name, e.g. store"`
	Verb       string `json:"verb" doc:"action verb, e.g. view"`
	Code       string `json:"code" doc:"stable permission code, resource_verb"`
	Scope      string `json:"scope" doc:"highest scope that owns the grant"`
	Summary    string `json:"summary" doc:"human-readable permission summary"`
	DataScoped bool   `json:"data_scoped" doc:"true when object/data filtering also applies"`
	Menu       bool   `json:"menu" doc:"true when this action may drive menu visibility"`
}

// Guard is the compact value handlers attach to a route.
type Guard struct {
	Resource string
	Verb     string
}

// Code returns the canonical permission code for the guard.
func (g Guard) Code() string {
	return PermissionCode(g.Resource, g.Verb)
}

// PermissionCode joins resource and verb into the SpiceDB-safe tenant permission
// name used by endpoint checks and role grants.
func PermissionCode(resource, verb string) string {
	return resource + "_" + verb
}

// Registry stores a validated, lookup-friendly action catalog.
type Registry struct {
	actions map[string]Action
}

// NewRegistry builds a registry from actions. Codes are derived when omitted.
func NewRegistry(actions ...Action) (*Registry, error) {
	r := &Registry{actions: make(map[string]Action, len(actions))}
	for _, a := range actions {
		if err := r.Register(a); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// MustRegistry is for process startup declarations.
func MustRegistry(actions ...Action) *Registry {
	r, err := NewRegistry(actions...)
	if err != nil {
		panic(err)
	}
	return r
}

// Register adds one action after validating its stable code.
func (r *Registry) Register(a Action) error {
	if r.actions == nil {
		r.actions = map[string]Action{}
	}
	if a.Resource == "" || a.Verb == "" {
		return fmt.Errorf("authz action requires resource and verb")
	}
	expectedCode := PermissionCode(a.Resource, a.Verb)
	if a.Code == "" {
		a.Code = expectedCode
	} else if a.Code != expectedCode {
		return fmt.Errorf("authz code %q must equal canonical code %q", a.Code, expectedCode)
	}
	if !codePattern.MatchString(a.Resource) {
		return fmt.Errorf("invalid authz resource %q", a.Resource)
	}
	if !codePattern.MatchString(a.Verb) {
		return fmt.Errorf("invalid authz verb %q", a.Verb)
	}
	if strings.HasSuffix(a.Verb, "_role") {
		return fmt.Errorf("invalid authz verb %q: suffix _role is reserved for generated relations", a.Verb)
	}
	if !codePattern.MatchString(a.Code) {
		return fmt.Errorf("invalid authz code %q", a.Code)
	}
	if _, exists := r.actions[a.Code]; exists {
		return fmt.Errorf("duplicate authz code %q", a.Code)
	}
	r.actions[a.Code] = a
	return nil
}

// Find returns an action by canonical permission code.
func (r *Registry) Find(code string) (Action, bool) {
	a, ok := r.actions[code]
	return a, ok
}

// MustFind returns an action or panics. It is suitable for route declarations.
func (r *Registry) MustFind(code string) Action {
	a, ok := r.Find(code)
	if !ok {
		panic("unknown authz code: " + code)
	}
	return a
}

// All returns actions sorted by code for stable API and schema output.
func (r *Registry) All() []Action {
	out := make([]Action, 0, len(r.actions))
	for _, a := range r.actions {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// DefaultRegistry is the first-phase catalog for platform, tenant, merchant,
// store, user, role, dept, and menu management.
func DefaultRegistry() *Registry {
	return MustRegistry(DefaultActions()...)
}

// DefaultActions is deliberately small but complete enough for the first IAM
// screen: endpoint grants, data-scoped resources, and menu visibility all share
// these same codes.
func DefaultActions() []Action {
	return []Action{
		{Resource: "platform", Verb: "administer", Scope: "platform", Summary: "platform administration"},
		{Resource: "tenant", Verb: "create", Scope: "platform", Summary: "create tenants"},
		{Resource: "tenant", Verb: "view", Scope: "platform", Summary: "view tenants", DataScoped: true},
		{Resource: "tenant", Verb: "update", Scope: "platform", Summary: "update tenants", DataScoped: true},
		{Resource: "tenant", Verb: "delete", Scope: "platform", Summary: "delete tenants", DataScoped: true},
		{Resource: "tenant", Verb: "administer", Scope: "tenant", Summary: "tenant administration", DataScoped: true},

		{Resource: "merchant", Verb: "create", Scope: "tenant", Summary: "create merchants", DataScoped: true},
		{Resource: "merchant", Verb: "view", Scope: "tenant", Summary: "view merchants", DataScoped: true, Menu: true},
		{Resource: "merchant", Verb: "update", Scope: "tenant", Summary: "update merchants", DataScoped: true},
		{Resource: "merchant", Verb: "delete", Scope: "tenant", Summary: "delete merchants", DataScoped: true},

		{Resource: "store", Verb: "create", Scope: "merchant", Summary: "create stores", DataScoped: true},
		{Resource: "store", Verb: "view", Scope: "merchant", Summary: "view stores", DataScoped: true, Menu: true},
		{Resource: "store", Verb: "update", Scope: "merchant", Summary: "update stores", DataScoped: true},
		{Resource: "store", Verb: "delete", Scope: "merchant", Summary: "delete stores", DataScoped: true},

		{Resource: "dept", Verb: "create", Scope: "tenant", Summary: "create departments", DataScoped: true},
		{Resource: "dept", Verb: "view", Scope: "tenant", Summary: "view departments", DataScoped: true, Menu: true},
		{Resource: "dept", Verb: "update", Scope: "tenant", Summary: "update departments", DataScoped: true},
		{Resource: "dept", Verb: "delete", Scope: "tenant", Summary: "delete departments", DataScoped: true},

		{Resource: "user", Verb: "create", Scope: "tenant", Summary: "create users", DataScoped: true},
		{Resource: "user", Verb: "view", Scope: "tenant", Summary: "view users", DataScoped: true, Menu: true},
		{Resource: "user", Verb: "update", Scope: "tenant", Summary: "update users", DataScoped: true},
		{Resource: "user", Verb: "delete", Scope: "tenant", Summary: "delete users", DataScoped: true},

		{Resource: "role", Verb: "create", Scope: "tenant", Summary: "create roles"},
		{Resource: "role", Verb: "view", Scope: "tenant", Summary: "view roles", Menu: true},
		{Resource: "role", Verb: "update", Scope: "tenant", Summary: "update roles"},
		{Resource: "role", Verb: "delete", Scope: "tenant", Summary: "delete roles"},
		{Resource: "role", Verb: "grant_permission", Scope: "tenant", Summary: "grant and revoke role permissions"},
		{Resource: "role", Verb: "manage_member", Scope: "tenant", Summary: "add and remove role members"},

		{Resource: "menu", Verb: "create", Scope: "tenant", Summary: "create menus"},
		{Resource: "menu", Verb: "view", Scope: "tenant", Summary: "view menus", Menu: true},
		{Resource: "menu", Verb: "update", Scope: "tenant", Summary: "update menus"},
		{Resource: "menu", Verb: "delete", Scope: "tenant", Summary: "delete menus"},
		{Resource: "menu", Verb: "bind_permission", Scope: "tenant", Summary: "bind menu permissions"},
	}
}
