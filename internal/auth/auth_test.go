package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CodexCircle/Regulus/internal/storage"
)

const goodPassword = "correct horse battery"

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := New(context.Background(), db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func setup(t *testing.T, s *Service) (User, string) {
	t.Helper()
	u, token, _, err := s.Setup(context.Background(), s.SetupToken(), "Admin", goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	return u, token
}

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword(goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected hash format: %s", h)
	}
	if h2, _ := HashPassword(goodPassword); h2 == h {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
	if ok, err := VerifyPassword(h, goodPassword); err != nil || !ok {
		t.Fatalf("correct password rejected: ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword(h, goodPassword+"x"); err != nil || ok {
		t.Fatalf("wrong password accepted: ok=%v err=%v", ok, err)
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	for _, h := range []string{
		"", "plain", "$bcrypt$x$y$z$w", "$argon2id$v=19$m=19456,t=2,p=1$onlysalt",
		"$argon2id$v=18$m=19456,t=2,p=1$c2FsdHNhbHQ$aGFzaA",
		"$argon2id$v=19$m=99999999,t=2,p=1$c2FsdHNhbHQ$aGFzaA", // memory far beyond the cap
		"$argon2id$v=19$m=19456,t=0,p=1$c2FsdHNhbHQ$aGFzaA",
		"$argon2id$v=19$m=19456,t=2,p=1$!!$aGFzaA",
	} {
		if ok, err := VerifyPassword(h, "x"); err == nil || ok {
			t.Errorf("%q: ok=%v err=%v, want an error", h, ok, err)
		}
	}
}

func TestSetupCreatesTheFirstAdminOnlyOnce(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if !s.SetupRequired() || s.SetupToken() == "" {
		t.Fatal("a fresh hub must require the setup")
	}

	if _, _, _, err := s.Setup(ctx, "wrong", "admin", goodPassword); !errors.Is(err, ErrBadSetupToken) {
		t.Fatalf("wrong token: err = %v, want ErrBadSetupToken", err)
	}
	if !s.SetupRequired() {
		t.Fatal("a wrong token must not complete the setup")
	}

	token := s.SetupToken()
	user, session, expires, err := s.Setup(ctx, token, "  Admin ", goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	if user.Username != "admin" || user.Role != RoleAdmin || session == "" || !expires.After(time.Now()) {
		t.Fatalf("unexpected result: %+v %q %v", user, session, expires)
	}
	if s.SetupRequired() {
		t.Fatal("setup must be closed after the first admin exists")
	}
	if _, _, _, err := s.Setup(ctx, token, "other", goodPassword); !errors.Is(err, ErrSetupDone) {
		t.Fatalf("second setup: err = %v, want ErrSetupDone", err)
	}
}

func TestSetupSurvivesRestartOnlyWhileNoUserExists(t *testing.T) {
	dir := t.TempDir()
	open := func() *Service {
		db, err := storage.Open(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		s, err := New(context.Background(), db, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	first := open()
	if !first.SetupRequired() {
		t.Fatal("setup must be required")
	}
	setup(t, first)
	if second := open(); second.SetupRequired() {
		t.Fatal("setup must not be offered again once an account exists")
	}
}

func TestSetupValidatesInput(t *testing.T) {
	s := newService(t)
	for name, tc := range map[string][2]string{
		"short username": {"ab", goodPassword},
		"bad characters": {"ad min", goodPassword},
		"short password": {"admin", "short"},
		"long password":  {"admin", strings.Repeat("x", maxPasswordLen+1)},
	} {
		_, _, _, err := s.Setup(context.Background(), s.SetupToken(), tc[0], tc[1])
		var in *inputError
		if !errors.As(err, &in) {
			t.Errorf("%s: err = %v, want an input error", name, err)
		}
	}
	if !s.SetupRequired() {
		t.Fatal("invalid input must not complete the setup")
	}
}

func TestLoginAndSessions(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	setup(t, s)

	if _, _, _, err := s.Login(ctx, "1.2.3.4", "admin", "wrong password!"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: err = %v", err)
	}
	if _, _, _, err := s.Login(ctx, "1.2.3.4", "nobody", goodPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user: err = %v", err)
	}

	user, token, _, err := s.Login(ctx, "1.2.3.4", "ADMIN", goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Authenticate(ctx, token)
	if err != nil || got.Username != user.Username || got.Role != RoleAdmin {
		t.Fatalf("Authenticate = %+v, %v", got, err)
	}

	if err := s.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("after logout: err = %v, want ErrUnauthenticated", err)
	}
	if _, err := s.Authenticate(ctx, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("empty token: err = %v", err)
	}
	if err := s.Logout(ctx, "unknown"); err != nil {
		t.Fatalf("logout of an unknown token: %v", err)
	}
}

func TestSessionExpires(t *testing.T) {
	s := newService(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	_, token := setup(t, s)

	if _, err := s.Authenticate(context.Background(), token); err != nil {
		t.Fatalf("fresh session: %v", err)
	}
	now = now.Add(SessionTTL + time.Second)
	if _, err := s.Authenticate(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired session: err = %v, want ErrUnauthenticated", err)
	}
}

func TestSessionTokenIsNotStored(t *testing.T) {
	s := newService(t)
	_, token := setup(t, s)
	var n int
	err := s.store.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, token).Scan(&n)
	if err != nil || n != 0 {
		t.Fatalf("the raw token must not be stored (n=%d, err=%v)", n, err)
	}
}

func TestLoginRateLimit(t *testing.T) {
	s := newService(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	ctx := context.Background()
	setup(t, s)

	for range maxFailures {
		if _, _, _, err := s.Login(ctx, "9.9.9.9", "admin", "wrong password!"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("err = %v", err)
		}
	}
	// Even the right password is refused while the client is blocked.
	if _, _, _, err := s.Login(ctx, "9.9.9.9", "admin", goodPassword); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("blocked client: err = %v, want ErrRateLimited", err)
	}
	// Other clients are not affected.
	if _, _, _, err := s.Login(ctx, "8.8.8.8", "admin", goodPassword); err != nil {
		t.Fatalf("other client: %v", err)
	}
	// The block ends with the window.
	now = now.Add(failureWindow + time.Second)
	if _, _, _, err := s.Login(ctx, "9.9.9.9", "admin", goodPassword); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}

// --- HTTP ---

type client struct {
	t       *testing.T
	handler http.Handler
	cookie  *http.Cookie
}

func newClient(t *testing.T, s *Service) *client {
	t.Helper()
	mux := http.NewServeMux()
	s.Register(mux)
	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/secret", func(w http.ResponseWriter, r *http.Request) {
		u, _ := UserFrom(r.Context())
		_, _ = io.WriteString(w, "hello "+u.Username)
	})
	for _, r := range APIRoutes(s) {
		protected.Handle(r.Pattern, r.Handler)
	}
	mux.Handle("/api/", s.Require(protected))
	return &client{t: t, handler: mux}
}

func (c *client) do(method, path string, body any, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	c.t.Helper()
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.RemoteAddr = "192.0.2.1:1234"
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	for _, m := range mutate {
		m(r)
	}
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, r)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == CookieName {
			if ck.MaxAge < 0 {
				c.cookie = nil
			} else {
				c.cookie = ck
			}
		}
	}
	return rec
}

func TestHTTPFlow(t *testing.T) {
	s := newService(t)
	c := newClient(t, s)

	// Before anything: setup pending, nobody logged in, the API is closed.
	var me struct {
		SetupRequired bool  `json:"setup_required"`
		User          *User `json:"user"`
	}
	rec := c.do("GET", "/api/auth/me", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || !me.SetupRequired || me.User != nil {
		t.Fatalf("me before setup: %s (%v)", rec.Body, err)
	}
	if rec := c.do("GET", "/api/secret", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("secret without login: status = %d, want 401", rec.Code)
	}

	// Setup needs the token.
	if rec := c.do("POST", "/api/auth/setup", map[string]string{"username": "admin", "password": goodPassword, "setup_token": "nope"}); rec.Code != http.StatusForbidden {
		t.Fatalf("setup with a bad token: status = %d, want 403", rec.Code)
	}
	rec = c.do("POST", "/api/auth/setup", map[string]string{"username": "admin", "password": goodPassword, "setup_token": s.SetupToken()})
	if rec.Code != http.StatusOK || c.cookie == nil {
		t.Fatalf("setup: status = %d, cookie = %v, body = %s", rec.Code, c.cookie, rec.Body)
	}
	if !c.cookie.HttpOnly || c.cookie.SameSite != http.SameSiteLaxMode || c.cookie.Path != "/" {
		t.Errorf("session cookie must be HttpOnly, SameSite=Lax and site-wide: %+v", c.cookie)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Errorf("the response must not mention the password: %s", rec.Body)
	}

	// Logged in.
	if rec := c.do("GET", "/api/secret", nil); rec.Code != http.StatusOK || rec.Body.String() != "hello admin" {
		t.Fatalf("secret after setup: status = %d, body = %q", rec.Code, rec.Body)
	}
	rec = c.do("GET", "/api/auth/me", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || me.SetupRequired || me.User == nil || me.User.Username != "admin" {
		t.Fatalf("me after setup: %s (%v)", rec.Body, err)
	}
	if rec := c.do("POST", "/api/auth/setup", map[string]string{"username": "x2x", "password": goodPassword, "setup_token": "any"}); rec.Code != http.StatusConflict {
		t.Fatalf("second setup: status = %d, want 409", rec.Code)
	}

	// Logout closes the session; the old cookie no longer works.
	old := c.cookie
	if rec := c.do("POST", "/api/auth/logout", struct{}{}); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: status = %d", rec.Code)
	}
	c.cookie = old
	if rec := c.do("GET", "/api/secret", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("secret with a logged-out cookie: status = %d, want 401", rec.Code)
	}
	c.cookie = nil

	// Login again.
	if rec := c.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "wrong password!"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login: status = %d, want 401", rec.Code)
	}
	if rec := c.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": goodPassword}); rec.Code != http.StatusOK || c.cookie == nil {
		t.Fatalf("login: status = %d", rec.Code)
	}
	if rec := c.do("GET", "/api/secret", nil); rec.Code != http.StatusOK {
		t.Fatalf("secret after login: status = %d", rec.Code)
	}
}

func TestHTTPRejectsCrossSiteAndMalformedPosts(t *testing.T) {
	s := newService(t)
	c := newClient(t, s)
	body := map[string]string{"username": "admin", "password": goodPassword, "setup_token": s.SetupToken()}

	form := func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }
	if rec := c.do("POST", "/api/auth/setup", body, form); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("form post: status = %d, want 415", rec.Code)
	}
	evil := func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }
	if rec := c.do("POST", "/api/auth/setup", body, evil); rec.Code != http.StatusForbidden {
		t.Errorf("foreign origin: status = %d, want 403", rec.Code)
	}
	own := func(r *http.Request) { r.Header.Set("Origin", "http://"+r.Host) }
	if rec := c.do("POST", "/api/auth/setup", body, own); rec.Code != http.StatusOK {
		t.Errorf("same origin: status = %d, want 200", rec.Code)
	}

	c2 := newClient(t, newService(t))
	if rec := c2.do("POST", "/api/auth/login", map[string]any{"username": "a", "extra": 1}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: status = %d, want 400", rec.Code)
	}
	if rec := c2.do("POST", "/api/auth/setup", map[string]string{"username": "ab", "password": "x", "setup_token": c2.t.Name()}); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid input: status = %d, want 400", rec.Code)
	}
}

func TestHTTPRateLimitAnswers429(t *testing.T) {
	s := newService(t)
	c := newClient(t, s)
	setup(t, s)
	for range maxFailures {
		c.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "wrong password!"})
	}
	rec := c.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": goodPassword})
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status = %d, Retry-After = %q; want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestCookieIsSecureBehindTLS(t *testing.T) {
	s := newService(t)
	c := newClient(t, s)
	forwarded := func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") }
	c.do("POST", "/api/auth/setup", map[string]string{"username": "admin", "password": goodPassword, "setup_token": s.SetupToken()}, forwarded)
	if c.cookie == nil || !c.cookie.Secure {
		t.Fatalf("cookie must be Secure when the client used HTTPS: %+v", c.cookie)
	}
}
