package hub

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealthz(t *testing.T) {
	rec := get(t, NewHandler(fstest.MapFS{}), "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestServesDashboard(t *testing.T) {
	ui := fstest.MapFS{"index.html": {Data: []byte("<h1>Regulus</h1>")}}
	rec := get(t, NewHandler(ui), "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestDashboardMissing(t *testing.T) {
	rec := get(t, NewHandler(fstest.MapFS{}), "/")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
