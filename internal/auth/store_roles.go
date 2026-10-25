package auth

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"
)

// roleRow is a custom role as stored.
type roleRow struct {
	name        Role
	description string
	createdAt   time.Time
	permissions []string
}

// customRoles returns all the stored roles, ordered by name.
func (s store) customRoles(ctx context.Context) ([]roleRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, description, created_at FROM roles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var list []roleRow
	index := map[string]int{}
	for rows.Next() {
		var (
			r       roleRow
			created string
		)
		if err := rows.Scan(&r.name, &r.description, &created); err != nil {
			return nil, err
		}
		r.createdAt, _ = time.Parse(timeLayout, created)
		index[strings.ToLower(string(r.name))] = len(list)
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()

	perms, err := s.db.QueryContext(ctx, `SELECT role, permission FROM role_permissions ORDER BY permission`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = perms.Close() }()
	for perms.Next() {
		var role, perm string
		if err := perms.Scan(&role, &perm); err != nil {
			return nil, err
		}
		if i, ok := index[strings.ToLower(role)]; ok {
			list[i].permissions = append(list[i].permissions, perm)
		}
	}
	return list, perms.Err()
}

// customRole returns one stored role, or sql.ErrNoRows.
func (s store) customRole(ctx context.Context, name Role) (roleRow, error) {
	var (
		r       roleRow
		created string
	)
	err := s.db.QueryRowContext(ctx, `SELECT name, description, created_at FROM roles WHERE name = ?`, string(name)).
		Scan(&r.name, &r.description, &created)
	if err != nil {
		return roleRow{}, err
	}
	r.createdAt, _ = time.Parse(timeLayout, created)
	rows, err := s.db.QueryContext(ctx, `SELECT permission FROM role_permissions WHERE role = ? ORDER BY permission`, string(name))
	if err != nil {
		return roleRow{}, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return roleRow{}, err
		}
		r.permissions = append(r.permissions, p)
	}
	return r, rows.Err()
}

func (s store) createRole(ctx context.Context, r roleRow, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO roles (name, description, created_at) VALUES (?, ?, ?)`,
		string(r.name), r.description, now.UTC().Format(timeLayout)); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrRoleExists
		}
		return err
	}
	if err := putPermissions(ctx, tx, r.name, r.permissions); err != nil {
		return err
	}
	return tx.Commit()
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func putPermissions(ctx context.Context, tx execer, role Role, perms []string) error {
	for _, p := range perms {
		if _, err := tx.ExecContext(ctx, `INSERT INTO role_permissions (role, permission) VALUES (?, ?)`, string(role), p); err != nil {
			return err
		}
	}
	return nil
}

// updateRole changes a stored role: its name (the users that have it follow),
// description and permissions.
func (s store) updateRole(ctx context.Context, old Role, next roleRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE roles SET name = ?, description = ? WHERE name = ?`,
		string(next.name), next.description, string(old)); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrRoleExists
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET role = ? WHERE role = ?`, string(next.name), string(old)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM role_permissions WHERE role = ?`, string(next.name)); err != nil {
		return err
	}
	if err := putPermissions(ctx, tx, next.name, next.permissions); err != nil {
		return err
	}
	return tx.Commit()
}

// deleteRole removes a stored role that no user has. It reports the number of
// users that still have it instead when there are some.
func (s store) deleteRole(ctx context.Context, name Role) (inUse int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = ?`, string(name)).Scan(&inUse); err != nil {
		return 0, err
	}
	if inUse > 0 {
		return inUse, nil
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM roles WHERE name = ?`, string(name))
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrRoleNotFound
	}
	return 0, tx.Commit()
}

// roleUserCounts returns how many users have each role.
func (s store) roleUserCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT lower(role), COUNT(*) FROM users GROUP BY lower(role)`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	counts := map[string]int{}
	for rows.Next() {
		var (
			role string
			n    int
		)
		if err := rows.Scan(&role, &n); err != nil {
			return nil, err
		}
		counts[role] = n
	}
	return counts, rows.Err()
}

func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}
