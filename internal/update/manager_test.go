package update

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodexCircle/Regulus/internal/storage"
)

type fakeSource struct {
	mu       sync.Mutex
	releases []Release
	err      error
	calls    int
}

func (f *fakeSource) Releases(context.Context) ([]Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.releases, f.err
}

func (f *fakeSource) set(releases []Release, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases, f.err = releases, err
}

type recordedEvent struct{ actor, event, target, detail string }

type fakeAudit struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (a *fakeAudit) Record(_ context.Context, actor, event, target, detail string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, recordedEvent{actor, event, target, detail})
}

func (a *fakeAudit) has(event string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range a.events {
		if e.event == event {
			return true
		}
	}
	return false
}

type rig struct {
	m     *Manager
	src   *fakeSource
	audit *fakeAudit
	clock *time.Time
	rel   *fakeRelease
}

// newRig makes a manager for a hub at the given version, with a release
// server that offers `offered`.
func newRig(t *testing.T, current, offered string, mutate ...func(*Options)) *rig {
	t.Helper()
	db, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	r := &rig{src: &fakeSource{}, audit: &fakeAudit{}}
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	r.clock = &now
	if offered != "" {
		r.rel = newFakeRelease(t, offered)
		r.src.set([]Release{r.rel.release()}, nil)
	}
	opts := Options{
		DB: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Source: r.src, Audit: r.audit,
		Stager:  &Stager{Dir: filepath.Join(t.TempDir(), "updates")},
		Version: current, GOOS: "linux", GOARCH: "amd64", Enabled: true,
		Now: func() time.Time { return *r.clock },
	}
	for _, f := range mutate {
		f(&opts)
	}
	r.m = New(opts)
	t.Cleanup(r.m.Close)
	return r
}

