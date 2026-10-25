// Package auth implements the hub's local accounts: first-run setup of the
// admin, password hashing, login sessions and the HTTP middleware that
// protects the API.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// SessionTTL is how long a login stays valid.
	SessionTTL = 7 * 24 * time.Hour

	minPasswordLen = 10
	maxPasswordLen = 128

	maxFailures   = 10
	failureWindow = 15 * time.Minute
)

var usernameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)

// Service manages accounts and sessions.
type Service struct {
	store store
	log   *slog.Logger
	now   func() time.Time

	// box encrypts the TOTP secrets; nil when the hub has no secret key, which
	// turns two-factor off.
	box *sealer

	mu         sync.Mutex
	setupToken string // empty once the first admin exists
	failures   map[string]*failure
	policy     Policy
	rolePerms  map[string]cachedPerms // permissions per role, cleared when a role changes

	dummyOnce sync.Once
	dummyHash string
}

type failure struct {
	count int
	since time.Time
}

// Option customizes a Service.
type Option func(*Service)

// WithSecret sets the hub secret that protects the TOTP secrets (see
// LoadOrCreateSecret). Without it two-factor authentication is unavailable.
func WithSecret(secret []byte) Option {
	return func(s *Service) {
		if box, err := sealerFor(secret); err == nil {
			s.box = box
		}
	}
}

// New returns the service for the hub database. If there is no user yet it
// generates a one-time setup token and logs it: whoever can read the hub log
// can create the first admin, and nobody else.
func New(ctx context.Context, db *sql.DB, log *slog.Logger, opts ...Option) (*Service, error) {
	s := &Service{store: store{db}, log: log, now: time.Now, failures: map[string]*failure{}, rolePerms: map[string]cachedPerms{}}
	for _, opt := range opts {
		opt(s)
	}
	if err := s.loadPolicy(ctx); err != nil {
		return nil, err
	}
	n, err := s.store.countUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("count users: %w", err)
	}
	if n == 0 {
		token, err := randomToken(16)
		if err != nil {
			return nil, err
		}
		s.setupToken = token
		log.Warn("no accounts yet: open the dashboard and create the admin with this one-time setup token", "setup_token", token)
	}
	return s, nil
}

// SetupToken returns the pending one-time setup token, or "" when the setup
// was already completed.
func (s *Service) SetupToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setupToken
}

// SetupRequired reports whether the hub has no accounts yet.
func (s *Service) SetupRequired() bool {
	return s.SetupToken() != ""
}

// Setup creates the first admin account, which must present the setup token,
// and logs it in. It returns the session token for the cookie.
func (s *Service) Setup(ctx context.Context, setupToken, username, password string) (User, string, time.Time, error) {
	username = normalizeUsername(username)
	if err := validateCredentials(username, password); err != nil {
		return User{}, "", time.Time{}, err
	}

	s.mu.Lock()
	want := s.setupToken
	s.mu.Unlock()
	if want == "" {
		return User{}, "", time.Time{}, ErrSetupDone
	}
	if subtle.ConstantTimeCompare([]byte(setupToken), []byte(want)) != 1 {
		return User{}, "", time.Time{}, ErrBadSetupToken
	}

	hash, err := HashPassword(password)
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	now := s.now()
	user, err := s.store.createFirstAdmin(ctx, username, hash, now)
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	s.mu.Lock()
	s.setupToken = ""
	s.mu.Unlock()
	s.log.Info("admin account created", "user", user.Username)
	s.audit(ctx, user.Username, EventSetup, user.Username, "first admin")

	token, expires, err := s.newSession(ctx, user.ID, now)
	return user, token, expires, err
}

// Login checks the credentials and starts a session. remote identifies the
// client (its IP address) for the limit on failed attempts.
func (s *Service) Login(ctx context.Context, remote, username, password string) (User, string, time.Time, error) {
	if s.limited(remote) {
		return User{}, "", time.Time{}, ErrRateLimited
	}
	user, hash, err := s.store.userByName(ctx, normalizeUsername(username))
	known := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return User{}, "", time.Time{}, err
	}
	if !known {
		hash = s.dummy() // spend the same time as for a real user
	}
	ok, verr := VerifyPassword(hash, password)
	if verr != nil {
		s.log.Error("stored password hash is unusable", "user", user.Username, "err", verr)
	}
	if !known || !ok || user.Disabled {
		s.recordFailure(remote)
		detail := ""
		if known && ok && user.Disabled {
			detail = "account disabled"
		}
		s.audit(ctx, usernameForAudit(username), EventLoginFailed, "", detail)
		return User{}, "", time.Time{}, ErrInvalidCredentials
	}
	if user.TwoFactor {
		// The password is right but the login is not done. Failures are not
		// cleared yet: a wrong code must keep counting.
		pending, err := s.beginPending(ctx, user.ID)
		if err != nil {
			return User{}, "", time.Time{}, err
		}
		return user, "", time.Time{}, pending
	}
	s.clearFailures(remote)

	now := s.now()
	_ = s.store.deleteExpiredSessions(ctx, now) // housekeeping; a failure here must not block the login
	token, expires, err := s.newSession(ctx, user.ID, now)
	if err == nil {
		s.audit(ctx, user.Username, EventLogin, "", "")
	}
	return user, token, expires, err
}

// Authenticate returns the user of a session token.
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrUnauthenticated
	}
	hash := hashToken(token)
	now := s.now()
	user, err := s.store.sessionUser(ctx, hash, now)
	if err == nil {
		s.store.touchSession(ctx, hash, now)
		user.Permissions = s.permsOfUser(ctx, user).list()
	}
	return user, err
}

// Logout ends the session of the token. An unknown token is not an error.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.deleteSession(ctx, hashToken(token))
}

func (s *Service) newSession(ctx context.Context, userID int64, now time.Time) (string, time.Time, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := now.Add(SessionTTL)
	if err := s.store.createSession(ctx, hashToken(token), userID, now, expires, clientFrom(ctx)); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

func (s *Service) dummy() string {
	s.dummyOnce.Do(func() { s.dummyHash, _ = HashPassword("not a real password") })
	return s.dummyHash
}

// limited reports whether the client made too many failed logins recently.
func (s *Service) limited(remote string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.failures[remote]
	if f == nil {
		return false
	}
	if s.now().Sub(f.since) > failureWindow {
		delete(s.failures, remote)
		return false
	}
	return f.count >= maxFailures
}

func (s *Service) recordFailure(remote string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	f := s.failures[remote]
	if f == nil || now.Sub(f.since) > failureWindow {
		f = &failure{since: now}
		s.failures[remote] = f
	}
	f.count++
}

func (s *Service) clearFailures(remote string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failures, remote)
}

func normalizeUsername(u string) string {
	return strings.ToLower(strings.TrimSpace(u))
}

// inputError is a problem with the username or password chosen by the user.
type inputError struct{ msg string }

func (e *inputError) Error() string { return e.msg }

func validateCredentials(username, password string) error {
	if !usernameRE.MatchString(username) {
		return &inputError{"username must be 3 to 32 characters: letters, digits, dot, dash or underscore, starting with a letter or digit"}
	}
	n := utf8.RuneCountInString(password)
	if n < minPasswordLen {
		return &inputError{fmt.Sprintf("password must be at least %d characters", minPasswordLen)}
	}
	if n > maxPasswordLen {
		return &inputError{fmt.Sprintf("password must be at most %d characters", maxPasswordLen)}
	}
	return nil
}

// randomToken returns n random bytes, base64url encoded.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
