package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func roleNames(list []RoleInfo) string {
	var names []string
	for _, r := range list {
		names = append(names, string(r.Name))
	}
	return strings.Join(names, ",")
}

func TestBuiltinRoles(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)
	addUser(t, s, admin, "eve", RoleViewer)

	roles, err := s.Roles(ctx)
	if err != nil || roleNames(roles) != "admin,operator,viewer" {
		t.Fatalf("roles = %s, %v", roleNames(roles), err)
	}
	byName := map[Role]RoleInfo{}
	for _, r := range roles {
		byName[r.Name] = r
		if !r.Builtin {
			t.Errorf("%s must be built in", r.Name)
		}
	}
	if len(byName[RoleAdmin].Permissions) != len(Permissions()) {
		t.Errorf("admin has %v, want every permission", byName[RoleAdmin].Permissions)
	}
	if got := strings.Join(byName[RoleViewer].Permissions, ","); got != "layout.edit,servers.read" {
		t.Errorf("viewer = %s", got)
	}
	if got := strings.Join(byName[RoleOperator].Permissions, ","); got != "layout.edit,servers.manage,servers.read" {
		t.Errorf("operator = %s", got)
	}
	if byName[RoleAdmin].Users != 1 || byName[RoleViewer].Users != 1 || byName[RoleOperator].Users != 0 {
		t.Errorf("user counts = %+v", byName)
	}
	for _, name := range []Role{RoleAdmin, RoleOperator, RoleViewer} {
		if _, err := s.UpdateRole(ctx, admin, name, RoleChange{Description: ptr("x")}); !errors.Is(err, ErrBuiltinRole) {
			t.Errorf("changing %s: %v", name, err)
		}
		if err := s.DeleteRole(ctx, admin, name); !errors.Is(err, ErrBuiltinRole) {
			t.Errorf("deleting %s: %v", name, err)
		}
	}
	if _, err := s.CreateRole(ctx, admin, "Admin", "", nil); !isInputError(err) {
		t.Errorf("a custom role named like a built-in one: %v", err)
	}
}

func TestCustomRoleLifecycle(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)

	for name, tc := range map[string]struct {
		name  Role
		perms []string
	}{
		"short name":         {"a", nil},
		"capitals and space": {"Help Desk", nil},
		"leading digit":      {"1abc", nil},
		"unknown permission": {"helpdesk", []string{"servers.read", "world.domination"}},
	} {
		if _, err := s.CreateRole(ctx, admin, tc.name, "", tc.perms); !isInputError(err) {
			t.Errorf("%s: err = %v, want an input error", name, err)
		}
	}
	if _, err := s.CreateRole(ctx, admin, "helpdesk", strings.Repeat("x", 201), nil); !isInputError(err) {
		t.Errorf("long description: %v", err)
	}

	r, err := s.CreateRole(ctx, admin, " HelpDesk ", " Answers users ", []string{PermUsersManage, PermServersRead, PermServersRead})
	if err != nil || r.Name != "helpdesk" || r.Description != "Answers users" || strings.Join(r.Permissions, ",") != "servers.read,users.manage" {
		t.Fatalf("create = %+v, %v", r, err)
	}
	if _, err := s.CreateRole(ctx, admin, "HELPDESK", "", nil); !errors.Is(err, ErrRoleExists) {
		t.Errorf("duplicate: %v", err)
	}

	u := addUser(t, s, admin, "hana", "helpdesk")
	if u.Role != "helpdesk" {
		t.Fatalf("role = %q", u.Role)
	}
	if err := s.DeleteRole(ctx, admin, "helpdesk"); !errors.Is(err, ErrRoleInUse) {
		t.Fatalf("delete while in use: %v", err)
	}

	// Rename: the users follow. Permissions change: effective at once.
	r, err = s.UpdateRole(ctx, admin, "helpdesk", RoleChange{Name: ptr(Role("support")), Permissions: ptr([]string{PermServersRead})})
	if err != nil || r.Name != "support" || r.Users != 1 || strings.Join(r.Permissions, ",") != "servers.read" {
		t.Fatalf("update = %+v, %v", r, err)
	}
	if got, _ := s.userByName(ctx, "hana"); got.Role != "support" {
		t.Errorf("hana's role after the rename = %q", got.Role)
	}
	if _, err := s.UpdateRole(ctx, admin, "helpdesk", RoleChange{}); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("old name: %v", err)
	}
	if _, err := s.UpdateRole(ctx, admin, "support", RoleChange{Name: ptr(RoleViewer)}); !isInputError(err) {
		t.Errorf("renaming to a built-in name: %v", err)
	}
	other, _ := s.CreateRole(ctx, admin, "other", "", nil)
	if _, err := s.UpdateRole(ctx, admin, "support", RoleChange{Name: &other.Name}); !errors.Is(err, ErrRoleExists) {
		t.Errorf("renaming to a taken name: %v", err)
	}

	// Free the role, then delete it.
	if _, err := s.UpdateUser(ctx, admin, "hana", UserChange{Role: ptr(RoleViewer)}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRole(ctx, admin, "support"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteRole(ctx, admin, "support"); !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("delete again: %v", err)
	}
	roles, _ := s.Roles(ctx)
	if roleNames(roles) != "admin,operator,viewer,other" {
		t.Errorf("roles = %s", roleNames(roles))
	}

	entries, _ := s.Audit(ctx, 40)
	for _, ev := range []string{EventRoleCreated, EventRoleUpdated, EventRoleDeleted, EventRoleChanged} {
		found := false
		for _, e := range entries {
			found = found || e.Event == ev
		}
		if !found {
			t.Errorf("no %s event in the audit log", ev)
		}
	}
	if _, err := s.CreateUser(ctx, admin, "ghost", otherPassword, "nonexistent"); !isInputError(err) {
		t.Errorf("a user with an unknown role: %v", err)
	}
}

