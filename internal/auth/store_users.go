package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// removesAnAdmin is true, in a statement about one row of users, when that
// change cannot leave the hub without an enabled admin: the user is not an
// enabled admin, or another enabled admin exists.
const removesAnAdmin = `(users.role <> 'admin' OR users.disabled = 1 OR EXISTS (
	SELECT 1 FROM users o WHERE o.id <> users.id AND o.role = 'admin' AND o.disabled = 0))`

func (s store) listUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, role, created_at, disabled FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	users := []User{}
	for rows.Next() {
		var (
			u       User
			created string
		)
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &created, &u.Disabled); err != nil {
			return nil, err
		}
		u.CreatedAt, _ = time.Parse(timeLayout, created)
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s store) createUser(ctx context.Context, username, hash string, role Role, now time.Time) (User, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role, created_at) VALUES (?, ?, ?, ?)`,
		username, hash, string(role), now.UTC().Format(timeLayout))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, ErrUserExists
		}
		return User{}, fmt.Errorf("create user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return User{ID: id, Username: username, Role: role, CreatedAt: now.UTC()}, nil
}

// changeUser runs an UPDATE or DELETE on one user and tells apart an unknown
// user (ErrUserNotFound) from a refused change (ErrLastAdmin).
func (s store) changeUser(ctx context.Context, id int64, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	var one int
	err = s.db.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return err
	}
	return ErrLastAdmin
}

func (s store) setRole(ctx context.Context, id int64, role Role) error {
	if role == RoleAdmin {
		return s.changeUser(ctx, id, `UPDATE users SET role = 'admin' WHERE id = ?`, id)
	}
	return s.changeUser(ctx, id, `UPDATE users SET role = ? WHERE id = ? AND `+removesAnAdmin, string(role), id)
}

func (s store) setDisabled(ctx context.Context, id int64, disabled bool) error {
	if !disabled {
		return s.changeUser(ctx, id, `UPDATE users SET disabled = 0 WHERE id = ?`, id)
	}
	return s.changeUser(ctx, id, `UPDATE users SET disabled = 1 WHERE id = ? AND `+removesAnAdmin, id)
}

func (s store) deleteUser(ctx context.Context, id int64) error {
	return s.changeUser(ctx, id, `DELETE FROM users WHERE id = ? AND `+removesAnAdmin, id)
}

func (s store) setPassword(ctx context.Context, id int64, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	return err
}

func (s store) passwordHash(ctx context.Context, id int64) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, id).Scan(&hash)
	return hash, err
}

// deleteSessionsOf ends every session of the user except the one whose token
// hash is keep (which may be empty).
func (s store) deleteSessionsOf(ctx context.Context, userID int64, keep string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, userID, keep)
	return err
}

// sessionIDLen is how much of the token hash identifies a session in the API.
// It reveals nothing that could be used as a session token.
const sessionIDLen = 16

// SessionInfo is a login session as shown to its owner.
type SessionInfo struct {
	ID         string    `json:"id"`
	RemoteAddr string    `json:"remote_addr"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	// Current is true for the session that made the request.
	Current bool `json:"current"`
}

func (s store) listSessions(ctx context.Context, userID int64, now time.Time, currentHash string) ([]SessionInfo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT token_hash, remote_addr, user_agent, created_at, last_used_at, expires_at
		 FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY last_used_at DESC`,
		userID, now.UTC().Format(timeLayout))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	list := []SessionInfo{}
	for rows.Next() {
		var (
			hash, created, used, expires string
			si                           SessionInfo
		)
		if err := rows.Scan(&hash, &si.RemoteAddr, &si.UserAgent, &created, &used, &expires); err != nil {
			return nil, err
		}
		si.ID = hash[:sessionIDLen]
		si.Current = hash == currentHash
		si.CreatedAt, _ = time.Parse(timeLayout, created)
		si.LastUsedAt, _ = time.Parse(timeLayout, used)
		si.ExpiresAt, _ = time.Parse(timeLayout, expires)
		list = append(list, si)
	}
	return list, rows.Err()
}

// deleteSessionByID ends one session of the user, found by its API id.
func (s store) deleteSessionByID(ctx context.Context, userID int64, id string) error {
	if len(id) != sessionIDLen {
		return ErrSessionNotFound
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND substr(token_hash, 1, ?) = ?`, userID, sessionIDLen, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// touchSession records that the session was used, at most once a minute.
func (s store) touchSession(ctx context.Context, tokenHash string, now time.Time) {
	_, _ = s.db.ExecContext(ctx,
		`UPDATE sessions SET last_used_at = ? WHERE token_hash = ? AND last_used_at < ?`,
		now.UTC().Format(timeLayout), tokenHash, now.Add(-time.Minute).UTC().Format(timeLayout))
}

// AuditEntry is one recorded account event.
type AuditEntry struct {
	ID     int64     `json:"id"`
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Event  string    `json:"event"`
	Target string    `json:"target"`
	Remote string    `json:"remote"`
	Detail string    `json:"detail"`
}

// maxAuditRows is how many events are kept; older ones are dropped.
const maxAuditRows = 10000

func (s store) addAudit(ctx context.Context, e AuditEntry) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (at, actor, event, target, remote, detail) VALUES (?, ?, ?, ?, ?, ?)`,
		e.At.UTC().Format(timeLayout), e.Actor, e.Event, e.Target, e.Remote, e.Detail); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE id <= (SELECT MAX(id) FROM audit_log) - ?`, maxAuditRows)
	return err
}

func (s store) listAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, actor, event, target, remote, detail FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	list := []AuditEntry{}
	for rows.Next() {
		var (
			e  AuditEntry
			at string
		)
		if err := rows.Scan(&e.ID, &at, &e.Actor, &e.Event, &e.Target, &e.Remote, &e.Detail); err != nil {
			return nil, err
		}
		e.At, _ = time.Parse(timeLayout, at)
		list = append(list, e)
	}
	return list, rows.Err()
}
