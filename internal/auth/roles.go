package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Errors about roles.
var (
	ErrRoleNotFound = errors.New("role not found")
	ErrRoleExists   = errors.New("a role with this name already exists")
	ErrBuiltinRole  = errors.New("the built-in roles cannot be changed or deleted")
	ErrRoleInUse    = errors.New("users still have this role")
)

// Audit events about roles. Giving a user another role is EventRoleChanged.
const (
	EventRoleCreated = "role_created"
	EventRoleUpdated = "role_updated"
	EventRoleDeleted = "role_deleted"
)

var roleNameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)

const maxRoleDescription = 200

// RoleInfo is a role as shown to the users who may see roles.
type RoleInfo struct {
	Name        Role     `json:"name"`
	Description string   `json:"description"`
	Builtin     bool     `json:"builtin"`
	Permissions []string `json:"permissions"`
	// Users is how many accounts have the role.
	Users int `json:"users"`
}

func validRoleName(name Role) error {
	if !roleNameRE.MatchString(string(name)) {
		return &inputError{"role name must be 2 to 32 characters: lowercase letters, digits, dash or underscore, starting with a letter"}
	}
	if _, ok := builtin(name); ok {
		return &inputError{fmt.Sprintf("%q is the name of a built-in role", name)}
	}
	return nil
}

// permsOfRole returns what a role allows, and whether the role exists. The
// answer comes from a cache that every change to a role clears.
func (s *Service) permsOfRole(ctx context.Context, name Role) (permSet, bool, error) {
	key := strings.ToLower(string(name))
	s.mu.Lock()
	if c, ok := s.rolePerms[key]; ok {
		s.mu.Unlock()
		return c.perms, c.exists, nil
	}
	s.mu.Unlock()

	var (
		set    permSet
		exists bool
	)
	if b, ok := builtin(name); ok {
		exists = true
		if b.all {
			set = permSet{all: true}
		} else {
			set = newPermSet(b.perms)
		}
	} else {
		r, err := s.store.customRole(ctx, name)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return permSet{}, false, err
		default:
			exists = true
			set = newPermSet(r.permissions)
		}
	}
	s.mu.Lock()
	s.rolePerms[key] = cachedPerms{perms: set, exists: exists}
	s.mu.Unlock()
	return set, exists, nil
}

type cachedPerms struct {
	perms  permSet
	exists bool
}

func (s *Service) forgetRoles() {
	s.mu.Lock()
	s.rolePerms = map[string]cachedPerms{}
	s.mu.Unlock()
}

// permsOfUser returns what the user's role allows; a role that no longer
// exists allows nothing.
func (s *Service) permsOfUser(ctx context.Context, u User) permSet {
	set, _, err := s.permsOfRole(ctx, u.Role)
	if err != nil {
		s.log.Error("cannot read the permissions of a role", "role", u.Role, "err", err)
		return permSet{}
	}
	return set
}

// mayManage checks that by may act on target: by must hold every permission
// target has, so nobody can touch (disable, delete, reset) an account that is
// more privileged than their own.
func (s *Service) mayManage(ctx context.Context, by, target User) error {
	if !s.permsOfUser(ctx, by).covers(s.permsOfUser(ctx, target)) {
		return fmt.Errorf("%w: %s has permissions you do not have", ErrForbidden, target.Username)
	}
	return nil
}

// mayGrant checks that by holds every permission of the role, so nobody can
// hand out more than they have.
func (s *Service) mayGrant(ctx context.Context, by User, role Role) error {
	set, exists, err := s.permsOfRole(ctx, role)
	if err != nil {
		return err
	}
	if !exists {
		return &inputError{fmt.Sprintf("there is no role %q", role)}
	}
	if !s.permsOfUser(ctx, by).covers(set) {
		return fmt.Errorf("%w: the role %q has permissions you do not have", ErrForbidden, role)
	}
	return nil
}

