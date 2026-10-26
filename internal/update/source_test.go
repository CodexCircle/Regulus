package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const releasesJSON = `[
 {"tag_name":"v0.3.0-beta.1","prerelease":true,"html_url":"https://example.test/r/beta","body":"beta notes",
  "published_at":"2026-10-20T10:00:00Z",
  "assets":[{"name":"checksums.txt","browser_download_url":"https://example.test/a/sums","size":10}]},
 {"tag_name":"v0.2.0","prerelease":false,"html_url":"https://example.test/r/2","body":"notes",
  "published_at":"2026-10-10T10:00:00Z",
  "assets":[{"name":"checksums.txt","browser_download_url":"https://example.test/a/sums2","size":10},
            {"name":"evil","browser_download_url":"http://example.test/plain","size":1}]},
 {"tag_name":"v0.9.0","draft":true,"prerelease":false},
 {"tag_name":"plugin/docker/v1.0.0","prerelease":false},
 {"tag_name":"v0.4.0","prerelease":true},
 {"tag_name":"not-a-version","prerelease":false},
 {"tag_name":"v0.1.0","prerelease":false,"assets":[]}
]`

func newGitHub(t *testing.T, h http.HandlerFunc) *GitHubSource {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &GitHubSource{Repo: "o/r", BaseURL: srv.URL, UserAgent: "regulus-test", Client: srv.Client()}
}

func TestGitHubSourceListsTheCoreReleases(t *testing.T) {
	var path, agent string
	g := newGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		path, agent = r.URL.Path, r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(releasesJSON))
	})
	got, err := g.Releases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if path != "/repos/o/r/releases" || agent != "regulus-test" {
		t.Errorf("asked %q with agent %q", path, agent)
	}
	var tags []string
	for _, r := range got {
		tags = append(tags, r.Tag)
	}
	// Drafts, plugin tags, non-versions and a release whose pre-release flag
	// disagrees with its tag (v0.4.0) are left out.
	if want := "v0.3.0-beta.1 v0.2.0 v0.1.0"; strings.Join(tags, " ") != want {
		t.Fatalf("releases = %v, want %s", tags, want)
	}
	// Only HTTPS asset addresses are kept (this server is plain HTTP in tests
	// but the addresses in the data are not its own).
	if a, ok := got[1].Asset("evil"); ok {
		t.Errorf("kept an http asset: %+v", a)
	}
	if a, ok := got[1].Asset("checksums.txt"); !ok || a.URL != "https://example.test/a/sums2" {
		t.Errorf("checksums.txt asset = %+v, %v", a, ok)
	}
	if got[1].Notes != "notes" || got[1].URL != "https://example.test/r/2" {
		t.Errorf("release = %+v", got[1])
	}
}

func TestGitHubSourceReusesTheAnswerWhenNotModified(t *testing.T) {
	var calls, conditional atomic.Int32
	g := newGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("If-None-Match") == `"v1"` {
			conditional.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(releasesJSON))
	})
	first, err := g.Releases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.Releases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || conditional.Load() != 1 || len(second) != len(first) {
		t.Fatalf("calls = %d, conditional = %d, releases = %d/%d", calls.Load(), conditional.Load(), len(first), len(second))
	}
}

func TestGitHubSourceErrors(t *testing.T) {
	cases := map[string]struct {
		handler http.HandlerFunc
		want    string
	}{
		"rate limit": {func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "1793000000")
			w.WriteHeader(http.StatusForbidden)
		}, "request limit is used up until 2026-"},
		"other 403": {func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }, "refused the request"},
		"server":    {func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }, "502"},
		"not json":  {func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) }, "cannot read"},
	}
	for name, tc := range cases {
		g := newGitHub(t, tc.handler)
		_, err := g.Releases(context.Background())
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want it to contain %q", name, err, tc.want)
		}
	}
}

func TestLatestByChannel(t *testing.T) {
	mk := func(vs ...string) []Release {
		var out []Release
		for _, s := range vs {
			v, err := ParseVersion(s)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, Release{Version: v, Tag: "v" + s})
		}
		return out
	}
	releases := mk("0.2.0", "0.3.0-beta.1", "0.1.0", "0.3.0-beta.2")

	if r, ok := Latest(releases, Stable); !ok || r.Tag != "v0.2.0" {
		t.Errorf("stable = %v, %v; want v0.2.0", r.Tag, ok)
	}
	if r, ok := Latest(releases, Beta); !ok || r.Tag != "v0.3.0-beta.2" {
		t.Errorf("beta = %v, %v; want v0.3.0-beta.2", r.Tag, ok)
	}
	// When the release catches up with the beta, the beta channel moves on.
	if r, _ := Latest(append(releases, mk("0.3.0")...), Beta); r.Tag != "v0.3.0" {
		t.Errorf("beta after 0.3.0 = %v, want v0.3.0", r.Tag)
	}
	if _, ok := Latest(mk("0.3.0-beta.1"), Stable); ok {
		t.Error("stable found a release among pre-releases only")
	}
	if _, ok := Latest(nil, Beta); ok {
		t.Error("found a release in an empty list")
	}
}

func TestClipKeepsCharactersWhole(t *testing.T) {
	if got := clip("abcdef", 3); got != "abc…" {
		t.Errorf("clip = %q", got)
	}
	// "è" is two bytes; cutting after the first byte must drop it entirely.
	if got := clip("abè", 3); got != "ab…" {
		t.Errorf("clip = %q", got)
	}
	if got := clip("short", 10); got != "short" {
		t.Errorf("clip = %q", got)
	}
}
