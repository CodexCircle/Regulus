package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/CodexCircle/Regulus/internal/auth"
	"github.com/CodexCircle/Regulus/internal/config"
	"github.com/CodexCircle/Regulus/internal/update"
)

// releaseServer serves one release of the core and a Source that lists it.
type releaseServer struct {
	update.Source
	archive string
}

func newReleaseServer(t *testing.T, version string) releaseServer {
	t.Helper()
	v, err := update.ParseVersion(version)
	if err != nil {
		t.Fatal(err)
	}
	// The hub asks for the archive of the platform it runs on.
	name := update.ArchiveName(v, runtime.GOOS, runtime.GOARCH)
	files := map[string][]byte{name: []byte("archive of " + version)}
	sum := sha256.Sum256(files[name])
	files["checksums.txt"] = []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	files["checksums.txt.sigstore.json"] = []byte("{}")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	rel := update.Release{Version: v, Tag: "v" + version, URL: "https://example.test/releases/v" + version, Notes: "What's new"}
	for n, data := range files {
		rel.Assets = append(rel.Assets, update.Asset{Name: n, URL: srv.URL + "/" + n, Size: int64(len(data))})
	}
	return releaseServer{Source: staticSource{rel}, archive: name}
}

// acceptAll stands in for the Sigstore check, which has its own tests.
type acceptAll struct{}

func (acceptAll) Verify(context.Context, string, []byte, []byte) error { return nil }

type staticSource []update.Release

func (s staticSource) Releases(context.Context) ([]update.Release, error) { return s, nil }

type failingSource struct{ err error }

func (f failingSource) Releases(context.Context) ([]update.Release, error) { return nil, f.err }

// newUpdateHub makes a hub, without serving, that believes it runs the given
// version and finds its releases in src.
func newUpdateHub(t *testing.T, version string, src update.Source, mutate ...func(*config.Config)) *Hub {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.BuiltinAgent = false
	for _, m := range mutate {
		m(&cfg)
	}
	h, err := New(t.Context(), cfg, quiet(), fstest.MapFS{}, WithVersion(version), WithUpdateSource(src), WithUpdateVerifier(acceptAll{}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func decodeStatus(t *testing.T, rec *httptest.ResponseRecorder) update.Status {
	t.Helper()
	var st update.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("status %d, body %q: %v", rec.Code, rec.Body, err)
	}
	return st
}

func TestUpdatesAPIFollowsTheFlow(t *testing.T) {
	rs := newReleaseServer(t, "0.2.0")
	h := newUpdateHub(t, "0.1.0", rs)
	admin := newAPI(t, h, auth.RoleAdmin)

	rec := admin.do("GET", "/api/updates", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET: status = %d, body = %s", rec.Code, rec.Body)
	}
	st := decodeStatus(t, rec)
	if st.CurrentVersion != "0.1.0" || st.Mode != update.ModeDownload || st.Channel != update.Stable || !st.ChecksEnabled || st.UpdateAvailable {
		t.Fatalf("initial status = %+v", st)
	}

	// Change the mode and back (download is the default; saving it unchanged
	// is not a change and is not audited).
	for _, mode := range []update.Mode{update.ModeManual, update.ModeDownload} {
		rec = admin.do("PUT", "/api/updates/settings", map[string]string{"mode": string(mode), "channel": "stable"})
		if st := decodeStatus(t, rec); rec.Code != http.StatusOK || st.Mode != mode {
			t.Fatalf("PUT settings %s: status = %d, body = %s", mode, rec.Code, rec.Body)
		}
	}

	rec = admin.do("POST", "/api/updates/check", struct{}{})
	st = decodeStatus(t, rec)
	if rec.Code != http.StatusOK || !st.UpdateAvailable || st.Latest == nil || st.Latest.Version != "0.2.0" || st.Latest.Notes != "What's new" {
		t.Fatalf("check: status = %d, body = %s", rec.Code, rec.Body)
	}
	if rec := admin.do("POST", "/api/updates/check", struct{}{}); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("second check: status = %d, Retry-After = %q", rec.Code, rec.Header().Get("Retry-After"))
	}

	if rec := admin.do("POST", "/api/updates/download", struct{}{}); rec.Code != http.StatusAccepted {
		t.Fatalf("download: status = %d, body = %s", rec.Code, rec.Body)
	}
	waitFor(t, "the download", func() bool {
		st := decodeStatus(t, admin.do("GET", "/api/updates", nil))
		return st.Staged != nil && !st.Downloading
	})
	st = decodeStatus(t, admin.do("GET", "/api/updates", nil))
	if st.Staged.Version != "0.2.0" || st.Staged.Verified != update.VerifiedSignature || st.DownloadError != "" {
		t.Errorf("status after the download = %+v", st)
	}

	// Both changes are in the audit log, under the admin's name.
	entries, err := h.auth.Audit(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, e := range entries {
		seen[e.Event] = e.Actor
	}
	if seen[update.EventSettings] != "admin" || seen[update.EventDownloaded] != "admin" {
		t.Errorf("audit events = %v", seen)
	}
}

func TestUpdatesAPIRefusals(t *testing.T) {
	rs := newReleaseServer(t, "0.2.0")
	h := newUpdateHub(t, "0.1.0", rs)
	admin := newAPI(t, h, auth.RoleAdmin)

	for name, tc := range map[string]struct {
		method, path string
		body         any
		want         int
	}{
		"automatic mode":         {"PUT", "/api/updates/settings", map[string]string{"mode": "automatic", "channel": "stable"}, http.StatusBadRequest},
		"unknown mode":           {"PUT", "/api/updates/settings", map[string]string{"mode": "whenever", "channel": "stable"}, http.StatusBadRequest},
		"unknown channel":        {"PUT", "/api/updates/settings", map[string]string{"mode": "manual", "channel": "nightly"}, http.StatusBadRequest},
		"unknown field":          {"PUT", "/api/updates/settings", map[string]string{"mode": "manual", "channel": "stable", "extra": "x"}, http.StatusBadRequest},
		"download before check":  {"POST", "/api/updates/download", struct{}{}, http.StatusConflict},
		"check with a body":      {"POST", "/api/updates/check", map[string]string{"x": "y"}, http.StatusBadRequest},
		"wrong method on status": {"POST", "/api/updates", struct{}{}, http.StatusMethodNotAllowed},
	} {
		if rec := admin.do(tc.method, tc.path, tc.body); rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d (%s)", name, rec.Code, tc.want, rec.Body)
		}
	}
}

func TestUpdatesAPIWhenChecksAreOff(t *testing.T) {
	h := newUpdateHub(t, "0.1.0", failingSource{errors.New("must not be asked")}, func(c *config.Config) { c.Updates.Check = false })
	admin := newAPI(t, h, auth.RoleAdmin)

	if st := decodeStatus(t, admin.do("GET", "/api/updates", nil)); st.ChecksEnabled {
		t.Error("status says checks are on")
	}
	for _, path := range []string{"/api/updates/check", "/api/updates/download"} {
		if rec := admin.do("POST", path, struct{}{}); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "updates.check") {
			t.Errorf("%s: status = %d, body = %s", path, rec.Code, rec.Body)
		}
	}
}

