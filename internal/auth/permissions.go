package auth

import (
	"sort"
	"strings"
)

// Permission is something a role can allow. The API checks permissions, never
// role names.
type Permission struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// The permissions known to the hub. Features add theirs here (restarting
// services, package updates, logs, plugins) together with the endpoints that
// check them.
const (
	PermServersRead   = "servers.read"
	PermServersManage = "servers.manage"
	PermLayoutEdit    = "layout.edit"
	PermUsersManage   = "users.manage"
	PermRolesManage   = "roles.manage"
	PermAuditRead     = "audit.read"
)

var catalogue = []Permission{
	{PermServersRead, "See the servers, their state and their metrics"},
	{PermServersManage, "Add, rename and revoke servers"},
	{PermLayoutEdit, "Customize and save their own dashboard layout"},
	{PermUsersManage, "Manage accounts: create, disable, delete, reset passwords and two-factor, and the two-factor policy"},
	{PermRolesManage, "Create, change and delete roles"},
	{PermAuditRead, "Read the audit log"},
}

// Permissions returns every permission the hub knows, in a stable order.
func Permissions() []Permission {
	return append([]Permission(nil), catalogue...)
}

func validPermission(id string) bool {
	for _, p := range catalogue {
		if p.ID == id {
			return true
		}
	}
	return false
}

// The built-in roles. They live in the code, not in the database, so a new
// version can give them the permissions of its new features, and nobody can
// edit them by mistake; custom roles cover the variations.
const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

type builtinRole struct {
	name        Role
	description string
	// all means every permission, including those added later.
	all   bool
	perms []string
}

var builtinRoles = []builtinRole{
	{name: RoleAdmin, description: "Everything, including accounts and roles", all: true},
	{name: RoleOperator, description: "Works with the servers; does not manage accounts", perms: []string{PermServersRead, PermServersManage, PermLayoutEdit}},
	{name: RoleViewer, description: "Read only", perms: []string{PermServersRead, PermLayoutEdit}},
}

func builtin(name Role) (builtinRole, bool) {
	for _, b := range builtinRoles {
		if strings.EqualFold(string(b.name), string(name)) {
			return b, true
		}
	}
	return builtinRole{}, false
}

// permSet is the permissions of a role.
type permSet struct {
	all bool
	ids map[string]bool
}

func newPermSet(ids []string) permSet {
	p := permSet{ids: map[string]bool{}}
	for _, id := range ids {
		if validPermission(id) {
			p.ids[id] = true
		}
	}
	return p
}

func (p permSet) has(id string) bool { return p.all || p.ids[id] }

// covers reports whether p allows everything o allows.
func (p permSet) covers(o permSet) bool {
	if p.all {
		return true
	}
	if o.all {
		return false
	}
	for id := range o.ids {
		if !p.ids[id] {
			return false
		}
	}
	return true
}

// list returns the permission ids, sorted; a role with everything lists every
// permission there is.
func (p permSet) list() []string {
	var out []string
	if p.all {
		for _, c := range catalogue {
			out = append(out, c.ID)
		}
	} else {
		for id := range p.ids {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}

// Can reports whether the user has the permission. Users from Authenticate
// carry their permissions.
func (u User) Can(permission string) bool {
	for _, p := range u.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}
