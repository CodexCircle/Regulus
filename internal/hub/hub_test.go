package hub

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/CodexCircle/Regulus/internal/config"
)

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealthz(t *testing.T) {
	rec := get(t, NewHandler(Deps{UI: fstest.MapFS{}}), "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestServesDashboard(t *testing.T) {
	ui := fstest.MapFS{"index.html": {Data: []byte("<h1>Regulus</h1>")}}
	rec := get(t, NewHandler(Deps{UI: ui}), "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestDashboardMissing(t *testing.T) {
	rec := get(t, NewHandler(Deps{UI: fstest.MapFS{}}), "/")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHubServesAndShutsDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	useTestDatabase(t, &cfg)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	h, err := New(ctx, cfg, log, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx, ln) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v after cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after cancellation")
	}
}

func TestDashboardOnlyAnswersGet(t *testing.T) {
	ui := fstest.MapFS{"index.html": {Data: []byte("<h1>Regulus</h1>")}}
	rec := httptest.NewRecorder()
	NewHandler(Deps{UI: ui}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") == "" {
		t.Fatalf("status = %d, Allow = %q; want 405 with Allow", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestHubKeepsItsSecretOutsideTheDatabase(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	useTestDatabase(t, &cfg)
	h, err := New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	fi, err := os.Stat(filepath.Join(cfg.DataDir, "secret.key"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("secret.key: %v, %v", fi, err)
	}
}

func TestDatabaseStatusIsPublicAndCarriesOnlyABoolean(t *testing.T) {
	problem := ""
	// A real Auth would answer 401 to everything under /api/ without a session;
	// this endpoint must not, because the database that holds the sessions is
	// the thing that may be down.
	h := NewHandler(Deps{UI: fstest.MapFS{}, DatabaseProblem: func() string { return problem }})

	for _, tc := range []struct {
		problem string
		want    string
	}{
		{"", `{"ok":true}`},
		{"connection refused to db.internal:5432 as regulus", `{"ok":false}`},
	} {
		problem = tc.problem
		rec := get(t, h, "/api/database")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != tc.want {
			t.Errorf("problem %q: body = %s, want %s (the reason must never be sent)", tc.problem, got, tc.want)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", cc)
		}
	}

	// No DatabaseProblem function (SQLite, tests) means no problem.
	if got := strings.TrimSpace(get(t, NewHandler(Deps{UI: fstest.MapFS{}}), "/api/database").Body.String()); got != `{"ok":true}` {
		t.Errorf("without a function: %s", got)
	}
}

// /healthz stays "ok" while the database is down: the installer and the update
// helper wait for it, and a hub that outlives the database must not look dead.
func TestHealthzIgnoresTheDatabase(t *testing.T) {
	h := NewHandler(Deps{UI: fstest.MapFS{}, DatabaseProblem: func() string { return "down" }})
	if rec := get(t, h, "/healthz"); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "ok" {
		t.Fatalf("healthz = %d %q", rec.Code, rec.Body.String())
	}
}

// A running hub reports the state it last saw of its database through
// /api/database, with no login.
func TestRunningHubReportsItsDatabaseState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	useTestDatabase(t, &cfg)
	h, err := New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = h.Serve(ctx, ln) }()

	read := func() string {
		resp, err := http.Get("http://" + ln.Addr().String() + "/api/database")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return strings.TrimSpace(string(b))
	}
	if got := read(); got != `{"ok":true}` {
		t.Fatalf("healthy: %s", got)
	}
	h.dbState.set("connection refused")
	if got := read(); got != `{"ok":false}` {
		t.Fatalf("with a problem: %s", got)
	}
	h.dbState.set("")
	if got := read(); got != `{"ok":true}` {
		t.Fatalf("after recovery: %s", got)
	}
}
