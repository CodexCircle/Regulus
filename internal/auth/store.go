package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Role is what a user may do. Admins can do everything; viewers can only read.
type Role string

// The roles known to the hub.
const (
	RoleAdmin  Role = "admin"
	RoleViewer Role = "viewer"
)

// User is a local account.
type User struct {
	ID        int64     `json:"-"`
	Username  string    `json:"username"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// Errors returned by the store and the service.
var (
	ErrSetupDone          = errors.New("setup was already completed")
	ErrBadSetupToken      = errors.New("invalid setup token")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrRateLimited        = errors.New("too many failed attempts, try again later")
	ErrUnauthenticated    = errors.New("authentication required")
)

// timeLayout is a fixed-width UTC format, so timestamps sort correctly as text
// (time.RFC3339Nano drops trailing zeros, which breaks the order).
const timeLayout = "2006-01-02T15:04:05.000000000Z"

type store struct {
	db *sql.DB
}

func (s store) countUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// createFirstAdmin inserts the admin only if there is no user yet, in a single
// statement, so two concurrent setups cannot both succeed.
func (s store) createFirstAdmin(ctx context.Context, username, hash string, now time.Time) (User, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role, created_at)
		 SELECT ?, ?, 'admin', ? WHERE NOT EXISTS (SELECT 1 FROM users)`,
		username, hash, now.UTC().Format(timeLayout))
	if err != nil {
		return User{}, fmt.Errorf("create admin: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return User{}, ErrSetupDone
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return User{ID: id, Username: username, Role: RoleAdmin, CreatedAt: now.UTC()}, nil
}

// userByName returns the user and the stored password hash. The error is
// sql.ErrNoRows for an unknown user.
func (s store) userByName(ctx context.Context, username string) (User, string, error) {
	var (
		u       User
		hash    string
		created string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, role, created_at, password_hash FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.Role, &created, &hash)
	if err != nil {
		return User{}, "", err
	}
	u.CreatedAt, _ = time.Parse(timeLayout, created)
	return u, hash, nil
}

func (s store) createSession(ctx context.Context, tokenHash string, userID int64, now, expires time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		tokenHash, userID, now.UTC().Format(timeLayout), expires.UTC().Format(timeLayout))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// sessionUser returns the user of a session that has not expired.
func (s store) sessionUser(ctx context.Context, tokenHash string, now time.Time) (User, error) {
	var (
		u                User
		created, expires string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT u.id, u.username, u.role, u.created_at, s.expires_at
		 FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ?`, tokenHash).
		Scan(&u.ID, &u.Username, &u.Role, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUnauthenticated
	}
	if err != nil {
		return User{}, err
	}
	exp, err := time.Parse(timeLayout, expires)
	if err != nil || !now.Before(exp) {
		return User{}, ErrUnauthenticated
	}
	u.CreatedAt, _ = time.Parse(timeLayout, created)
	return u, nil
}

func (s store) deleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s store) deleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.UTC().Format(timeLayout))
	return err
}
