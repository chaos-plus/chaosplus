// Package authz holds the application's authorization catalog.
//
// The catalog is intentionally code-first: route guards, the management UI,
// menu binding, and the relational authorizer all read the same declarations
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
	Resource         string   `json:"resource" doc:"resource name, e.g. store"`
	Verb             string   `json:"verb" doc:"action verb, e.g. view"`
	Code             string   `json:"code" doc:"stable permission code, resource_verb"`
	Scope            string   `json:"scope" doc:"highest scope that owns the grant"`
	Summary          string   `json:"summary" doc:"human-readable permission summary"`
	AllowedRelations []string `json:"allowed_relations" doc:"relationship grants that satisfy this action"`
	DataScoped       bool     `json:"data_scoped" doc:"true when object/data filtering also applies"`
	Menu             bool     `json:"menu" doc:"true when this action may drive menu visibility"`
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

// PermissionCode joins resource and verb into the stable tenant permission
// name used by endpoint checks and role grants.
func PermissionCode(resource, verb string) string {
	return resource + "_" + verb
}

func validRelationshipRelation(value string) bool {
	return value == "owner" || value == "editor" || value == "viewer"
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
	seenRelations := map[string]bool{}
	for _, relation := range a.AllowedRelations {
		if !validRelationshipRelation(relation) || seenRelations[relation] {
			return fmt.Errorf("invalid or duplicate authz relation %q", relation)
		}
		seenRelations[relation] = true
	}
	sort.Strings(a.AllowedRelations)
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

		{Resource: "entity", Verb: "create", Scope: "tenant", Summary: "create tenant entities", DataScoped: true},
		{Resource: "entity", Verb: "view", Scope: "tenant", Summary: "view tenant entities", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true},
		{Resource: "entity", Verb: "update", Scope: "tenant", Summary: "update tenant entities", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
		{Resource: "entity", Verb: "delete", Scope: "tenant", Summary: "delete tenant entities", AllowedRelations: []string{"owner"}, DataScoped: true},
		{Resource: "entity", Verb: "manage_binding", Scope: "tenant", Summary: "manage entity role bindings", AllowedRelations: []string{"owner"}, DataScoped: true},

		{Resource: "merchant", Verb: "create", Scope: "tenant", Summary: "create merchants", DataScoped: true},
		{Resource: "merchant", Verb: "view", Scope: "tenant", Summary: "view merchants", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true},
		{Resource: "merchant", Verb: "update", Scope: "tenant", Summary: "update merchants", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
		{Resource: "merchant", Verb: "delete", Scope: "tenant", Summary: "delete merchants", AllowedRelations: []string{"owner"}, DataScoped: true},

		{Resource: "store", Verb: "create", Scope: "merchant", Summary: "create stores", DataScoped: true},
		{Resource: "store", Verb: "view", Scope: "merchant", Summary: "view stores", AllowedRelations: []string{"owner", "editor", "viewer"}, DataScoped: true, Menu: true},
		{Resource: "store", Verb: "update", Scope: "merchant", Summary: "update stores", AllowedRelations: []string{"owner", "editor"}, DataScoped: true},
		{Resource: "store", Verb: "delete", Scope: "merchant", Summary: "delete stores", AllowedRelations: []string{"owner"}, DataScoped: true},

		{Resource: "dept", Verb: "create", Scope: "tenant", Summary: "create departments", DataScoped: true},
		{Resource: "dept", Verb: "view", Scope: "tenant", Summary: "view departments", DataScoped: true, Menu: true},
		{Resource: "dept", Verb: "update", Scope: "tenant", Summary: "update departments", DataScoped: true},
		{Resource: "dept", Verb: "delete", Scope: "tenant", Summary: "delete departments", DataScoped: true},

		{Resource: "position", Verb: "create", Scope: "tenant", Summary: "create positions", DataScoped: true},
		{Resource: "position", Verb: "view", Scope: "tenant", Summary: "view positions", DataScoped: true, Menu: true},
		{Resource: "position", Verb: "update", Scope: "tenant", Summary: "update positions", DataScoped: true},
		{Resource: "position", Verb: "delete", Scope: "tenant", Summary: "delete positions", DataScoped: true},
		{Resource: "position", Verb: "manage_member", Scope: "tenant", Summary: "assign and remove position members", DataScoped: true},

		{Resource: "group", Verb: "create", Scope: "tenant", Summary: "create groups", DataScoped: true},
		{Resource: "group", Verb: "view", Scope: "tenant", Summary: "view groups", DataScoped: true, Menu: true},
		{Resource: "group", Verb: "update", Scope: "tenant", Summary: "update groups", DataScoped: true},
		{Resource: "group", Verb: "delete", Scope: "tenant", Summary: "delete groups", DataScoped: true},
		{Resource: "group", Verb: "manage_member", Scope: "tenant", Summary: "assign and remove group members", DataScoped: true},

		{Resource: "user", Verb: "create", Scope: "tenant", Summary: "create users", DataScoped: true},
		{Resource: "user", Verb: "view", Scope: "tenant", Summary: "view users", DataScoped: true, Menu: true},
		{Resource: "user", Verb: "update", Scope: "tenant", Summary: "update users", DataScoped: true},
		{Resource: "user", Verb: "delete", Scope: "tenant", Summary: "delete users", DataScoped: true},

		{Resource: "service_account", Verb: "create", Scope: "tenant", Summary: "create service accounts"},
		{Resource: "service_account", Verb: "view", Scope: "tenant", Summary: "view service accounts", Menu: true},
		{Resource: "service_account", Verb: "update", Scope: "tenant", Summary: "update service accounts"},
		{Resource: "service_account", Verb: "delete", Scope: "tenant", Summary: "delete service accounts"},
		{Resource: "service_account", Verb: "manage_credential", Scope: "tenant", Summary: "create and revoke service account credentials"},

		{Resource: "invitation", Verb: "create", Scope: "tenant", Summary: "create tenant invitations"},
		{Resource: "invitation", Verb: "view", Scope: "tenant", Summary: "view tenant invitations", Menu: true},
		{Resource: "invitation", Verb: "revoke", Scope: "tenant", Summary: "revoke tenant invitations"},
		{Resource: "invitation", Verb: "resend", Scope: "tenant", Summary: "rotate tenant invitation credentials"},

		{Resource: "role", Verb: "create", Scope: "tenant", Summary: "create roles"},
		{Resource: "role", Verb: "view", Scope: "tenant", Summary: "view roles", Menu: true},
		{Resource: "role", Verb: "update", Scope: "tenant", Summary: "update roles"},
		{Resource: "role", Verb: "delete", Scope: "tenant", Summary: "delete roles"},
		{Resource: "role", Verb: "grant_permission", Scope: "tenant", Summary: "grant and revoke role permissions"},
		{Resource: "role", Verb: "manage_member", Scope: "tenant", Summary: "add and remove role members"},
		{Resource: "role", Verb: "manage_assignee", Scope: "tenant", Summary: "assign groups and positions to roles"},

		{Resource: "menu", Verb: "create", Scope: "tenant", Summary: "create menus"},
		{Resource: "menu", Verb: "view", Scope: "tenant", Summary: "view menus", Menu: true},
		{Resource: "menu", Verb: "update", Scope: "tenant", Summary: "update menus"},
		{Resource: "menu", Verb: "delete", Scope: "tenant", Summary: "delete menus"},
		{Resource: "menu", Verb: "bind_permission", Scope: "tenant", Summary: "bind menu permissions"},

		{Resource: "oauth_client", Verb: "create", Scope: "tenant", Summary: "create OAuth clients"},
		{Resource: "oauth_client", Verb: "view", Scope: "tenant", Summary: "view OAuth clients", Menu: true},
		{Resource: "oauth_client", Verb: "update", Scope: "tenant", Summary: "rotate OAuth client credentials"},
		{Resource: "oauth_client", Verb: "delete", Scope: "tenant", Summary: "delete OAuth clients"},
		{Resource: "identity_provider", Verb: "create", Scope: "tenant", Summary: "create identity providers"},
		{Resource: "identity_provider", Verb: "view", Scope: "tenant", Summary: "view identity providers", Menu: true},
		{Resource: "identity_provider", Verb: "update", Scope: "tenant", Summary: "update identity providers"},
		{Resource: "identity_provider", Verb: "delete", Scope: "tenant", Summary: "delete identity providers"},

		{Resource: "access_request", Verb: "view", Scope: "tenant", Summary: "view tenant access requests", Menu: true},
		{Resource: "access_request", Verb: "approve", Scope: "tenant", Summary: "approve, reject, and revoke temporary access"},
		{Resource: "access_review", Verb: "view", Scope: "tenant", Summary: "view tenant access review campaigns", Menu: true},
		{Resource: "access_review", Verb: "create", Scope: "tenant", Summary: "create tenant access review campaigns"},
		{Resource: "access_review", Verb: "decide", Scope: "tenant", Summary: "keep or revoke reviewed tenant role grants"},
		{Resource: "access_review", Verb: "manage", Scope: "tenant", Summary: "complete and cancel tenant access reviews"},

		{Resource: "audit_event", Verb: "view", Scope: "tenant", Summary: "view and verify tenant audit events", Menu: true},
		{Resource: "audit_event", Verb: "export", Scope: "tenant", Summary: "export tenant audit events"},
	}
}