// Roles lists the built-in roles, then the custom ones.
func (s *Service) Roles(ctx context.Context) ([]RoleInfo, error) {
	counts, err := s.store.roleUserCounts(ctx)
	if err != nil {
		return nil, err
	}
	var out []RoleInfo
	for _, b := range builtinRoles {
		set := newPermSet(b.perms)
		set.all = b.all
		out = append(out, RoleInfo{Name: b.name, Description: b.description, Builtin: true, Permissions: set.list(), Users: counts[string(b.name)]})
	}
	custom, err := s.store.customRoles(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range custom {
		out = append(out, RoleInfo{
			Name: r.name, Description: r.description,
			Permissions: newPermSet(r.permissions).list(),
			Users:       counts[strings.ToLower(string(r.name))],
		})
	}
	return out, nil
}

// cleanPermissions checks the names and removes repeats.
func cleanPermissions(perms []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, p := range perms {
		if !validPermission(p) {
			return nil, &inputError{fmt.Sprintf("unknown permission %q", p)}
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return sortedCopy(out), nil
}

func cleanDescription(d string) (string, error) {
	d = strings.TrimSpace(d)
	if len([]rune(d)) > maxRoleDescription {
		return "", &inputError{fmt.Sprintf("the description must be at most %d characters", maxRoleDescription)}
	}
	return d, nil
}

// CreateRole adds a custom role. The caller must hold every permission it
// gives.
func (s *Service) CreateRole(ctx context.Context, by User, name Role, description string, perms []string) (RoleInfo, error) {
	name = Role(strings.ToLower(strings.TrimSpace(string(name))))
	if err := validRoleName(name); err != nil {
		return RoleInfo{}, err
	}
	description, err := cleanDescription(description)
	if err != nil {
		return RoleInfo{}, err
	}
	perms, err = cleanPermissions(perms)
	if err != nil {
		return RoleInfo{}, err
	}
	if !s.permsOfUser(ctx, by).covers(newPermSet(perms)) {
		return RoleInfo{}, fmt.Errorf("%w: you cannot give permissions you do not have", ErrForbidden)
	}
	if err := s.store.createRole(ctx, roleRow{name: name, description: description, permissions: perms}, s.now()); err != nil {
		return RoleInfo{}, err
	}
	s.forgetRoles()
	s.audit(ctx, by.Username, EventRoleCreated, string(name), strings.Join(perms, ", "))
	return RoleInfo{Name: name, Description: description, Permissions: perms}, nil
}

// RoleChange lists the changes to a custom role; nil fields stay as they are.
type RoleChange struct {
	Name        *Role
	Description *string
	Permissions *[]string
}

// UpdateRole changes a custom role. The users that have it follow a rename and
// get the new permissions at their next request. The caller must hold every
// permission the role had and will have.
func (s *Service) UpdateRole(ctx context.Context, by User, name Role, ch RoleChange) (RoleInfo, error) {
	if _, ok := builtin(name); ok {
		return RoleInfo{}, ErrBuiltinRole
	}
	cur, err := s.store.customRole(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return RoleInfo{}, ErrRoleNotFound
	}
	if err != nil {
		return RoleInfo{}, err
	}
	next := roleRow{name: cur.name, description: cur.description, permissions: newPermSet(cur.permissions).list()}
	if ch.Name != nil {
		n := Role(strings.ToLower(strings.TrimSpace(string(*ch.Name))))
		if !strings.EqualFold(string(n), string(cur.name)) {
			if err := validRoleName(n); err != nil {
				return RoleInfo{}, err
			}
		}
		next.name = n
	}
	if ch.Description != nil {
		if next.description, err = cleanDescription(*ch.Description); err != nil {
			return RoleInfo{}, err
		}
	}
	if ch.Permissions != nil {
		if next.permissions, err = cleanPermissions(*ch.Permissions); err != nil {
			return RoleInfo{}, err
		}
	}
	mine := s.permsOfUser(ctx, by)
	if !mine.covers(newPermSet(cur.permissions)) || !mine.covers(newPermSet(next.permissions)) {
		return RoleInfo{}, fmt.Errorf("%w: the role involves permissions you do not have", ErrForbidden)
	}
	if err := s.store.updateRole(ctx, cur.name, next); err != nil {
		return RoleInfo{}, err
	}
	s.forgetRoles()
	detail := strings.Join(next.permissions, ", ")
	if next.name != cur.name {
		detail = fmt.Sprintf("renamed from %s; %s", cur.name, detail)
	}
	s.audit(ctx, by.Username, EventRoleUpdated, string(next.name), detail)
	counts, _ := s.store.roleUserCounts(ctx)
	return RoleInfo{Name: next.name, Description: next.description, Permissions: next.permissions, Users: counts[strings.ToLower(string(next.name))]}, nil
}

// DeleteRole removes a custom role that no user has. The caller must hold
// every permission the role has.
func (s *Service) DeleteRole(ctx context.Context, by User, name Role) error {
	if _, ok := builtin(name); ok {
		return ErrBuiltinRole
	}
	cur, err := s.store.customRole(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRoleNotFound
	}
	if err != nil {
		return err
	}
	if !s.permsOfUser(ctx, by).covers(newPermSet(cur.permissions)) {
		return fmt.Errorf("%w: the role has permissions you do not have", ErrForbidden)
	}
	inUse, err := s.store.deleteRole(ctx, cur.name)
	if err != nil {
		return err
	}
	if inUse > 0 {
		return fmt.Errorf("%w (%d): give them another role first", ErrRoleInUse, inUse)
	}
	s.forgetRoles()
	s.audit(ctx, by.Username, EventRoleDeleted, string(cur.name), "")
	return nil
}