func TestUpdatesAPIWhenTheReleaseServerFails(t *testing.T) {
	h := newUpdateHub(t, "0.1.0", failingSource{errors.New("cannot reach the release server: no route")})
	admin := newAPI(t, h, auth.RoleAdmin)

	rec := admin.do("POST", "/api/updates/check", struct{}{})
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "no route") {
		t.Fatalf("check: status = %d, body = %s", rec.Code, rec.Body)
	}
	// The failure is shown with the status too.
	if st := decodeStatus(t, admin.do("GET", "/api/updates", nil)); !strings.Contains(st.CheckError, "no route") || st.CheckedAt == nil {
		t.Errorf("status = %+v", st)
	}
}

func TestUpdatesAPIWithoutAnUpdateControlAnswers404(t *testing.T) {
	h := newUpdateHub(t, "0.1.0", staticSource{})
	admin := newAPI(t, h, auth.RoleAdmin)
	bare := &api{t: t, cookie: admin.cookie, handler: NewHandler(Deps{UI: fstest.MapFS{}, Log: quiet(), Auth: h.auth})}
	if rec := bare.do("GET", "/api/updates", nil); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestUpdateErrorsMapToStatusCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{update.ErrTooSoon, http.StatusTooManyRequests},
		{update.ErrBusy, http.StatusConflict},
		{fmt.Errorf("wrapped: %w", update.ErrNothingToFetch), http.StatusConflict},
		{update.ErrNotReleaseBuild, http.StatusConflict},
		{update.ErrApplyDisabled, http.StatusConflict},
		{update.ErrNotVerified, http.StatusConflict},
		{fmt.Errorf("%w: x", update.ErrInvalidSettings), http.StatusBadRequest},
		{errors.New("disk on fire"), http.StatusInternalServerError},
	} {
		rec := httptest.NewRecorder()
		updateHandlers{}.reply(rec, http.StatusOK, update.Status{}, tc.err)
		if rec.Code != tc.want {
			t.Errorf("%v: status = %d, want %d", tc.err, rec.Code, tc.want)
		}
		if tc.want == http.StatusInternalServerError && strings.Contains(rec.Body.String(), "disk") {
			t.Errorf("an internal error leaked: %s", rec.Body)
		}
	}
}

