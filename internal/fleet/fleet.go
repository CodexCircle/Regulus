// Package fleet manages the remote agents of the hub: the single-use tokens
// that let a new agent enroll, the registry of the agents that did, and the
// revocation of an agent. The certificates themselves come from package pki.
package fleet

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/CodexCircle/Regulus/internal/pki"
)

const (
	// TokenTTL is how long an enrollment token can be used.
	TokenTTL = time.Hour

	maxNameLen  = 64
	maxFailures = 10
	maxTracked  = 10000
	failWindow  = 15 * time.Minute
)

// Audit events recorded by the fleet.
const (
	EventTokenCreated  = "enroll_token_created"
	EventAgentEnrolled = "agent_enrolled"
	EventEnrollFailed  = "agent_enroll_failed"
	EventAgentRevoked  = "agent_revoked"
)

// Errors returned by the service.
var (
	// ErrInvalidToken is the one answer for a token that is unknown, expired or
	// already used.
	ErrInvalidToken = errors.New("this enrollment token is invalid or has expired")
	ErrRateLimited  = errors.New("too many failed attempts, try again later")
	ErrNotFound     = errors.New("agent not found")
	// ErrRevoked is returned for an agent that was revoked, or whose
	// certificate is not the one the hub issued last.
	ErrRevoked = errors.New("this agent is revoked")
)

// Auditor records events; *auth.Service implements it.
type Auditor interface {
	Record(ctx context.Context, actor, event, target, detail string)
}

// Agent is an enrolled agent.
type Agent struct {
	ID         string
	Name       string
	Hostname   string
	CreatedAt  time.Time
	EnrolledAt string // the address it enrolled from
	Revoked    bool
	RevokedAt  time.Time
}

// Service is the registry of remote agents.
type Service struct {
	db    *sql.DB
	ca    *pki.CA
	audit Auditor
	now   func() time.Time

	mu       sync.Mutex
	failures map[string]*failure
}

type failure struct {
	count int
	since time.Time
}

// New returns the service. audit may be nil.
func New(db *sql.DB, ca *pki.CA, audit Auditor) *Service {
	return &Service{db: db, ca: ca, audit: audit, now: time.Now, failures: map[string]*failure{}}
}

// CA returns the certificate authority, for the listener.
func (s *Service) CA() *pki.CA { return s.ca }

const timeLayout = "2006-01-02T15:04:05.000000000Z"

func ts(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTS(v string) time.Time {
	t, _ := time.Parse(timeLayout, v)
	return t
}

func (s *Service) record(ctx context.Context, actor, event, target, detail string) {
	if s.audit != nil {
		s.audit.Record(ctx, actor, event, target, detail)
	}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// NameError is a problem with the name chosen for a server.
type NameError struct{ msg string }

func (e *NameError) Error() string { return e.msg }

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) > maxNameLen {
		return "", &NameError{fmt.Sprintf("the name can have at most %d characters", maxNameLen)}
	}
	for _, r := range name {
		if r < ' ' || r == 0x7f {
			return "", &NameError{"the name cannot contain control characters"}
		}
	}
	return name, nil
}

// CreateToken makes a single-use enrollment token, valid for TokenTTL. The
// name, if given, becomes the name of the server that enrolls with it. The
// token is returned once; only its hash is kept.
func (s *Service) CreateToken(ctx context.Context, by, name string) (token string, expires time.Time, err error) {
	name, err = cleanName(name)
	if err != nil {
		return "", time.Time{}, err
	}
	token, err = randomToken(24)
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.now()
	expires = now.Add(TokenTTL)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM enrollment_tokens WHERE expires_at <= ?`, ts(now)); err != nil {
		return "", time.Time{}, err
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO enrollment_tokens (token_hash, name, created_at, expires_at, created_by) VALUES (?, ?, ?, ?, ?)`,
		hashToken(token), name, ts(now), ts(expires), by); err != nil {
		return "", time.Time{}, err
	}
	s.record(ctx, by, EventTokenCreated, name, "valid for "+TokenTTL.String())
	return token, expires, nil
}

// Enrolled is the result of a successful enrollment.
type Enrolled struct {
	AgentID          string
	CertificateDER   []byte
	CACertificateDER []byte
}