func TestNobodyGrantsOrTouchesMoreThanTheyHave(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)
	if _, err := s.CreateRole(ctx, admin, "helpdesk", "", []string{PermUsersManage, PermRolesManage, PermServersRead, PermLayoutEdit}); err != nil {
		t.Fatal(err)
	}
	hana := addUser(t, s, admin, "hana", "helpdesk")
	hana.Permissions = nil
	viewer := addUser(t, s, admin, "eve", RoleViewer)

	// hana can manage a viewer...
	if _, err := s.CreateUser(ctx, hana, "tom", otherPassword, RoleViewer); err != nil {
		t.Errorf("creating a viewer: %v", err)
	}
	if _, err := s.UpdateUser(ctx, hana, viewer.Username, UserChange{Disabled: ptr(true)}); err != nil {
		t.Errorf("disabling a viewer: %v", err)
	}
	if _, _, err := s.CreateReset(ctx, hana, viewer.Username); err == nil {
		// disabled accounts cannot be reset, but that is an input error, not a permission one
		t.Error("resetting a disabled account worked")
	} else if errors.Is(err, ErrForbidden) {
		t.Errorf("the permission check refused a viewer: %v", err)
	}

	// ...but cannot reach the admin, or hand out what it does not have.
	if _, err := s.CreateUser(ctx, hana, "boss", otherPassword, RoleAdmin); !errors.Is(err, ErrForbidden) {
		t.Errorf("creating an admin: %v", err)
	}
	if _, err := s.CreateUser(ctx, hana, "olly", otherPassword, RoleOperator); !errors.Is(err, ErrForbidden) {
		t.Errorf("creating an operator (servers.manage is not hana's): %v", err)
	}
	if _, err := s.UpdateUser(ctx, hana, "hana", UserChange{Role: ptr(RoleAdmin)}); !errors.Is(err, ErrForbidden) {
		t.Errorf("promoting oneself: %v", err)
	}
	if _, err := s.UpdateUser(ctx, hana, "admin", UserChange{Disabled: ptr(true)}); !errors.Is(err, ErrForbidden) {
		t.Errorf("disabling the admin: %v", err)
	}
	if err := s.DeleteUser(ctx, hana, "admin"); !errors.Is(err, ErrForbidden) {
		t.Errorf("deleting the admin: %v", err)
	}
	if _, _, err := s.CreateReset(ctx, hana, "admin"); !errors.Is(err, ErrForbidden) {
		t.Errorf("resetting the admin: %v", err)
	}
	if err := s.ResetTwoFactor(ctx, hana, "admin"); !errors.Is(err, ErrForbidden) {
		t.Errorf("resetting the admin's two-factor: %v", err)
	}
	if _, err := s.CreateRole(ctx, hana, "super", "", []string{PermAuditRead}); !errors.Is(err, ErrForbidden) {
		t.Errorf("a role with a permission the creator lacks: %v", err)
	}
	if _, err := s.CreateRole(ctx, hana, "mine", "", []string{PermServersRead}); err != nil {
		t.Errorf("a role within hana's permissions: %v", err)
	}
	if _, err := s.UpdateRole(ctx, hana, "helpdesk", RoleChange{Permissions: ptr([]string{PermUsersManage, PermAuditRead})}); !errors.Is(err, ErrForbidden) {
		t.Errorf("adding audit.read to hana's own role: %v", err)
	}
	if err := s.DeleteRole(ctx, hana, "mine"); err != nil {
		t.Errorf("deleting a role within hana's permissions: %v", err)
	}
	// admin made a role with audit.read; hana lacks it, so she cannot delete or edit it.
	if _, err := s.CreateRole(ctx, admin, "auditor", "", []string{PermAuditRead}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRole(ctx, hana, "auditor"); !errors.Is(err, ErrForbidden) {
		t.Errorf("deleting a stronger role: %v", err)
	}
}

