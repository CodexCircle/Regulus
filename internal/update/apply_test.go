package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newApplyRig(t *testing.T, mutate ...func(*Options)) *rig {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "update-apply")
	r := newRig(t, "0.1.0", "0.2.0", append([]func(*Options){func(o *Options) {
		o.ApplyDir = dir
		o.Stager.Verifier = &fakeVerifier{}
	}}, mutate...)...)
	if _, err := r.m.Download(t.Context(), "admin"); err != nil {
		// not available yet: a check comes first
		if _, err := r.m.Check(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := r.m.Download(t.Context(), "admin"); err != nil {
			t.Fatal(err)
		}
	}
	r.waitStaged(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestApplyLeavesARequestForTheHelper(t *testing.T) {
	r := newApplyRig(t)
	st := r.status(t)
	if !st.ApplyEnabled || !st.ApplyAvailable || st.Applying {
		t.Fatalf("before: %+v", st)
	}

	st, err := r.m.Apply(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Applying || st.ApplyAvailable {
		t.Errorf("after: %+v", st)
	}
	req, ok := ReadRequest(r.m.opts.ApplyDir)
	if !ok || req.Version != "0.2.0" || req.RequestedBy != "admin" {
		t.Fatalf("request = %+v, %v", req, ok)
	}
	if !hasActor(r.audit, EventApplyRequested, "admin") {
		t.Errorf("events = %+v", r.audit.events)
	}
	if _, err := r.m.Apply(t.Context(), "admin"); !errors.Is(err, ErrApplying) {
		t.Errorf("a second request: %v", err)
	}

	// Nobody picks it up: after a while the dashboard is told.
	*r.clock = r.clock.Add(applyStall + time.Minute)
	if st := r.status(t); !st.ApplyStalled {
		t.Errorf("stalled request not reported: %+v", st)
	}
}

func TestApplyRefusals(t *testing.T) {
	// Turned off.
	r := newRig(t, "0.1.0", "0.2.0")
	if _, err := r.m.Apply(t.Context(), "admin"); !errors.Is(err, ErrApplyDisabled) {
		t.Errorf("disabled: %v", err)
	}
	if st := r.status(t); st.ApplyEnabled || st.ApplyAvailable {
		t.Errorf("disabled but %+v", st)
	}

	// Nothing downloaded.
	r = newRig(t, "0.1.0", "0.2.0", func(o *Options) { o.ApplyDir = filepath.Join(t.TempDir(), "a") })
	if _, err := r.m.Apply(t.Context(), "admin"); !errors.Is(err, ErrNothingToApply) {
		t.Errorf("nothing staged: %v", err)
	}

	// Only the checksum was checked: the helper would refuse, so the hub does.
	r = newRig(t, "0.1.0", "0.2.0", func(o *Options) { o.ApplyDir = filepath.Join(t.TempDir(), "a") })
	_, _ = r.m.Check(t.Context())
	_, _ = r.m.Download(t.Context(), "admin")
	r.waitStaged(t)
	if _, err := r.m.Apply(t.Context(), "admin"); !errors.Is(err, ErrNotVerified) {
		t.Errorf("unsigned: %v", err)
	}
}

func TestResultOfTheHelperIsAuditedOnceAndTheDownloadForgotten(t *testing.T) {
	r := newApplyRig(t)
	dir := r.m.opts.ApplyDir
	at := time.Date(2026, 10, 25, 12, 5, 0, 0, time.UTC)

	// While the helper works, the dashboard shows it.
	_ = WriteResult(dir, ApplyResult{Version: "0.2.0", Outcome: OutcomeInstalling, At: *r.clock})
	if st := r.status(t); !st.Applying || st.LastApply != nil || st.ApplyAvailable {
		t.Errorf("installing: %+v", st)
	}

	_ = WriteResult(dir, ApplyResult{Version: "0.2.0", From: "0.1.0", Outcome: OutcomeApplied, RequestedBy: "admin", At: at})
	st := r.status(t)
	if st.Applying || st.LastApply == nil || st.LastApply.Outcome != OutcomeApplied {
		t.Errorf("applied: %+v", st)
	}
	r.status(t)
	r.m.syncApply(t.Context())
	n := 0
	for _, e := range r.audit.events {
		if e.event == EventApplied {
			n++
			if e.actor != "admin" || e.target != "0.2.0" {
				t.Errorf("event = %+v", e)
			}
		}
	}
	if n != 1 {
		t.Errorf("update_applied recorded %d times", n)
	}
	if _, err := os.Stat(filepath.Join(r.m.opts.Stager.Dir, "0.2.0")); err == nil {
		t.Error("the installed release is still in the cache")
	}

	_ = WriteResult(dir, ApplyResult{Version: "0.3.0", Outcome: OutcomeRolledBack, At: at.Add(time.Hour)})
	r.m.syncApply(t.Context())
	if !r.audit.has(EventRolledBack) {
		t.Error("the rollback was not audited")
	}
}

func TestAnInstallingResultIsNotBelievedForever(t *testing.T) {
	r := newApplyRig(t)
	_ = WriteResult(r.m.opts.ApplyDir, ApplyResult{Version: "0.2.0", Outcome: OutcomeInstalling, At: *r.clock})
	*r.clock = r.clock.Add(installingMax + time.Minute)
	if st := r.status(t); st.Applying {
		t.Errorf("still installing after a long time: %+v", st)
	}
}

func TestMaintenanceWindow(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 26, h, m, 0, 0, time.UTC) }
	day := Settings{WindowStart: "03:00", WindowEnd: "05:00"}
	night := Settings{WindowStart: "22:00", WindowEnd: "02:00"}
	for _, tc := range []struct {
		s    Settings
		now  time.Time
		want bool
	}{
		{day, at(2, 59), false}, {day, at(3, 0), true}, {day, at(4, 59), true}, {day, at(5, 0), false},
		{night, at(21, 59), false}, {night, at(22, 0), true}, {night, at(23, 30), true}, {night, at(1, 59), true}, {night, at(2, 0), false},
		{Settings{}, at(3, 0), false},
		{Settings{WindowStart: "03:00", WindowEnd: "03:00"}, at(3, 0), false},
	} {
		if got := tc.s.InWindow(tc.now); got != tc.want {
			t.Errorf("%+v at %v = %v", tc.s, tc.now.Format("15:04"), got)
		}
	}
}

func TestAutomaticModeInstallsOnlyInsideTheWindow(t *testing.T) {
	r := newApplyRig(t)
	ctx := t.Context()
	if _, err := r.m.SetSettings(ctx, "admin", Settings{Mode: ModeAutomatic, Channel: Stable, WindowStart: "03:00", WindowEnd: "05:00"}); err != nil {
		t.Fatal(err)
	}

	r.m.automatic(ctx) // 12:00: outside
	if _, ok := ReadRequest(r.m.opts.ApplyDir); ok {
		t.Fatal("installed outside the window")
	}
	*r.clock = time.Date(2026, 10, 26, 3, 10, 0, 0, time.UTC)
	r.m.automatic(ctx)
	req, ok := ReadRequest(r.m.opts.ApplyDir)
	if !ok || req.RequestedBy != SystemActor {
		t.Fatalf("request = %+v, %v", req, ok)
	}
}

func TestAutomaticModeDoesNotTryARolledBackVersionAgain(t *testing.T) {
	r := newApplyRig(t)
	ctx := t.Context()
	if _, err := r.m.SetSettings(ctx, "admin", Settings{Mode: ModeAutomatic, Channel: Stable, WindowStart: "03:00", WindowEnd: "05:00"}); err != nil {
		t.Fatal(err)
	}
	*r.clock = time.Date(2026, 10, 26, 3, 10, 0, 0, time.UTC)
	_ = WriteResult(r.m.opts.ApplyDir, ApplyResult{Version: "0.2.0", Outcome: OutcomeRolledBack, At: r.clock.Add(-time.Hour)})
	r.m.automatic(ctx)
	if _, ok := ReadRequest(r.m.opts.ApplyDir); ok {
		t.Error("a version that was rolled back was tried again")
	}
}

func TestOtherModesNeverInstallByThemselves(t *testing.T) {
	r := newApplyRig(t)
	*r.clock = time.Date(2026, 10, 26, 3, 10, 0, 0, time.UTC)
	if _, err := r.m.SetSettings(t.Context(), "admin", Settings{Mode: ModeDownload, Channel: Stable, WindowStart: "03:00", WindowEnd: "05:00"}); err != nil {
		t.Fatal(err)
	}
	r.m.automatic(t.Context())
	if _, ok := ReadRequest(r.m.opts.ApplyDir); ok {
		t.Error("download mode installed an update")
	}
}
