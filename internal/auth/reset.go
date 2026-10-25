package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"
)

// ResetTTL is how long a reset token can be used.
const ResetTTL = time.Hour

// maxResetTokenLen bounds what is hashed for a token sent by a client.
const maxResetTokenLen = 128

// Audit events of the credential reset.
const (
	EventResetCreated  = "reset_created"
	EventPasswordReset = "password_reset"
)

// ErrInvalidReset is the one answer for a token that is unknown, expired or
// already used, so it says nothing about which accounts exist.
var ErrInvalidReset = errors.New("this reset link is invalid or has expired")

// CreateReset makes a one-time token that lets the owner of an account choose
// a new password, and ends all the sessions of the account, since its
// credentials can no longer be trusted. The admin never learns the password.
// The caller must be an admin; that is checked by the HTTP layer.
func (s *Service) CreateReset(ctx context.Context, by, username string) (string, time.Time, error) {
	target, err := s.userByName(ctx, username)
	if err != nil {
		return "", time.Time{}, err
	}
	return s.issueReset(ctx, by, target, "")
}

func (s *Service) issueReset(ctx context.Context, by string, target User, detail string) (string, time.Time, error) {
	if target.Disabled {
		return "", time.Time{}, &inputError{"the account is disabled: enable it before resetting its password"}
	}
	token, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.now()
	expires := now.Add(ResetTTL)
	if err := s.store.putReset(ctx, target.ID, hashToken(token), now, expires, by); err != nil {
		return "", time.Time{}, err
	}
	if err := s.store.deleteSessionsOf(ctx, target.ID, ""); err != nil {
		return "", time.Time{}, err
	}
	s.audit(ctx, by, EventResetCreated, target.Username, strings.TrimSpace("sessions ended "+detail))
	return token, expires, nil
}

// ResetPassword sets a new password with a reset token, ends every session of
// the account and uses the token up. remote identifies the client for the
// limit on failed attempts. It does not log the user in.
func (s *Service) ResetPassword(ctx context.Context, remote, token, password string) error {
	key := "reset:" + remote
	if s.limited(key) {
		return ErrRateLimited
	}
	if n := utf8.RuneCountInString(password); n < minPasswordLen || n > maxPasswordLen {
		return &inputError{fmt.Sprintf("the new password must be %d to %d characters", minPasswordLen, maxPasswordLen)}
	}
	if token == "" || len(token) > maxResetTokenLen {
		s.recordFailure(key)
		return ErrInvalidReset
	}
	hash := hashToken(token)
	ok, err := s.store.resetPending(ctx, hash, s.now())
	if err != nil {
		return err
	}
	if !ok {
		s.recordFailure(key)
		return ErrInvalidReset
	}
	// Hash before using the token up, so a failure here keeps it valid.
	newHash, err := HashPassword(password)
	if err != nil {
		return err
	}
	id, err := s.store.takeReset(ctx, hash, s.now())
	if err != nil {
		if errors.Is(err, ErrInvalidReset) {
			s.recordFailure(key)
		}
		return err
	}
	user, err := s.store.userByID(ctx, id)
	if err != nil {
		return err
	}
	if user.Disabled {
		return ErrInvalidReset
	}
	if err := s.store.setPassword(ctx, id, newHash); err != nil {
		return err
	}
	if err := s.store.deleteSessionsOf(ctx, id, ""); err != nil {
		return err
	}
	s.clearFailures(key)
	s.audit(ctx, user.Username, EventPasswordReset, user.Username, "reset link used, sessions ended")
	return nil
}

// Recovery is what RecoverAdmin did.
type Recovery struct {
	Username string
	Token    string
	Expires  time.Time
	// Reenabled is true if the account was disabled and is enabled now.
	Reenabled bool
}

// RecoverAdmin gives back access to a hub whose admin cannot log in. It runs
// on the server, against the database, for someone who can reach the data
// directory, which is the proof of ownership: it makes a reset token for the
// named admin (or the only admin, when username is empty), enables the
// account and ends its sessions. The password is chosen later, in the
// dashboard, so it never shows up in a terminal or in the shell history.
func RecoverAdmin(ctx context.Context, db *sql.DB, username string, log *slog.Logger) (Recovery, error) {
	s := &Service{store: store{db}, log: log, now: time.Now, failures: map[string]*failure{}}
	admins, total, err := s.store.listAdmins(ctx)
	if err != nil {
		return Recovery{}, err
	}
	if total == 0 {
		return Recovery{}, errors.New("there are no accounts yet: start the hub and create the admin with the setup token it logs")
	}

	username = normalizeUsername(username)
	var target User
	switch {
	case username != "":
		for _, a := range admins {
			if a.Username == username {
				target = a
			}
		}
		if target.ID == 0 {
			return Recovery{}, fmt.Errorf("%q is not an admin account (admins: %s)", username, adminNames(admins))
		}
	case len(admins) == 1:
		target = admins[0]
	case len(admins) == 0:
		return Recovery{}, errors.New("there is no admin account; the hub keeps at least one, so the database looks damaged")
	default:
		return Recovery{}, fmt.Errorf("there are several admins (%s): choose one with --user", adminNames(admins))
	}

	rec := Recovery{Username: target.Username}
	if target.Disabled {
		if err := s.store.setDisabled(ctx, target.ID, false); err != nil {
			return Recovery{}, err
		}
		target.Disabled = false
		rec.Reenabled = true
		s.audit(ctx, ConsoleActor, EventUserEnabled, target.Username, "reset-admin command")
	}
	rec.Token, rec.Expires, err = s.issueReset(ctx, ConsoleActor, target, "(reset-admin command)")
	return rec, err
}

// ConsoleActor is the name under which commands run on the server appear in
// the audit log.
const ConsoleActor = "console"

func adminNames(admins []User) string {
	names := make([]string, len(admins))
	for i, a := range admins {
		names[i] = a.Username
	}
	return strings.Join(names, ", ")
}
