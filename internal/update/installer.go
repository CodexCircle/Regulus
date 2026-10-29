// installer.go is the root helper behind `regulus update-apply`: Installer.Apply
// re-verifies a staged release, replaces the executable, restarts the service,
// watches that the new version is healthy, and rolls back if it is not. Reading
// order: Installer and defaults, Apply, fetchVerified, restartAndWatch,
// rollback, then the safe file helpers at the end. The request and result files
// it exchanges with the hub are in apply.go. See docs/code-map.md.

package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/CodexCircle/Regulus/internal/localstatus"
)

const maxBinary = 256 << 20

// Runner runs a command and returns what it printed.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// Installer is the helper that runs as root. Nothing it is given by the hub's
// user is trusted: the request only says which version, and the archive is
// checked again here, signature included, before a byte of it is installed.
type Installer struct {
	// DataDir is the hub's data directory; the staged release and the request
	// are in it.
	DataDir string
	// StateDir belongs to root: the copy of the previous binary and the cache
	// of the signature trust root.
	StateDir string
	// Binary is the executable to replace, for example /usr/bin/regulus.
	Binary string
	// Unit is the service to restart, for example regulus.service.
	Unit string

	Verifier Verifier
	Run      Runner
	// Agent is true on a server that runs the agent: its health is that it
	// connected to the hub again (see WriteConnected), not the hub's local
	// status socket.
	Agent bool
	// Probe returns the version of the service that is running, or an error
	// while it is not up. Only a process that started at or after since counts:
	// the one that was there before the restart does not.
	Probe func(ctx context.Context, since time.Time) (string, error)

	// HealthTimeout is how long the new version has to come up; it must then
	// keep running for Stable. Poll is how often it is looked at.
	HealthTimeout, Stable, Poll time.Duration

	GOOS, GOARCH string
	Now          func() time.Time
	Log          *slog.Logger
}

func (in *Installer) defaults() {
	if in.Run == nil {
		in.Run = execRunner
	}
	if in.Now == nil {
		in.Now = time.Now
	}
	if in.Log == nil {
		in.Log = slog.Default()
	}
	if in.GOOS == "" {
		in.GOOS = runtime.GOOS
	}
	if in.GOARCH == "" {
		in.GOARCH = runtime.GOARCH
	}
	if in.HealthTimeout <= 0 {
		in.HealthTimeout = 90 * time.Second
	}
	if in.Stable <= 0 {
		in.Stable = 15 * time.Second
	}
	if in.Poll <= 0 {
		in.Poll = 2 * time.Second
	}
	if in.Probe == nil {
		in.Probe = in.probeUnit
	}
}

func (in *Installer) probeUnit(ctx context.Context, since time.Time) (string, error) {
	if _, err := in.Run(ctx, "systemctl", "is-active", "--quiet", in.Unit); err != nil {
		return "", fmt.Errorf("%s is not active", in.Unit)
	}
	if in.Agent {
		c, ok := ReadConnected(in.DataDir)
		if !ok || c.At.Before(since) {
			return "", errors.New("the agent has not connected to the hub since the restart")
		}
		return c.Version, nil
	}
	st, err := localstatus.Fetch(ctx, in.DataDir)
	if err != nil {
		return "", err
	}
	if st.StartedAt.Before(since) {
		return "", errors.New("the hub that answers is the one from before the restart")
	}
	return st.Version, nil
}