func TestPermissionsAreEnforcedOverHTTP(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)
	for name, perms := range map[Role][]string{
		"helpdesk": {PermUsersManage, PermServersRead},
		"auditor":  {PermAuditRead},
		"nothing":  nil,
	} {
		if _, err := s.CreateRole(ctx, admin, name, "", perms); err != nil {
			t.Fatal(err)
		}
		addUser(t, s, admin, string(name), name)
	}
	addUser(t, s, admin, "olivia", RoleOperator)

	as := func(user string) *client {
		c := newClient(t, s)
		loginAs(t, c, user, otherPassword)
		return c
	}
	for _, tc := range []struct {
		user, method, path string
		want               int
	}{
		{"helpdesk", "GET", "/api/users", 200},
		{"helpdesk", "GET", "/api/audit", 403},
		{"helpdesk", "GET", "/api/roles", 200},
		{"helpdesk", "POST", "/api/roles", 403},
		{"helpdesk", "GET", "/api/settings/security", 200},
		{"auditor", "GET", "/api/audit", 200},
		{"auditor", "GET", "/api/users", 403},
		{"auditor", "GET", "/api/roles", 403},
		{"olivia", "GET", "/api/users", 403},
		{"olivia", "GET", "/api/audit", 403},
		{"olivia", "GET", "/api/roles", 403},
		{"olivia", "GET", "/api/account/sessions", 200},
		{"nothing", "GET", "/api/account/sessions", 200},
		{"nothing", "GET", "/api/users", 403},
	} {
		var body any
		if tc.method == "POST" {
			body = map[string]any{"name": "x1", "permissions": []string{}}
		}
		if rec := as(tc.user).do(tc.method, tc.path, body); rec.Code != tc.want {
			t.Errorf("%s %s as %s = %d, want %d", tc.method, tc.path, tc.user, rec.Code, tc.want)
		}
	}

	// The permissions travel with the session.
	c := as("olivia")
	var me struct {
		User struct {
			Permissions []string `json:"permissions"`
		} `json:"user"`
	}
	_ = json.Unmarshal(c.do("GET", "/api/auth/me", nil).Body.Bytes(), &me)
	if got := strings.Join(me.User.Permissions, ","); got != "layout.edit,servers.manage,servers.read" {
		t.Errorf("operator's permissions in /me = %s", got)
	}

	// Changing a role takes effect at the next request, with no new login.
	h := as("helpdesk")
	if rec := h.do("GET", "/api/users", nil); rec.Code != http.StatusOK {
		t.Fatalf("before: %d", rec.Code)
	}
	if _, err := s.UpdateRole(ctx, admin, "helpdesk", RoleChange{Permissions: ptr([]string{PermServersRead})}); err != nil {
		t.Fatal(err)
	}
	if rec := h.do("GET", "/api/users", nil); rec.Code != http.StatusForbidden {
		t.Errorf("after losing users.manage: %d, want 403", rec.Code)
	}
}

func TestAdminManagesRolesOverHTTP(t *testing.T) {
	s := newService(t)
	setup(t, s)
	c := newClient(t, s)
	loginAs(t, c, "admin", goodPassword)

	rec := c.do("GET", "/api/roles", nil)
	var list struct {
		Roles       []RoleInfo   `json:"roles"`
		Permissions []Permission `json:"permissions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if len(list.Roles) != 3 || len(list.Permissions) != len(Permissions()) {
		t.Errorf("list = %d roles, %d permissions", len(list.Roles), len(list.Permissions))
	}

	if rec := c.do("POST", "/api/roles", map[string]any{"name": "helpdesk", "permissions": []string{"servers.read"}}); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if rec := c.do("POST", "/api/roles", map[string]any{"name": "helpdesk", "permissions": []string{}}); rec.Code != http.StatusConflict {
		t.Errorf("duplicate: %d, want 409", rec.Code)
	}
	if rec := c.do("POST", "/api/roles", map[string]any{"name": "bad", "permissions": []string{"nope"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown permission: %d, want 400", rec.Code)
	}
	if rec := c.do("PATCH", "/api/roles/helpdesk", map[string]any{"description": "Answers users"}); rec.Code != http.StatusOK {
		t.Errorf("update: %d %s", rec.Code, rec.Body)
	}
	if rec := c.do("PATCH", "/api/roles/admin", map[string]any{"description": "x"}); rec.Code != http.StatusConflict && rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
		t.Errorf("built-in role edit: %d", rec.Code)
	}
	if rec := c.do("DELETE", "/api/roles/helpdesk", map[string]string{}); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec := c.do("DELETE", "/api/roles/helpdesk", map[string]string{}); rec.Code != http.StatusNotFound {
		t.Errorf("delete again: %d, want 404", rec.Code)
	}
}

func TestPolicyForAdminsFollowsPermissions(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)
	if _, err := s.CreateRole(ctx, admin, "helpdesk", "", []string{PermUsersManage}); err != nil {
		t.Fatal(err)
	}
	hana := addUser(t, s, admin, "hana", "helpdesk")
	olivia := addUser(t, s, admin, "olivia", RoleOperator)
	if err := s.SetPolicy(ctx, admin, PolicyAdmins); err != nil {
		t.Fatal(err)
	}
	if !s.MustEnroll(ctx, hana) || !s.MustEnroll(ctx, admin) || s.MustEnroll(ctx, olivia) {
		t.Errorf("admins policy: hana %v, admin %v, olivia %v", s.MustEnroll(ctx, hana), s.MustEnroll(ctx, admin), s.MustEnroll(ctx, olivia))
	}
}