// Enroll registers a new agent: it uses the token up and signs the agent's
// certificate request. remote is the address of the agent, for the limit on
// failed attempts and the audit log.
func (s *Service) Enroll(ctx context.Context, remote, token string, csrDER []byte, hostname string) (Enrolled, error) {
	if s.limited(remote) {
		return Enrolled{}, ErrRateLimited
	}
	hostname = strings.TrimSpace(hostname)
	if len(hostname) > 255 {
		hostname = hostname[:255]
	}

	now := s.now()
	var name string
	err := s.db.QueryRowContext(ctx,
		`DELETE FROM enrollment_tokens WHERE token_hash = ? AND expires_at > ? RETURNING name`,
		hashToken(token), ts(now)).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		s.fail(remote)
		s.record(ctx, "", EventEnrollFailed, "", "invalid token")
		return Enrolled{}, ErrInvalidToken
	}
	if err != nil {
		return Enrolled{}, err
	}

	id, err := newAgentID()
	if err != nil {
		return Enrolled{}, err
	}
	certDER, serial, err := s.ca.SignClient(csrDER, id, now)
	if err != nil {
		// The token is already used up: a bad request wastes it, and the admin
		// makes another.
		return Enrolled{}, &RequestError{err}
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO agents (id, name, hostname, created_at, enrolled_from, cert_serial) VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, hostname, ts(now), remote, serial); err != nil {
		return Enrolled{}, err
	}
	s.clear(remote)
	s.record(ctx, "", EventAgentEnrolled, id, nameOr(name, hostname))
	return Enrolled{AgentID: id, CertificateDER: certDER, CACertificateDER: s.ca.CertDER()}, nil
}

// RequestError is a problem with the certificate request of an agent.
type RequestError struct{ Err error }

func (e *RequestError) Error() string { return "invalid certificate request: " + e.Err.Error() }
func (e *RequestError) Unwrap() error { return e.Err }

func nameOr(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// Authorize checks that the agent may connect with the certificate that has
// this serial number: the agent exists, is not revoked, and the certificate is
// the one the hub issued for it.
func (s *Service) Authorize(ctx context.Context, id, serial string) error {
	var (
		want    string
		revoked sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `SELECT cert_serial, revoked_at FROM agents WHERE id = ?`, id).Scan(&want, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if revoked.Valid || want != serial {
		return ErrRevoked
	}
	return nil
}

// Revoke makes the agent unable to connect again. It does not close a
// connection that is open: the hub does that.
func (s *Service) Revoke(ctx context.Context, by, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE agents SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, ts(s.now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var one int
		err := s.db.QueryRowContext(ctx, `SELECT 1 FROM agents WHERE id = ?`, id).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err // nil: it was revoked already
	}
	s.record(ctx, by, EventAgentRevoked, id, "")
	return nil
}

// Agents lists the enrolled agents, oldest first.
func (s *Service) Agents(ctx context.Context) ([]Agent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, hostname, created_at, enrolled_from, revoked_at FROM agents ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	list := []Agent{}
	for rows.Next() {
		var (
			a       Agent
			created string
			revoked sql.NullString
		)
		if err := rows.Scan(&a.ID, &a.Name, &a.Hostname, &created, &a.EnrolledAt, &revoked); err != nil {
			return nil, err
		}
		a.CreatedAt = parseTS(created)
		if revoked.Valid {
			a.Revoked, a.RevokedAt = true, parseTS(revoked.String)
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

func (s *Service) limited(remote string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.failures[remote]
	if f == nil {
		return false
	}
	if s.now().Sub(f.since) > failWindow {
		delete(s.failures, remote)
		return false
	}
	return f.count >= maxFailures
}

func (s *Service) fail(remote string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if len(s.failures) > maxTracked { // addresses come from outside, so keep the map bounded
		for k, f := range s.failures {
			if now.Sub(f.since) > failWindow {
				delete(s.failures, k)
			}
		}
	}
	f := s.failures[remote]
	if f == nil || now.Sub(f.since) > failWindow {
		f = &failure{since: now}
		s.failures[remote] = f
	}
	f.count++
}

func (s *Service) clear(remote string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failures, remote)
}

func newAgentID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "agent-" + hex.EncodeToString(b), nil
}
