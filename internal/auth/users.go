package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Audit events recorded by the service.
const (
	EventSetup          = "setup"
	EventLogin          = "login"
	EventLoginFailed    = "login_failed"
	EventUserCreated    = "user_created"
	EventRoleChanged    = "role_changed"
	EventUserDisabled   = "user_disabled"
	EventUserEnabled    = "user_enabled"
	EventUserDeleted    = "user_deleted"
	EventPasswordChange = "password_changed"
	EventPasswordFailed = "password_change_failed"
	EventSessionRevoked = "session_revoked"
)

// audit records an event. A failure to record it is logged and does not stop
// the action it describes.
func (s *Service) audit(ctx context.Context, actor, event, target, detail string) {
	e := AuditEntry{
		At:     s.now(),
		Actor:  clip(actor, 64),
		Event:  event,
		Target: clip(target, 64),
		Remote: clientFrom(ctx).Remote,
		Detail: clip(detail, 200),
	}
	if err := s.store.addAudit(ctx, e); err != nil {
		s.log.Error("cannot record audit event", "event", event, "err", err)
	}
}

// Users lists all accounts, ordered by name.
func (s *Service) Users(ctx context.Context) ([]User, error) {
	return s.store.listUsers(ctx)
}

func validRole(r Role) bool { return r == RoleAdmin || r == RoleViewer }

// CreateUser adds an account with an initial password. The caller must be an
// admin; that is checked by the HTTP layer.
func (s *Service) CreateUser(ctx context.Context, by User, username, password string, role Role) (User, error) {
	username = normalizeUsername(username)
	if err := validateCredentials(username, password); err != nil {
		return User{}, err
	}
	if !validRole(role) {
		return User{}, &inputError{`role must be "admin" or "viewer"`}
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	u, err := s.store.createUser(ctx, username, hash, role, s.now())
	if err != nil {
		return User{}, err
	}
	s.audit(ctx, by.Username, EventUserCreated, u.Username, "role "+string(role))
	return u, nil
}

// UserChange lists the changes to an account; nil fields stay as they are.
type UserChange struct {
	Role     *Role
	Disabled *bool
}

// UpdateUser changes the role or the enabled state of an account. The last
// enabled admin cannot be demoted or disabled. Disabling an account ends its
// sessions.
func (s *Service) UpdateUser(ctx context.Context, by User, username string, ch UserChange) (User, error) {
	target, err := s.userByName(ctx, username)
	if err != nil {
		return User{}, err
	}
	if ch.Role != nil {
		if !validRole(*ch.Role) {
			return User{}, &inputError{`role must be "admin" or "viewer"`}
		}
		if *ch.Role != target.Role {
			if err := s.store.setRole(ctx, target.ID, *ch.Role); err != nil {
				return User{}, err
			}
			s.audit(ctx, by.Username, EventRoleChanged, target.Username, fmt.Sprintf("%s to %s", target.Role, *ch.Role))
			target.Role = *ch.Role
		}
	}
	if ch.Disabled != nil && *ch.Disabled != target.Disabled {
		if err := s.store.setDisabled(ctx, target.ID, *ch.Disabled); err != nil {
			return User{}, err
		}
		event := EventUserEnabled
		if *ch.Disabled {
			event = EventUserDisabled
			if err := s.store.deleteSessionsOf(ctx, target.ID, ""); err != nil {
				return User{}, err
			}
		}
		s.audit(ctx, by.Username, event, target.Username, "")
		target.Disabled = *ch.Disabled
	}
	return target, nil
}

// DeleteUser removes an account and its sessions. The last enabled admin
// cannot be deleted.
func (s *Service) DeleteUser(ctx context.Context, by User, username string) error {
	target, err := s.userByName(ctx, username)
	if err != nil {
		return err
	}
	if err := s.store.deleteUser(ctx, target.ID); err != nil {
		return err
	}
	_ = s.store.deleteSessionsOf(ctx, target.ID, "") // also cascaded by the database
	s.audit(ctx, by.Username, EventUserDeleted, target.Username, "")
	return nil
}

func (s *Service) userByName(ctx context.Context, username string) (User, error) {
	u, _, err := s.store.userByName(ctx, normalizeUsername(username))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return u, err
}

// ChangePassword sets a new password for the user after checking the current
// one, and ends all their other sessions (keepToken is the one to keep). Wrong
// current passwords count toward the rate limit, so a stolen session cannot be
// used to guess the password.
func (s *Service) ChangePassword(ctx context.Context, user User, keepToken, current, next string) error {
	key := fmt.Sprintf("password:%d", user.ID)
	if s.limited(key) {
		return ErrRateLimited
	}
	n := utf8.RuneCountInString(next)
	if n < minPasswordLen || n > maxPasswordLen {
		return &inputError{fmt.Sprintf("the new password must be %d to %d characters", minPasswordLen, maxPasswordLen)}
	}
	hash, err := s.store.passwordHash(ctx, user.ID)
	if err != nil {
		return err
	}
	ok, _ := VerifyPassword(hash, current)
	if !ok {
		s.recordFailure(key)
		s.audit(ctx, user.Username, EventPasswordFailed, user.Username, "")
		return ErrWrongPassword
	}
	s.clearFailures(key)
	newHash, err := HashPassword(next)
	if err != nil {
		return err
	}
	if err := s.store.setPassword(ctx, user.ID, newHash); err != nil {
		return err
	}
	if err := s.store.deleteSessionsOf(ctx, user.ID, hashToken(keepToken)); err != nil {
		return err
	}
	s.audit(ctx, user.Username, EventPasswordChange, user.Username, "other sessions ended")
	return nil
}

// Sessions lists the active sessions of the user; currentToken marks the one
// that made the request.
func (s *Service) Sessions(ctx context.Context, user User, currentToken string) ([]SessionInfo, error) {
	return s.store.listSessions(ctx, user.ID, s.now(), hashToken(currentToken))
}

// RevokeSession ends one session of the user, by the id shown in Sessions.
func (s *Service) RevokeSession(ctx context.Context, user User, id string) error {
	if err := s.store.deleteSessionByID(ctx, user.ID, id); err != nil {
		return err
	}
	s.audit(ctx, user.Username, EventSessionRevoked, user.Username, "")
	return nil
}

// Audit returns the most recent account events, newest first.
func (s *Service) Audit(ctx context.Context, limit int) ([]AuditEntry, error) {
	limit = min(max(limit, 1), 500)
	return s.store.listAudit(ctx, limit)
}

func usernameForAudit(u string) string { return strings.ToLower(clip(strings.TrimSpace(u), 64)) }

// Record adds an event to the audit log on behalf of the other parts of the
// hub. The remote address is taken from the context (see WithClient).
func (s *Service) Record(ctx context.Context, actor, event, target, detail string) {
	s.audit(ctx, actor, event, target, detail)
}
