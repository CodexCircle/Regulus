package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRelease serves the files of a release and describes it.
type fakeRelease struct {
	srv     *httptest.Server
	files   map[string][]byte
	version string
}

func newFakeRelease(t *testing.T, version string) *fakeRelease {
	t.Helper()
	f := &fakeRelease{files: map[string][]byte{}, version: version}
	v, _ := ParseVersion(version)
	archive := ArchiveName(v, "linux", "amd64")
	body := []byte("pretend this is a tarball of " + version)
	sum := sha256.Sum256(body)
	f.files[archive] = body
	f.files[checksumsName] = []byte(fmt.Sprintf("%s  %s\n%s  other_file.deb\n", hex.EncodeToString(sum[:]), archive, strings.Repeat("0", 64)))
	f.files[bundleName] = []byte(`{"mediaType":"application/vnd.dev.sigstore.bundle"}`)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := f.files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRelease) release() Release {
	v, _ := ParseVersion(f.version)
	r := Release{Version: v, Tag: "v" + f.version}
	for name, data := range f.files {
		r.Assets = append(r.Assets, Asset{Name: name, URL: f.srv.URL + "/" + name, Size: int64(len(data))})
	}
	return r
}

func newStager(t *testing.T) *Stager {
	t.Helper()
	return &Stager{Dir: filepath.Join(t.TempDir(), "updates"), Now: func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }}
}

func TestStageKeepsAVerifiedDownload(t *testing.T) {
	rel := newFakeRelease(t, "0.2.0")
	s := newStager(t)

	st, err := s.Stage(t.Context(), rel.release(), "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "0.2.0" || st.Archive != "regulus_0.2.0_linux_amd64.tar.gz" || st.Verified != VerifiedChecksum || !st.Signed {
		t.Errorf("staged = %+v", st)
	}
	if !st.StagedAt.Equal(time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("staged at %v", st.StagedAt)
	}
	got, err := os.ReadFile(st.Path(s.Dir, st.Archive))
	if err != nil || string(got) != string(rel.files[st.Archive]) {
		t.Fatalf("archive on disk = %q, %v", got, err)
	}
	for _, name := range []string{checksumsName, bundleName, metaName} {
		if _, err := os.Stat(st.Path(s.Dir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	if cur, ok := s.Current(); !ok || cur != st {
		t.Errorf("Current() = %+v, %v; want %+v", cur, ok, st)
	}
	if err := s.Verify(st); err != nil {
		t.Errorf("Verify: %v", err)
	}
	// Owner only: the cache holds what will run as the service.
	info, _ := os.Stat(st.Path(s.Dir, st.Archive))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("archive mode = %v", info.Mode().Perm())
	}
}

func TestStageRefusesAnArchiveThatDoesNotMatch(t *testing.T) {
	rel := newFakeRelease(t, "0.2.0")
	rel.files["regulus_0.2.0_linux_amd64.tar.gz"] = []byte("tampered")
	s := newStager(t)

	_, err := s.Stage(t.Context(), rel.release(), "linux", "amd64")
	if err == nil || !strings.Contains(err.Error(), "does not match its checksum") {
		t.Fatalf("error = %v", err)
	}
	if _, ok := s.Current(); ok {
		t.Error("a mismatching download was kept")
	}
	if entries, _ := os.ReadDir(s.Dir); len(entries) != 0 {
		t.Errorf("left behind: %v", entries)
	}
}

func TestStageNeedsTheFilesOfTheRelease(t *testing.T) {
	cases := map[string]struct {
		change func(*fakeRelease)
		want   string
	}{
		"no archive for the platform": {func(f *fakeRelease) { delete(f.files, "regulus_0.2.0_linux_amd64.tar.gz") }, "has no regulus_0.2.0_linux_amd64.tar.gz"},
		"no checksums":                {func(f *fakeRelease) { delete(f.files, checksumsName) }, "has no checksums.txt"},
		"archive not listed":          {func(f *fakeRelease) { f.files[checksumsName] = []byte(strings.Repeat("0", 64) + "  other.tar.gz\n") }, "does not list"},
		"malformed checksum":          {func(f *fakeRelease) { f.files[checksumsName] = []byte("zz  regulus_0.2.0_linux_amd64.tar.gz\n") }, "malformed"},
	}
	for name, tc := range cases {
		rel := newFakeRelease(t, "0.2.0")
		tc.change(rel)
		_, err := newStager(t).Stage(t.Context(), rel.release(), "linux", "amd64")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want it to contain %q", name, err, tc.want)
		}
	}
}

func TestStageWithoutASignatureBundleIsUnsigned(t *testing.T) {
	rel := newFakeRelease(t, "0.2.0")
	delete(rel.files, bundleName)
	st, err := newStager(t).Stage(t.Context(), rel.release(), "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if st.Signed {
		t.Error("a release without a signature bundle counts as signed")
	}
}

func TestStageKeepsOnlyTheNewestVersion(t *testing.T) {
	s := newStager(t)
	for _, v := range []string{"0.2.0", "0.3.0"} {
		if _, err := s.Stage(t.Context(), newFakeRelease(t, v).release(), "linux", "amd64"); err != nil {
			t.Fatal(err)
		}
	}
	// A download that was interrupted leaves a work directory behind.
	if err := os.MkdirAll(filepath.Join(s.Dir, ".stage-old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stage(t.Context(), newFakeRelease(t, "0.3.0").release(), "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(s.Dir)
	if len(entries) != 1 || entries[0].Name() != "0.3.0" {
		t.Errorf("cache holds %v, want only 0.3.0", entries)
	}
}

func TestStageStopsAtTheSizeLimit(t *testing.T) {
	rel := newFakeRelease(t, "0.2.0")
	r := rel.release()
	for i := range r.Assets {
		if r.Assets[i].Name == "regulus_0.2.0_linux_amd64.tar.gz" {
			r.Assets[i].Size = 3 // the release says 3 bytes, the server sends more
		}
	}
	_, err := newStager(t).Stage(t.Context(), r, "linux", "amd64")
	if err == nil || !strings.Contains(err.Error(), "but the release says 3") {
		t.Fatalf("error = %v", err)
	}
}

func TestVerifyNoticesAChangedFile(t *testing.T) {
	s := newStager(t)
	st, err := s.Stage(t.Context(), newFakeRelease(t, "0.2.0").release(), "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path(s.Dir, st.Archive), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(st); err == nil {
		t.Error("Verify accepted a changed file")
	}
	// The size changed too, so the cache no longer offers it.
	if _, ok := s.Current(); ok {
		t.Error("Current() offers a staged release whose archive changed size")
	}
}

func TestCurrentIgnoresStrangeEntries(t *testing.T) {
	s := newStager(t)
	if _, ok := s.Current(); ok {
		t.Error("an absent cache has a staged release")
	}
	_ = os.MkdirAll(filepath.Join(s.Dir, "9.9.9"), 0o700) // no staged.json
	_ = os.MkdirAll(filepath.Join(s.Dir, "not-a-version"), 0o700)
	_ = os.WriteFile(filepath.Join(s.Dir, "stray-file"), nil, 0o600)
	if _, ok := s.Current(); ok {
		t.Error("strange entries counted as a staged release")
	}
}

func TestRedirectToPlainHTTPIsRefused(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) }))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusFound)
	}))
	defer secure.Close()

	s := &Stager{Dir: t.TempDir(), Client: secure.Client()}
	if _, err := s.fetchSmall(t.Context(), secure.URL); err == nil || !strings.Contains(err.Error(), "refusing a redirect") {
		t.Fatalf("error = %v", err)
	}
}

