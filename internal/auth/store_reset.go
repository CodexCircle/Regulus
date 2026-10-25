package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// putReset stores the reset of a user, replacing any earlier one, and drops
// the expired ones.
func (s store) putReset(ctx context.Context, userID int64, tokenHash string, now, expires time.Time, by string) error {
	at := now.UTC().Format(timeLayout)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM password_resets WHERE expires_at <= ?`, at); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO password_resets (token_hash, user_id, created_at, expires_at, created_by) VALUES (?, ?, ?, ?, ?)`,
		tokenHash, userID, at, expires.UTC().Format(timeLayout), by)
	return err
}

// resetPending reports whether the token is a reset that has not expired.
func (s store) resetPending(ctx context.Context, tokenHash string, now time.Time) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM password_resets WHERE token_hash = ? AND expires_at > ?`,
		tokenHash, now.UTC().Format(timeLayout)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// takeReset uses up a reset in one statement, so two requests cannot both
// succeed with the same token. It returns the user it was made for, or
// ErrInvalidReset.
func (s store) takeReset(ctx context.Context, tokenHash string, now time.Time) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`DELETE FROM password_resets WHERE token_hash = ? AND expires_at > ? RETURNING user_id`,
		tokenHash, now.UTC().Format(timeLayout)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrInvalidReset
	}
	return id, err
}

// userByID returns an account without its password hash.
func (s store) userByID(ctx context.Context, id int64) (User, error) {
	var (
		u       User
		created string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, role, created_at, disabled FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.Role, &created, &u.Disabled)
	if err != nil {
		return User{}, err
	}
	u.CreatedAt, _ = time.Parse(timeLayout, created)
	return u, nil
}

// listAdmins returns the names of the admin accounts, and how many accounts
// there are in all.
func (s store) listAdmins(ctx context.Context) (admins []User, total int, err error) {
	users, err := s.listUsers(ctx)
	if err != nil {
		return nil, 0, err
	}
	for _, u := range users {
		if u.Role == RoleAdmin {
			admins = append(admins, u)
		}
	}
	return admins, len(users), nil
}