func (r *rig) status(t *testing.T) Status {
	t.Helper()
	st, err := r.m.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func (r *rig) waitStaged(t *testing.T) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := r.status(t)
		if !st.Downloading && (st.Staged != nil || st.DownloadError != "") {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("download did not finish: %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDefaults(t *testing.T) {
	st := newRig(t, "0.1.0", "").status(t)
	if st.Mode != ModeManual || st.Channel != Stable || !st.ReleaseBuild || !st.ChecksEnabled || st.Latest != nil || st.UpdateAvailable || st.CheckedAt != nil {
		t.Errorf("status = %+v", st)
	}
	if st.Platform != "linux/amd64" || st.CurrentVersion != "0.1.0" {
		t.Errorf("status = %+v", st)
	}
	// A hub running a pre-release follows the beta channel unless told otherwise.
	if got := newRig(t, "0.2.0-beta.1", "").status(t).Channel; got != Beta {
		t.Errorf("channel of a beta build = %q", got)
	}
}

func TestCheckFindsANewerRelease(t *testing.T) {
	r := newRig(t, "0.1.0", "0.2.0")
	st, err := r.m.Check(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !st.UpdateAvailable || st.Latest == nil || st.Latest.Version != "0.2.0" || st.CheckedAt == nil || st.CheckError != "" {
		t.Fatalf("status = %+v", st)
	}
	// Nothing is downloaded in manual mode.
	if st.Staged != nil {
		t.Errorf("staged = %+v", st.Staged)
	}
}

func TestNoUpdateWhenUpToDateOrAhead(t *testing.T) {
	for _, current := range []string{"0.2.0", "0.3.0", "0.3.0-beta.1"} {
		r := newRig(t, current, "0.2.0")
		st, err := r.m.Check(t.Context())
		if err != nil || st.UpdateAvailable || st.Latest == nil {
			t.Errorf("current %s: status = %+v, err = %v", current, st, err)
		}
	}
}

func TestADevelopmentBuildIsNeverBehind(t *testing.T) {
	for _, current := range []string{"dev", "v0.1.0-4-gabc1234-dirty"} {
		r := newRig(t, current, "9.9.9")
		st, err := r.m.Check(t.Context())
		if err != nil || st.UpdateAvailable || st.ReleaseBuild {
			t.Errorf("current %s: status = %+v, err = %v", current, st, err)
		}
		if _, err := r.m.Download(t.Context(), "admin"); !errors.Is(err, ErrNotReleaseBuild) {
			t.Errorf("current %s: Download error = %v", current, err)
		}
	}
}

func TestCheckFailureKeepsTheLastGoodAnswer(t *testing.T) {
	r := newRig(t, "0.1.0", "0.2.0")
	if _, err := r.m.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	*r.clock = r.clock.Add(time.Hour)
	r.src.set(nil, errors.New("cannot reach the release server: boom"))

	_, err := r.m.Check(t.Context())
	if err == nil || !IsUpstream(err) {
		t.Fatalf("error = %v, upstream = %v", err, IsUpstream(err))
	}
	st := r.status(t)
	if !strings.Contains(st.CheckError, "boom") || st.Latest == nil || st.Latest.Version != "0.2.0" || !st.UpdateAvailable {
		t.Errorf("status = %+v", st)
	}
	// A later good answer clears the error.
	*r.clock = r.clock.Add(time.Hour)
	r.src.set([]Release{r.rel.release()}, nil)
	if st, err := r.m.Check(t.Context()); err != nil || st.CheckError != "" {
		t.Errorf("status = %+v, err = %v", st, err)
	}
}

func TestManualChecksAreSpacedOut(t *testing.T) {
	r := newRig(t, "0.1.0", "0.2.0")
	if _, err := r.m.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Check(t.Context()); !errors.Is(err, ErrTooSoon) {
		t.Fatalf("second check error = %v", err)
	}
	if r.src.calls != 1 {
		t.Errorf("release server asked %d times", r.src.calls)
	}
	*r.clock = r.clock.Add(time.Minute)
	if _, err := r.m.Check(t.Context()); err != nil {
		t.Errorf("check after a minute: %v", err)
	}
}

func TestChecksOff(t *testing.T) {
	r := newRig(t, "0.1.0", "0.2.0", func(o *Options) { o.Enabled = false })
	if _, err := r.m.Check(t.Context()); !errors.Is(err, ErrChecksOff) {
		t.Errorf("Check error = %v", err)
	}
	if _, err := r.m.Download(t.Context(), "admin"); !errors.Is(err, ErrChecksOff) {
		t.Errorf("Download error = %v", err)
	}
	if st := r.status(t); st.ChecksEnabled {
		t.Error("status says checks are on")
	}
	done := make(chan struct{})
	go func() { r.m.Run(t.Context()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return with checks off")
	}
	if r.src.calls != 0 {
		t.Errorf("the release server was asked %d times with checks off", r.src.calls)
	}
}

func TestSettings(t *testing.T) {
	r := newRig(t, "0.1.0", "0.2.0")
	ctx := t.Context()
	st, err := r.m.SetSettings(ctx, "admin", Settings{Mode: ModeDownload, Channel: Beta})
	if err != nil || st.Mode != ModeDownload || st.Channel != Beta {
		t.Fatalf("status = %+v, err = %v", st, err)
	}
	if got, _ := r.m.Settings(ctx); got != (Settings{Mode: ModeDownload, Channel: Beta}) {
		t.Errorf("saved = %+v", got)
	}
	if !r.audit.has(EventSettings) {
		t.Error("the change was not audited")
	}

	for name, s := range map[string]Settings{
		"bad mode":    {Mode: "sometimes", Channel: Stable},
		"bad channel": {Mode: ModeManual, Channel: "nightly"},
		"empty":       {},
	} {
		if _, err := r.m.SetSettings(ctx, "admin", s); err == nil {
			t.Errorf("%s: accepted %+v", name, s)
		}
	}
	// The automatic mode needs a maintenance window, and a way to install.
	for name, s := range map[string]Settings{
		"no window":    {Mode: ModeAutomatic, Channel: Stable},
		"empty window": {Mode: ModeAutomatic, Channel: Stable, WindowStart: "03:00", WindowEnd: "03:00"},
		"bad time":     {Mode: ModeAutomatic, Channel: Stable, WindowStart: "3am", WindowEnd: "05:00"},
	} {
		if _, err := r.m.SetSettings(ctx, "admin", s); !errors.Is(err, ErrInvalidSettings) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
	if _, err := r.m.SetSettings(ctx, "admin", Settings{Mode: ModeAutomatic, Channel: Stable, WindowStart: "03:00", WindowEnd: "05:00"}); !errors.Is(err, ErrApplyDisabled) {
		t.Errorf("automatic without a helper: error = %v", err)
	}
	if got, _ := r.m.Settings(ctx); got != (Settings{Mode: ModeDownload, Channel: Beta}) {
		t.Errorf("a refused change altered the settings: %+v", got)
	}
}

func TestChangingTheChannelDropsTheOldAnswer(t *testing.T) {
	r := newRig(t, "0.1.0", "0.2.0")
	if _, err := r.m.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	st, err := r.m.SetSettings(t.Context(), "admin", Settings{Mode: ModeManual, Channel: Beta})
	if err != nil {
		t.Fatal(err)
	}
	if st.Latest != nil || st.UpdateAvailable {
		t.Errorf("the answer for the stable channel is shown on beta: %+v", st)
	}
	// And a check is asked for.
	select {
	case <-r.m.wake:
	default:
		t.Error("no new check was requested")
	}
}

func TestDownloadStagesAndAudits(t *testing.T) {
	r := newRig(t, "0.1.0", "0.2.0")
	if _, err := r.m.Download(t.Context(), "admin"); !errors.Is(err, ErrNothingToFetch) {
		t.Fatalf("download before any check: error = %v", err)
	}
	if _, err := r.m.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Download(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	st := r.waitStaged(t)
	if st.DownloadError != "" || st.Staged == nil || st.Staged.Version != "0.2.0" || st.Staged.Verified != VerifiedChecksum {
		t.Fatalf("status = %+v", st)
	}
	var got recordedEvent
	for _, e := range r.audit.events {
		if e.event == EventDownloaded {
			got = e
		}
	}
	if got.actor != "admin" || got.target != "0.2.0" || !strings.Contains(got.detail, st.Staged.SHA256) {
		t.Errorf("audit = %+v", got)
	}
}

func TestDownloadFailureIsReportedAndAudited(t *testing.T) {
	r := newRig(t, "0.1.0", "0.2.0")
	const archive = "regulus_0.2.0_linux_amd64.tar.gz"
	good := r.rel.files[archive]
	r.rel.files[archive] = []byte("tampered")
	if _, err := r.m.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Download(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	st := r.waitStaged(t)
	if !strings.Contains(st.DownloadError, "does not match its checksum") || st.Staged != nil || !st.UpdateAvailable {
		t.Fatalf("status = %+v", st)
	}
	if !r.audit.has(EventDownloadFailed) {
		t.Error("the failure was not audited")
	}
	// Trying again, once the server is right, clears the old error.
	r.rel.files[archive] = good
	if _, err := r.m.Download(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	if st := r.waitStaged(t); st.DownloadError != "" || st.Staged == nil {
		t.Errorf("status after the retry = %+v", st)
	}
}

func TestAStagedReleaseThatIsNoLongerNewerIsNotOffered(t *testing.T) {
	r := newRig(t, "0.1.0", "0.2.0")
	if _, err := r.m.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Download(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	r.waitStaged(t)

	// The same cache seen by a hub that has since been updated to 0.2.0.
	after := New(Options{DB: r.m.opts.DB, Source: r.src, Stager: r.m.opts.Stager, Version: "0.2.0", GOOS: "linux", GOARCH: "amd64", Enabled: true})
	st, err := after.Status(t.Context())
	if err != nil || st.Staged != nil {
		t.Errorf("status = %+v, err = %v", st, err)
	}
}

func TestStateSurvivesARestart(t *testing.T) {
	r := newRig(t, "0.1.0", "0.2.0")
	if _, err := r.m.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	again := New(Options{DB: r.m.opts.DB, Source: r.src, Stager: r.m.opts.Stager, Version: "0.1.0", GOOS: "linux", GOARCH: "amd64", Enabled: true})
	st, err := again.Status(t.Context())
	if err != nil || !st.UpdateAvailable || st.Latest.Version != "0.2.0" || st.CheckedAt == nil {
		t.Errorf("status = %+v, err = %v", st, err)
	}
}

func TestBackgroundDownloadInDownloadMode(t *testing.T) {
	r := newRig(t, "0.1.0", "0.2.0")
	if _, err := r.m.SetSettings(t.Context(), "admin", Settings{Mode: ModeDownload, Channel: Stable}); err != nil {
		t.Fatal(err)
	}
	r.m.cycle(t.Context())
	st := r.status(t)
	if st.Staged == nil || st.Staged.Version != "0.2.0" {
		t.Fatalf("status = %+v", st)
	}
	if !hasActor(r.audit, EventDownloaded, SystemActor) {
		t.Errorf("events = %+v", r.audit.events)
	}

	// A second round with the version already staged downloads nothing more.
	before := len(r.audit.events)
	*r.clock = r.clock.Add(7 * time.Hour)
	r.m.cycle(t.Context())
	if len(r.audit.events) != before {
		t.Errorf("events after the second round: %+v", r.audit.events[before:])
	}
}

func TestBackgroundCheckInManualModeDownloadsNothing(t *testing.T) {
	r := newRig(t, "0.1.0", "0.2.0")
	r.m.cycle(t.Context())
	st := r.status(t)
	if !st.UpdateAvailable || st.Staged != nil || st.Downloading {
		t.Fatalf("status = %+v", st)
	}
}

func TestRunChecksAfterTheFirstDelayAndOnWake(t *testing.T) {
	r := newRig(t, "0.1.0", "0.2.0")
	// Pretend the last check was long ago: the first one is due after the
	// short initial delay, which a test cannot wait for, so wake the loop.
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { r.m.Run(ctx); close(done) }()
	r.m.wake <- struct{}{}
	deadline := time.Now().Add(5 * time.Second)
	for r.status(t).Latest == nil {
		if time.Now().After(deadline) {
			t.Fatal("the loop did not check")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestFirstDelayWaitsOutTheIntervalAfterARecentCheck(t *testing.T) {
	r := newRig(t, "0.1.0", "0.2.0")
	if d := r.m.firstDelay(t.Context()); d != firstCheckDelay {
		t.Errorf("first delay = %v with no previous check", d)
	}
	if _, err := r.m.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	*r.clock = r.clock.Add(time.Hour)
	if d := r.m.firstDelay(t.Context()); d != 5*time.Hour {
		t.Errorf("first delay = %v an hour after a check, want 5h", d)
	}
}

func hasActor(a *fakeAudit, event, actor string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range a.events {
		if e.event == event && e.actor == actor {
			return true
		}
	}
	return false
}
