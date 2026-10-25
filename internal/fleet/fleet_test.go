package fleet

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodexCircle/Regulus/internal/pki"
	"github.com/CodexCircle/Regulus/internal/storage"
)

type audit struct {
	mu     sync.Mutex
	events []string
}

func (a *audit) Record(_ context.Context, actor, event, target, detail string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event+" "+actor+" "+target+" "+detail)
}

func (a *audit) text() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return strings.Join(a.events, "\n")
}

func newService(t *testing.T) (*Service, *audit) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ca, err := pki.LoadOrCreate(dir+"/pki", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	a := &audit{}
	return New(db, ca, a), a
}

func csr(t *testing.T) []byte {
	t.Helper()
	_, der, err := pki.NewAgentKey()
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func TestEnrollmentUsesTheTokenUp(t *testing.T) {
	s, a := newService(t)
	ctx := context.Background()
	token, expires, err := s.CreateToken(ctx, "admin", " Web 1 ")
	if err != nil {
		t.Fatal(err)
	}
	if got := expires.Sub(s.now()); got < TokenTTL-time.Minute || got > TokenTTL+time.Minute {
		t.Errorf("token valid for %v, want about %v", got, TokenTTL)
	}

	got, err := s.Enroll(ctx, "10.0.0.5", token, csr(t), "web1.example")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	cert, err := x509.ParseCertificate(got.CertificateDER)
	if err != nil || cert.Subject.CommonName != got.AgentID || !strings.HasPrefix(got.AgentID, "agent-") {
		t.Fatalf("certificate = %v, %v; agent id = %q", cert, err, got.AgentID)
	}
	if string(got.CACertificateDER) != string(s.CA().CertDER()) {
		t.Error("the response does not carry the CA certificate")
	}

	list, _ := s.Agents(ctx)
	if len(list) != 1 || list[0].ID != got.AgentID || list[0].Name != "Web 1" || list[0].Hostname != "web1.example" || list[0].Revoked {
		t.Fatalf("agents = %+v", list)
	}

	// A token works once.
	if _, err := s.Enroll(ctx, "10.0.0.5", token, csr(t), "again"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("second use: err = %v, want ErrInvalidToken", err)
	}
	if !strings.Contains(a.text(), EventTokenCreated+" admin Web 1") || !strings.Contains(a.text(), EventAgentEnrolled+"  "+got.AgentID) {
		t.Errorf("audit =\n%s", a.text())
	}
	if strings.Contains(a.text(), token) {
		t.Error("the audit log holds the token")
	}
}

func TestTokensExpireAndAreNotStoredInTheClear(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	token, _, _ := s.CreateToken(ctx, "admin", "")
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM enrollment_tokens WHERE token_hash = ?`, token).Scan(&n); err != nil || n != 0 {
		t.Errorf("the raw token is stored: n = %d, err = %v", n, err)
	}
	now := s.now()
	s.now = func() time.Time { return now.Add(TokenTTL + time.Second) }
	if _, err := s.Enroll(ctx, "r", token, csr(t), ""); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expired token: err = %v, want ErrInvalidToken", err)
	}
}

func TestBadNamesAreRefused(t *testing.T) {
	s, _ := newService(t)
	for _, name := range []string{strings.Repeat("x", 65), "a\nb"} {
		var ne *NameError
		if _, _, err := s.CreateToken(context.Background(), "admin", name); !errors.As(err, &ne) {
			t.Errorf("name %q: err = %v, want a NameError", name, err)
		}
	}
}

func TestABadRequestSpendsTheTokenButEnrollsNobody(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	token, _, _ := s.CreateToken(ctx, "admin", "")
	var re *RequestError
	if _, err := s.Enroll(ctx, "r", token, []byte("not a csr"), ""); !errors.As(err, &re) {
		t.Fatalf("err = %v, want a RequestError", err)
	}
	if list, _ := s.Agents(ctx); len(list) != 0 {
		t.Errorf("agents = %+v", list)
	}
	if _, err := s.Enroll(ctx, "r", token, csr(t), ""); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("reuse: err = %v", err)
	}
}

func TestGuessingTokensIsRateLimited(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	good, _, _ := s.CreateToken(ctx, "admin", "")
	for range maxFailures {
		if _, err := s.Enroll(ctx, "9.9.9.9", "guess", csr(t), ""); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("guess: err = %v", err)
		}
	}
	if _, err := s.Enroll(ctx, "9.9.9.9", good, csr(t), ""); !errors.Is(err, ErrRateLimited) {
		t.Errorf("after the guesses: err = %v, want ErrRateLimited", err)
	}
	if _, err := s.Enroll(ctx, "8.8.8.8", good, csr(t), ""); err != nil {
		t.Errorf("another address: %v", err)
	}
}

func TestAuthorizeAndRevoke(t *testing.T) {
	s, a := newService(t)
	ctx := context.Background()
	token, _, _ := s.CreateToken(ctx, "admin", "")
	got, _ := s.Enroll(ctx, "r", token, csr(t), "h")
	cert, _ := x509.ParseCertificate(got.CertificateDER)
	serial := cert.SerialNumber.Text(16)

	if err := s.Authorize(ctx, got.AgentID, serial); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if err := s.Authorize(ctx, got.AgentID, "deadbeef"); !errors.Is(err, ErrRevoked) {
		t.Errorf("another certificate: err = %v, want ErrRevoked", err)
	}
	if err := s.Authorize(ctx, "agent-unknown", serial); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown agent: err = %v, want ErrNotFound", err)
	}

	if err := s.Revoke(ctx, "admin", got.AgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, got.AgentID, serial); !errors.Is(err, ErrRevoked) {
		t.Errorf("revoked: err = %v, want ErrRevoked", err)
	}
	if err := s.Revoke(ctx, "admin", got.AgentID); err != nil { // again: no change, no error
		t.Errorf("revoking twice: %v", err)
	}
	if err := s.Revoke(ctx, "admin", "agent-unknown"); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoking an unknown agent: err = %v, want ErrNotFound", err)
	}
	if n := strings.Count(a.text(), EventAgentRevoked); n != 1 {
		t.Errorf("revocation audited %d times, want 1:\n%s", n, a.text())
	}
	if list, _ := s.Agents(ctx); !list[0].Revoked || list[0].RevokedAt.IsZero() {
		t.Errorf("agents = %+v", list)
	}
}
