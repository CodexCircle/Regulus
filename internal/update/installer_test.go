package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// installFixture is a data directory with a release staged in it, a "binary"
// that is a text file holding its version, and a runner that treats such files
// as programs.
type installFixture struct {
	in       *Installer
	dataDir  string
	binary   string
	restarts int
	// healthy decides whether a running version answers the probe.
	healthy func(version string) bool
	verify  *fakeVerifier
}

func tarball(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func newInstallFixture(t *testing.T, installed, staged string, archiveEntries map[string]string) *installFixture {
	t.Helper()
	root := t.TempDir()
	f := &installFixture{dataDir: filepath.Join(root, "data"), binary: filepath.Join(root, "bin", "regulus"), healthy: func(string) bool { return true }, verify: &fakeVerifier{}}
	stateDir := filepath.Join(root, "state")
	for _, d := range []string{filepath.Join(root, "bin"), stateDir, ApplyDir(f.dataDir)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(f.binary, []byte(installed), 0o755); err != nil {
		t.Fatal(err)
	}

	v, _ := ParseVersion(staged)
	if archiveEntries == nil {
		archiveEntries = map[string]string{"LICENSE": "license", "regulus": staged}
	}
	archive := tarball(t, archiveEntries)
	name := ArchiveName(v, "linux", "amd64")
	sum := sha256.Sum256(archive)
	dir := filepath.Join(f.dataDir, "updates", staged)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(Staged{Version: staged, Tag: "v" + staged, Archive: name, Verified: VerifiedSignature, Signed: true})
	for file, data := range map[string][]byte{
		name:          archive,
		checksumsName: []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n"),
		bundleName:    []byte("{}"),
		metaName:      meta,
	} {
		if err := os.WriteFile(filepath.Join(dir, file), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	f.in = &Installer{
		DataDir: f.dataDir, StateDir: stateDir, Binary: f.binary, Unit: "regulus.service",
		Verifier: f.verify, GOOS: "linux", GOARCH: "amd64",
		HealthTimeout: 200 * time.Millisecond, Stable: 20 * time.Millisecond, Poll: 5 * time.Millisecond,
		Log: slog.New(slog.DiscardHandler),
		Now: func() time.Time { return time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC) },
	}
	f.in.Run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "systemctl" {
			if len(args) > 0 && args[0] == "restart" {
				f.restarts++
			}
			return nil, nil
		}
		if len(args) == 1 && args[0] == "version" {
			data, err := os.ReadFile(name)
			return data, err
		}
		return nil, errors.New("unexpected command " + name)
	}
	f.in.Probe = func(context.Context, time.Time) (string, error) {
		data, err := os.ReadFile(f.binary)
		if err != nil {
			return "", err
		}
		if !f.healthy(string(data)) {
			return "", errors.New("not answering")
		}
		return string(data), nil
	}
	return f
}

func (f *installFixture) request(t *testing.T, version string) {
	t.Helper()
	if err := WriteRequest(ApplyDir(f.dataDir), ApplyRequest{Version: version, RequestedBy: "admin"}); err != nil {
		t.Fatal(err)
	}
}

func (f *installFixture) installed(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.binary)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestInstallerReplacesTheBinaryAndRestarts(t *testing.T) {
	f := newInstallFixture(t, "0.1.0", "0.2.0", nil)
	f.request(t, "0.2.0")

	res, err := f.in.Apply(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeApplied || res.From != "0.1.0" || res.Version != "0.2.0" || res.RequestedBy != "admin" {
		t.Errorf("result = %+v", res)
	}
	if got := f.installed(t); got != "0.2.0" {
		t.Errorf("installed binary = %q", got)
	}
	if f.restarts != 1 {
		t.Errorf("restarts = %d", f.restarts)
	}
	// The hub is told, and the request is gone so systemd does not start again.
	if r, ok := ReadResult(ApplyDir(f.dataDir)); !ok || r.Outcome != OutcomeApplied {
		t.Errorf("written result = %+v, %v", r, ok)
	}
	if _, ok := ReadRequest(ApplyDir(f.dataDir)); ok {
		t.Error("the request was left behind")
	}
	// The signature was checked on the copy root made, with the signed names.
	if f.verify.tag != "v0.2.0" || len(f.verify.sums) == 0 {
		t.Errorf("verifier saw tag %q", f.verify.tag)
	}
	if prev, _ := os.ReadFile(filepath.Join(f.in.StateDir, "regulus.previous")); string(prev) != "0.1.0" {
		t.Errorf("previous binary = %q", prev)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.binary), ".regulus.update")); err == nil {
		t.Error("the candidate file was left next to the binary")
	}
}

func TestInstallerRollsBackAVersionThatDoesNotComeUp(t *testing.T) {
	f := newInstallFixture(t, "0.1.0", "0.2.0", nil)
	f.healthy = func(v string) bool { return v != "0.2.0" }
	f.request(t, "0.2.0")

	res, err := f.in.Apply(t.Context())
	if err == nil || res.Outcome != OutcomeRolledBack {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
	if got := f.installed(t); got != "0.1.0" {
		t.Errorf("after the rollback the binary is %q", got)
	}
	if f.restarts != 2 {
		t.Errorf("restarts = %d, want one for the update and one for the rollback", f.restarts)
	}
	if !strings.Contains(res.Message, "0.1.0 was put back") {
		t.Errorf("message = %q", res.Message)
	}
}

func TestInstallerRollsBackAVersionThatDiesAgain(t *testing.T) {
	f := newInstallFixture(t, "0.1.0", "0.2.0", nil)
	f.in.Stable = 100 * time.Millisecond
	f.in.HealthTimeout = 150 * time.Millisecond
	start := time.Now()
	// Up at first, dead soon after: a crash loop shows only after a while.
	f.healthy = func(v string) bool { return v != "0.2.0" || time.Since(start) < 40*time.Millisecond }
	f.request(t, "0.2.0")

	res, _ := f.in.Apply(t.Context())
	if res.Outcome != OutcomeRolledBack || f.installed(t) != "0.1.0" {
		t.Errorf("result = %+v, binary = %q", res, f.installed(t))
	}
}

func TestInstallerRefusesWhatItCannotVerify(t *testing.T) {
	cases := map[string]struct {
		prepare func(*testing.T, *installFixture)
		version string
		want    string
	}{
		"bad signature": {func(_ *testing.T, f *installFixture) { f.verify.err = errors.New("signature is not valid") }, "0.2.0", "signature is not valid"},
		"changed archive": {func(t *testing.T, f *installFixture) {
			p := filepath.Join(f.dataDir, "updates", "0.2.0", ArchiveName(Version{}, "", ""))
			_ = p
			entries, _ := os.ReadDir(filepath.Join(f.dataDir, "updates", "0.2.0"))
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".tar.gz") {
					if err := os.WriteFile(filepath.Join(f.dataDir, "updates", "0.2.0", e.Name()), []byte("swapped"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
		}, "0.2.0", "does not match the signed checksum list"},
		"not staged":             {func(*testing.T, *installFixture) {}, "0.3.0", "not staged"},
		"older version":          {func(*testing.T, *installFixture) {}, "0.1.0", "is not newer"},
		"invalid version":        {func(*testing.T, *installFixture) {}, "../../etc", "invalid version"},
		"missing the executable": {nil, "0.2.0", "holds no regulus executable"},
	}
	for name, tc := range cases {
		var entries map[string]string
		if tc.prepare == nil {
			entries = map[string]string{"README.md": "x"}
		}
		f := newInstallFixture(t, "0.1.0", "0.2.0", entries)
		if tc.prepare != nil {
			tc.prepare(t, f)
		}
		f.request(t, tc.version)
		res, err := f.in.Apply(t.Context())
		if err == nil || res.Outcome != OutcomeFailed || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: result = %+v, err = %v", name, res, err)
		}
		if got := f.installed(t); got != "0.1.0" || f.restarts != 0 {
			t.Errorf("%s: the installation was touched (binary %q, restarts %d)", name, got, f.restarts)
		}
	}
}

func TestInstallerRefusesABinaryThatReportsAnotherVersion(t *testing.T) {
	f := newInstallFixture(t, "0.1.0", "0.2.0", map[string]string{"regulus": "0.9.9"})
	f.request(t, "0.2.0")
	res, err := f.in.Apply(t.Context())
	if err == nil || res.Outcome != OutcomeFailed || f.installed(t) != "0.1.0" {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
}

func TestInstallerDoesNotUpdateADevelopmentBuild(t *testing.T) {
	f := newInstallFixture(t, "dev", "0.2.0", nil)
	f.request(t, "0.2.0")
	if res, err := f.in.Apply(t.Context()); err == nil || res.Outcome != OutcomeFailed {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
}

func TestInstallerWithoutARequestDoesNothing(t *testing.T) {
	f := newInstallFixture(t, "0.1.0", "0.2.0", nil)
	if _, err := f.in.Apply(t.Context()); !errors.Is(err, ErrNoRequest) {
		t.Errorf("err = %v", err)
	}
	if _, ok := ReadResult(ApplyDir(f.dataDir)); ok {
		t.Error("a result was written without a request")
	}
}

func TestInstallerDoesNotFollowALinkInTheStagingDirectory(t *testing.T) {
	f := newInstallFixture(t, "0.1.0", "0.2.0", nil)
	dir := filepath.Join(f.dataDir, "updates", "0.2.0")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tar.gz") {
			target := filepath.Join(t.TempDir(), "elsewhere")
			_ = os.WriteFile(target, []byte("secret"), 0o600)
			_ = os.Remove(filepath.Join(dir, e.Name()))
			if err := os.Symlink(target, filepath.Join(dir, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.request(t, "0.2.0")
	if res, err := f.in.Apply(t.Context()); err == nil || res.Outcome != OutcomeFailed {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
}

func TestAgentHealthIsConnectingAgainAfterTheRestart(t *testing.T) {
	f := newInstallFixture(t, "0.1.0", "0.2.0", nil)
	in := f.in
	in.Agent = true
	in.Probe = nil
	in.defaults()
	restart := time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)

	if _, err := in.Probe(t.Context(), restart); err == nil {
		t.Error("an agent that never wrote its file is healthy")
	}
	// What the agent wrote before the restart is not proof.
	_ = WriteConnected(f.dataDir, Connected{Version: "0.2.0", At: restart.Add(-time.Minute)})
	if _, err := in.Probe(t.Context(), restart); err == nil {
		t.Error("an old connection counts")
	}
	_ = WriteConnected(f.dataDir, Connected{Version: "0.2.0", At: restart.Add(time.Second)})
	if v, err := in.Probe(t.Context(), restart); err != nil || v != "0.2.0" {
		t.Errorf("probe = %q, %v", v, err)
	}
}