// Apply installs the staged release the request asks for. The returned result
// has also been written for the hub. The error is the same as Message and is
// returned for the exit status.
func (in *Installer) Apply(ctx context.Context) (ApplyResult, error) {
	in.defaults()
	dir := ApplyDir(in.DataDir)
	req, ok := ReadRequest(dir)
	if !ok {
		return ApplyResult{}, ErrNoRequest
	}
	// The request goes first, so a failure cannot make systemd start the
	// helper again and again.
	_ = os.Remove(filepath.Join(dir, requestName))

	res := ApplyResult{Version: req.Version, RequestedBy: req.RequestedBy, At: in.Now().UTC()}
	finish := func(outcome string, err error) (ApplyResult, error) {
		res.Outcome, res.At = outcome, in.Now().UTC()
		if err != nil {
			res.Message = err.Error()
		}
		if werr := WriteResult(dir, res); werr != nil {
			in.Log.Error("writing the result", "err", werr)
		}
		in.Log.Info("update finished", "outcome", outcome, "version", res.Version, "err", err)
		return res, err
	}

	want, err := ParseVersion(req.Version)
	if err != nil {
		return finish(OutcomeFailed, fmt.Errorf("the request names an invalid version: %w", err))
	}
	res.Version = want.String()
	from, err := in.installedVersion(ctx)
	if err != nil {
		return finish(OutcomeFailed, err)
	}
	res.From = from.String()
	if want.Compare(from) <= 0 {
		return finish(OutcomeFailed, fmt.Errorf("version %s is not newer than the installed %s; updates never go backwards", want, from))
	}
	res.Outcome = OutcomeInstalling
	_ = WriteResult(dir, res)

	work, err := os.MkdirTemp(in.StateDir, "apply-")
	if err != nil {
		return finish(OutcomeFailed, err)
	}
	defer func() { _ = os.RemoveAll(work) }()

	archive, err := in.fetchVerified(ctx, want, work)
	if err != nil {
		return finish(OutcomeFailed, err)
	}
	binDir := filepath.Dir(in.Binary)
	candidate := filepath.Join(binDir, ".regulus.update")
	defer func() { _ = os.Remove(candidate) }()
	if err := extractBinary(archive, candidate); err != nil {
		return finish(OutcomeFailed, err)
	}
	out, err := in.Run(ctx, candidate, "version")
	if err != nil || strings.TrimSpace(string(out)) != want.String() {
		return finish(OutcomeFailed, fmt.Errorf("the new binary does not report version %s (got %q, %v)", want, strings.TrimSpace(string(out)), err))
	}

	backup := filepath.Join(in.StateDir, "regulus.previous")
	if err := copyExecutable(in.Binary, backup); err != nil {
		return finish(OutcomeFailed, fmt.Errorf("keeping the current binary for a rollback: %w", err))
	}
	if err := os.Rename(candidate, in.Binary); err != nil {
		return finish(OutcomeFailed, err)
	}
	in.Log.Info("binary replaced; restarting", "unit", in.Unit, "from", from, "to", want)

	if err := in.restartAndWatch(ctx, want.String()); err != nil {
		in.Log.Error("the new version is not healthy; rolling back", "err", err)
		if rerr := in.rollback(ctx, backup, from.String()); rerr != nil {
			return finish(OutcomeFailed, fmt.Errorf("%v; and putting %s back failed: %v", err, from, rerr))
		}
		return finish(OutcomeRolledBack, fmt.Errorf("%v; version %s was put back", err, from))
	}
	return finish(OutcomeApplied, nil)
}

func (in *Installer) installedVersion(ctx context.Context) (Version, error) {
	out, err := in.Run(ctx, in.Binary, "version")
	if err != nil {
		return Version{}, fmt.Errorf("asking the installed binary for its version: %w", err)
	}
	v, ok := ParseBuildVersion(strings.TrimSpace(string(out)))
	if !ok {
		return Version{}, fmt.Errorf("the installed binary reports %q, which is not a release version, so an update cannot be compared with it", strings.TrimSpace(string(out)))
	}
	return v, nil
}

