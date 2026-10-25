package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// loggedIn is a logged-in client of a hub handler.
type loggedIn struct {
	t       *testing.T
	handler http.Handler
	cookie  *http.Cookie
}

// newLoggedIn creates the admin of a fresh hub and keeps its cookie.
func newLoggedIn(t *testing.T, h *Hub) *loggedIn {
	t.Helper()
	s := &loggedIn{t: t, handler: NewHandler(Deps{UI: fstest.MapFS{}, Agents: h, Layouts: h, Auth: h.auth})}
	rec := s.do("POST", "/api/auth/setup",
		`{"username":"admin","password":"correct horse battery","setup_token":"`+h.auth.SetupToken()+`"}`)
	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) == 0 {
		t.Fatalf("setup: status = %d, body = %s", rec.Code, rec.Body)
	}
	s.cookie = rec.Result().Cookies()[0]
	return s
}

func (s *loggedIn) do(method, path, body string) *httptest.ResponseRecorder {
	s.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if s.cookie != nil {
		r.AddCookie(s.cookie)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, r)
	return rec
}

func TestLayoutIsStoredPerUser(t *testing.T) {
	h := startHub(t)
	s := newLoggedIn(t, h)

	var got layoutJSON
	rec := s.do("GET", "/api/layout", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || string(got.Layout) != "null" || got.UpdatedAt != nil {
		t.Fatalf("empty layout: status = %d, body = %s", rec.Code, rec.Body)
	}

	if rec := s.do("PUT", "/api/layout", `{"layout":{"widgets":[{"id":"cpu","x":0,"y":0,"w":4,"h":2}]}}`); rec.Code != http.StatusNoContent {
		t.Fatalf("PUT: status = %d, body = %s", rec.Code, rec.Body)
	}
	rec = s.do("GET", "/api/layout", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.UpdatedAt == nil {
		t.Fatalf("GET after PUT: %s (%v)", rec.Body, err)
	}
	var layout struct{ Widgets []struct{ ID string } }
	if err := json.Unmarshal(got.Layout, &layout); err != nil || len(layout.Widgets) != 1 || layout.Widgets[0].ID != "cpu" {
		t.Fatalf("layout not stored as sent: %s", got.Layout)
	}

	// Saving again replaces it.
	s.do("PUT", "/api/layout", `{"layout":[]}`)
	rec = s.do("GET", "/api/layout", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if string(got.Layout) != "[]" {
		t.Fatalf("layout after the second PUT = %s, want []", got.Layout)
	}
}

func TestLayoutValidation(t *testing.T) {
	h := startHub(t)
	s := newLoggedIn(t, h)

	for name, body := range map[string]string{
		"not JSON":      `nope`,
		"missing field": `{}`,
		"null layout":   `{"layout":null}`,
		"unknown field": `{"layout":{},"extra":1}`,
	} {
		if rec := s.do("PUT", "/api/layout", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
	big := `{"layout":"` + strings.Repeat("x", maxLayoutBytes) + `"}`
	if rec := s.do("PUT", "/api/layout", big); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized layout: status = %d, want 413", rec.Code)
	}

	// PUT must be JSON from the same origin, like the auth POSTs.
	r := httptest.NewRequest("PUT", "/api/layout", strings.NewReader(`{"layout":{}}`))
	r.AddCookie(s.cookie)
	r.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("non-JSON PUT: status = %d, want 415", rec.Code)
	}
	r = httptest.NewRequest("PUT", "/api/layout", strings.NewReader(`{"layout":{}}`))
	r.AddCookie(s.cookie)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	s.handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin PUT: status = %d, want 403", rec.Code)
	}
}

func TestLayoutNeedsALogin(t *testing.T) {
	h := startHub(t)
	s := newLoggedIn(t, h)
	s.cookie = nil
	for _, m := range []string{"GET", "PUT"} {
		if rec := s.do(m, "/api/layout", `{"layout":{}}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a login: status = %d, want 401", m, rec.Code)
		}
	}
}

func TestSingleAgentEndpoint(t *testing.T) {
	h := startHub(t)
	_, _ = register(t, h, "a1")
	waitFor(t, "agent", func() bool { return len(h.Agents()) == 1 })
	s := newLoggedIn(t, h)

	rec := s.do("GET", "/api/agents/a1", "")
	var a agentJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil || rec.Code != http.StatusOK || a.ID != "a1" || a.Status != statusConnected {
		t.Fatalf("known agent: status = %d, body = %s", rec.Code, rec.Body)
	}
	if rec := s.do("GET", "/api/agents/nobody", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown agent: status = %d, want 404", rec.Code)
	}
}
