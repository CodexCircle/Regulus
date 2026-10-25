package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// totpRow is the TOTP enrollment of a user.
type totpRow struct {
	secretEnc string
	confirmed bool
	lastStep  int64
}

// totpOf returns the enrollment of the user, or sql.ErrNoRows.
func (s store) totpOf(ctx context.Context, userID int64) (totpRow, error) {
	var (
		r         totpRow
		confirmed sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT secret_enc, confirmed_at, last_step FROM user_totp WHERE user_id = ?`, userID).
		Scan(&r.secretEnc, &confirmed, &r.lastStep)
	r.confirmed = confirmed.Valid
	return r, err
}

// putTOTP starts an enrollment, replacing one that was never confirmed. It
// does nothing, and reports false, when two-factor is already on.
func (s store) putTOTP(ctx context.Context, userID int64, secretEnc string, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO user_totp (user_id, secret_enc, created_at, last_step) VALUES (?, ?, ?, 0)
		 ON CONFLICT (user_id) DO UPDATE SET secret_enc = excluded.secret_enc, created_at = excluded.created_at, last_step = 0
		 WHERE user_totp.confirmed_at IS NULL`,
		userID, secretEnc, now.UTC().Format(timeLayout))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// confirmTOTP turns two-factor on, remembering the step of the code that
// proved it. It reports false when there was nothing to confirm.
func (s store) confirmTOTP(ctx context.Context, userID, step int64, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE user_totp SET confirmed_at = ?, last_step = ? WHERE user_id = ? AND confirmed_at IS NULL`,
		now.UTC().Format(timeLayout), step, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// useStep records that a time step was used. It reports false if that step,
// or a later one, was already used: the code is a replay.
func (s store) useStep(ctx context.Context, userID, step int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE user_totp SET last_step = ? WHERE user_id = ? AND confirmed_at IS NOT NULL AND last_step < ?`,
		step, userID, step)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// clearTwoFactor removes the TOTP secret and the recovery codes of a user.
// It reports whether two-factor was on or being set up.
func (s store) clearTwoFactor(ctx context.Context, userID int64) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `DELETE FROM user_totp WHERE user_id = ?`, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pending_logins WHERE user_id = ?`, userID); err != nil {
		return false, err
	}
	return n > 0, tx.Commit()
}

// replaceRecoveryCodes stores new code hashes in place of all the old ones.
func (s store) replaceRecoveryCodes(ctx context.Context, userID int64, hashes []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, h := range hashes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes (user_id, code_hash) VALUES (?, ?)`, userID, h); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// useRecoveryCode marks a code as used; false if there is no such unused code.
func (s store) useRecoveryCode(ctx context.Context, userID int64, hash string, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`,
		now.UTC().Format(timeLayout), userID, hash)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s store) recoveryCodesLeft(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM recovery_codes WHERE user_id = ? AND used_at IS NULL`, userID).Scan(&n)
	return n, err
}

// putPending stores a login that waits for the second factor.
func (s store) putPending(ctx context.Context, tokenHash string, userID int64, now, expires time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pending_logins (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		tokenHash, userID, now.UTC().Format(timeLayout), expires.UTC().Format(timeLayout))
	return err
}

// tryPending counts an attempt on a pending login and returns its user. It
// returns false if the login is unknown, expired, or already used up its
// attempts (it is deleted in that case).
func (s store) tryPending(ctx context.Context, tokenHash string, now time.Time, maxAttempts int) (int64, bool, error) {
	var (
		userID   int64
		expires  string
		attempts int
	)
	err := s.db.QueryRowContext(ctx,
		`UPDATE pending_logins SET attempts = attempts + 1 WHERE token_hash = ? RETURNING user_id, expires_at, attempts`,
		tokenHash).Scan(&userID, &expires, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	exp, perr := time.Parse(timeLayout, expires)
	if perr != nil || !now.Before(exp) || attempts > maxAttempts {
		_ = s.deletePending(ctx, tokenHash)
		return 0, false, nil
	}
	return userID, true, nil
}

func (s store) deletePending(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_logins WHERE token_hash = ?`, tokenHash)
	return err
}

func (s store) deleteExpiredPending(ctx context.Context, now time.Time) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM pending_logins WHERE expires_at <= ?`, now.UTC().Format(timeLayout))
}

// Settings (key/value) of the hub.

func (s store) setting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s store) putSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