// Without a test double the hub checks the Sigstore signature, so a release
// whose signature file is not a valid bundle is never kept.
func TestHubRefusesAReleaseWithoutAValidSignature(t *testing.T) {
	rs := newReleaseServer(t, "0.2.0")
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.BuiltinAgent = false
	h, err := New(t.Context(), cfg, quiet(), fstest.MapFS{}, WithVersion("0.1.0"), WithUpdateSource(rs.Source))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	admin := newAPI(t, h, auth.RoleAdmin)

	admin.do("POST", "/api/updates/check", struct{}{})
	if rec := admin.do("POST", "/api/updates/download", struct{}{}); rec.Code != http.StatusAccepted {
		t.Fatalf("download: %d %s", rec.Code, rec.Body)
	}
	waitFor(t, "the download to fail", func() bool {
		return decodeStatus(t, admin.do("GET", "/api/updates", nil)).DownloadError != ""
	})
	st := decodeStatus(t, admin.do("GET", "/api/updates", nil))
	if st.Staged != nil || !strings.Contains(st.DownloadError, "not a Sigstore bundle") {
		t.Errorf("status = %+v", st)
	}
}

func TestUpdatesAPIApplyLeavesARequestForTheHelper(t *testing.T) {
	rs := newReleaseServer(t, "0.2.0")
	h := newUpdateHub(t, "0.1.0", rs, func(c *config.Config) { c.Updates.Apply = true })
	admin := newAPI(t, h, auth.RoleAdmin)

	if rec := admin.do("POST", "/api/updates/apply", struct{}{}); rec.Code != http.StatusConflict {
		t.Errorf("apply with nothing downloaded: status = %d, body = %s", rec.Code, rec.Body)
	}
	admin.do("POST", "/api/updates/check", struct{}{})
	admin.do("POST", "/api/updates/download", struct{}{})
	waitFor(t, "the download", func() bool {
		st := decodeStatus(t, admin.do("GET", "/api/updates", nil))
		return st.Staged != nil && !st.Downloading
	})
	if st := decodeStatus(t, admin.do("GET", "/api/updates", nil)); !st.ApplyEnabled || !st.ApplyAvailable {
		t.Fatalf("status = %+v", st)
	}

	rec := admin.do("POST", "/api/updates/apply", struct{}{})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("apply: status = %d, body = %s", rec.Code, rec.Body)
	}
	if st := decodeStatus(t, rec); !st.Applying || st.ApplyAvailable {
		t.Errorf("status after apply = %+v", st)
	}
	req, ok := update.ReadRequest(update.ApplyDir(h.cfg.DataDir))
	if !ok || req.Version != "0.2.0" || req.RequestedBy != "admin" {
		t.Errorf("request = %+v, %v", req, ok)
	}
	if rec := admin.do("POST", "/api/updates/apply", struct{}{}); rec.Code != http.StatusConflict {
		t.Errorf("second apply: status = %d", rec.Code)
	}
}

func TestUpdatesAPIApplyIsOffByDefault(t *testing.T) {
	h := newUpdateHub(t, "0.1.0", newReleaseServer(t, "0.2.0"))
	admin := newAPI(t, h, auth.RoleAdmin)
	if rec := admin.do("POST", "/api/updates/apply", struct{}{}); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "updates.apply") {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
	}
	rec := admin.do("PUT", "/api/updates/settings", map[string]string{"mode": "automatic", "channel": "stable", "window_start": "03:00", "window_end": "05:00"})
	if rec.Code != http.StatusConflict {
		t.Errorf("automatic without the helper: status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestOperatorsCannotApplyUpdates(t *testing.T) {
	h := newUpdateHub(t, "0.1.0", newReleaseServer(t, "0.2.0"), func(c *config.Config) { c.Updates.Apply = true })
	op := newAPI(t, h, auth.RoleOperator)
	if rec := op.do("POST", "/api/updates/apply", struct{}{}); rec.Code != http.StatusForbidden {
		t.Errorf("status = %d", rec.Code)
	}
}
