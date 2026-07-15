package authz

import (
	"strings"
)

// GenerateSchema renders the first-phase SpiceDB schema. Tenant-level grant
// relations are generated from the catalog: assigning a role to a code writes
// tenant:<tid>#<code>_role@role:<rid>#member.
func GenerateSchema(actions []Action) string {
	var b strings.Builder
	b.WriteString(`definition user {}

definition role {
  relation member: user
}

definition platform {
  relation admin: user
  permission administer = admin
}

definition tenant {
  relation platform: platform
  relation admin: user | role#member
  permission administer = admin + platform->administer
`)
	for _, a := range MustRegistry(actions...).All() {
		b.WriteString("  relation ")
		b.WriteString(a.Code)
		b.WriteString("_role: role#member\n")
		b.WriteString("  permission ")
		b.WriteString(a.Code)
		b.WriteString(" = ")
		b.WriteString(a.Code)
		b.WriteString("_role + administer\n")
	}
	b.WriteString(`}

definition merchant {
  relation tenant: tenant
  relation admin: user | role#member
  permission administer = admin + tenant->administer
  permission view = administer + tenant->merchant_view
  permission update = administer + tenant->merchant_update
  permission delete = administer + tenant->merchant_delete
}

definition store {
  relation tenant: tenant
  relation merchant: merchant
  relation admin: user | role#member
  permission administer = admin + merchant->administer + tenant->administer
  permission view = administer + tenant->store_view
  permission update = administer + tenant->store_update
  permission delete = administer + tenant->store_delete
}

definition dept {
  relation tenant: tenant
  relation parent: dept
  relation manager: user | role#member
  permission manage = manager + parent->manage + tenant->administer
  permission view = manage + tenant->dept_view
  permission update = manage + tenant->dept_update
  permission delete = manage + tenant->dept_delete
}

definition menu {
  relation tenant: tenant
  relation parent: menu
  relation viewer: user | role#member
  permission view = viewer + parent->view + tenant->menu_view
  permission update = tenant->menu_update
  permission delete = tenant->menu_delete
}
`)
	return b.String()
}

// DefaultSchema renders the schema for DefaultActions.
func DefaultSchema() string {
	return GenerateSchema(DefaultActions())
}