type fakeVerifier struct {
	err    error
	tag    string
	sums   []byte
	bundle []byte
}

func (f *fakeVerifier) Verify(_ context.Context, tag string, checksums, bundle []byte) error {
	f.tag, f.sums, f.bundle = tag, checksums, bundle
	return f.err
}

func TestStageWithAVerifierRecordsTheSignature(t *testing.T) {
	rel := newFakeRelease(t, "0.2.0")
	s := newStager(t)
	fv := &fakeVerifier{}
	s.Verifier = fv

	st, err := s.Stage(t.Context(), rel.release(), "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if st.Verified != VerifiedSignature || !st.Signed {
		t.Errorf("staged = %+v", st)
	}
	if fv.tag != "v0.2.0" || string(fv.sums) != string(rel.files[checksumsName]) || string(fv.bundle) != string(rel.files[bundleName]) {
		t.Errorf("the verifier was given tag %q, checksums %q, bundle %q", fv.tag, fv.sums, fv.bundle)
	}
}

func TestStageRefusesWhatTheVerifierRefusesBeforeDownloadingTheArchive(t *testing.T) {
	rel := newFakeRelease(t, "0.2.0")
	var archiveRequests int
	inner := rel.srv.Config.Handler
	rel.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			archiveRequests++
		}
		inner.ServeHTTP(w, r)
	})
	s := newStager(t)
	s.Verifier = &fakeVerifier{err: errors.New("signature is not valid")}

	_, err := s.Stage(t.Context(), rel.release(), "linux", "amd64")
	if err == nil || !strings.Contains(err.Error(), "signature is not valid") {
		t.Fatalf("error = %v", err)
	}
	if archiveRequests != 0 {
		t.Errorf("the archive was downloaded %d times although the signature was refused", archiveRequests)
	}
	if _, ok := s.Current(); ok {
		t.Error("a release with a bad signature was kept")
	}
}

func TestStageWithAVerifierNeedsTheSignatureFile(t *testing.T) {
	rel := newFakeRelease(t, "0.2.0")
	delete(rel.files, bundleName)
	s := newStager(t)
	s.Verifier = &fakeVerifier{}
	_, err := s.Stage(t.Context(), rel.release(), "linux", "amd64")
	if err == nil || !strings.Contains(err.Error(), "origin cannot be verified") {
		t.Fatalf("error = %v", err)
	}
}