// fetchVerified copies the staged archive into root's own directory and checks
// the copy: the signature of checksums.txt by the release workflow, then the
// archive against that file. The hub's user may change the staging directory
// at any time, so only the copy is trusted.
func (in *Installer) fetchVerified(ctx context.Context, v Version, work string) (string, error) {
	stage := filepath.Join(in.DataDir, "updates", v.String())
	var meta Staged
	data, err := readRegular(filepath.Join(stage, metaName), maxControlFile)
	if err != nil {
		return "", fmt.Errorf("version %s is not staged: %w", v, err)
	}
	if err := json.Unmarshal(data, &meta); err != nil || meta.Version != v.String() || meta.Tag != "v"+v.String() {
		return "", fmt.Errorf("the staged release does not match version %s", v)
	}
	name := ArchiveName(v, in.GOOS, in.GOARCH)
	sums, err := readRegular(filepath.Join(stage, checksumsName), maxSmallFile)
	if err != nil {
		return "", err
	}
	bundle, err := readRegular(filepath.Join(stage, bundleName), maxSmallFile)
	if err != nil {
		return "", fmt.Errorf("the staged release has no signature: %w", err)
	}
	if in.Verifier == nil {
		return "", errors.New("no signature verifier is configured")
	}
	if err := in.Verifier.Verify(ctx, meta.Tag, sums, bundle); err != nil {
		return "", err
	}
	want, err := checksumFor(sums, name)
	if err != nil {
		return "", err
	}

	src, err := openRegular(filepath.Join(stage, name))
	if err != nil {
		return "", err
	}
	defer func() { _ = src.Close() }()
	dst := filepath.Join(work, name)
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(src, maxArchive+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if n > maxArchive {
		return "", errors.New("the staged archive is larger than any release should be")
	}
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(h.Sum(nil))), []byte(want)) != 1 {
		return "", fmt.Errorf("%s does not match the signed checksum list", name)
	}
	return dst, nil
}

func (in *Installer) restartAndWatch(ctx context.Context, want string) error {
	since := in.Now()
	if _, err := in.Run(ctx, "systemctl", "restart", in.Unit); err != nil {
		return err
	}
	deadline := time.Now().Add(in.HealthTimeout)
	var upSince time.Time
	var last error
	for {
		got, err := in.Probe(ctx, since)
		switch {
		case err != nil:
			last, upSince = err, time.Time{}
		case got != want:
			last, upSince = fmt.Errorf("the service runs %q, not %s", got, want), time.Time{}
		case upSince.IsZero():
			upSince = time.Now()
		}
		if !upSince.IsZero() && time.Since(upSince) >= in.Stable {
			return nil
		}
		if time.Now().After(deadline) && upSince.IsZero() {
			return fmt.Errorf("the new version did not come up within %s: %v", in.HealthTimeout, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(in.Poll):
		}
	}
}

func (in *Installer) rollback(ctx context.Context, backup, want string) error {
	tmp := filepath.Join(filepath.Dir(in.Binary), ".regulus.rollback")
	defer func() { _ = os.Remove(tmp) }()
	if err := copyExecutable(backup, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, in.Binary); err != nil {
		return err
	}
	since := in.Now()
	if _, err := in.Run(ctx, "systemctl", "restart", in.Unit); err != nil {
		return err
	}
	// The old version is only expected to run; it is not held to the stricter
	// test of the new one.
	deadline := time.Now().Add(in.HealthTimeout)
	for {
		if got, err := in.Probe(ctx, since); err == nil && got == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the previous version did not come up within %s", in.HealthTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(in.Poll):
		}
	}
}

// openRegular opens a regular file without following a link.
func openRegular(p string) (*os.File, error) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("%s is not a regular file", p)
	}
	return f, nil
}

func readRegular(p string, limit int64) ([]byte, error) {
	f, err := openRegular(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is unexpectedly large", filepath.Base(p))
	}
	return data, nil
}

// extractBinary writes the regulus executable of a release archive to dest.
func extractBinary(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("the archive holds no regulus executable")
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg || path.Base(hdr.Name) != "regulus" {
			continue
		}
		_ = os.Remove(dest)
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		n, err := io.Copy(out, io.LimitReader(tr, maxBinary+1))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err == nil && n > maxBinary {
			err = errors.New("the executable is larger than any release should be")
		}
		if err != nil {
			_ = os.Remove(dest)
		}
		return err
	}
}

// copyExecutable copies a file to dest with mode 0755, replacing dest.
func copyExecutable(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	_ = os.Remove(dest)
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
