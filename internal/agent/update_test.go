package agent

import (
	"log/slog"
	"os"
	"strings"
	"testing"

	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
	"github.com/CodexCircle/Regulus/internal/update"
)

func TestAgentRefusesUpdatesItCannotOrShouldNotTake(t *testing.T) {
	archive := update.ArchiveName(mustVersion(t, "0.2.0"), "linux", "amd64")
	if runtimeSkip() {
		t.Skip("release archives are built for linux amd64 and arm64")
	}
	for name, tc := range map[string]struct {
		opts Options
		au   *agentv1.ApplyUpdate
		want string
	}{
		"not set up":        {Options{Version: "0.1.0"}, &agentv1.ApplyUpdate{Version: "0.2.0", Archive: archive}, "not set up to update itself"},
		"development build": {Options{Version: "dev", UpdateDir: t.TempDir()}, &agentv1.ApplyUpdate{Version: "0.2.0", Archive: archive}, "development build"},
		"not newer":         {Options{Version: "0.2.0", UpdateDir: t.TempDir()}, &agentv1.ApplyUpdate{Version: "0.2.0", Archive: archive}, "not newer"},
		"older":             {Options{Version: "0.3.0", UpdateDir: t.TempDir()}, &agentv1.ApplyUpdate{Version: "0.2.0", Archive: archive}, "not newer"},
		"invalid version":   {Options{Version: "0.1.0", UpdateDir: t.TempDir()}, &agentv1.ApplyUpdate{Version: "../x", Archive: archive}, "invalid version"},
		"another build":     {Options{Version: "0.1.0", UpdateDir: t.TempDir()}, &agentv1.ApplyUpdate{Version: "0.2.0", Archive: "regulus_0.2.0_linux_s390x.tar.gz"}, "not the build for"},
	} {
		a := &agent{opts: tc.opts, log: slog.New(slog.DiscardHandler)}
		res := a.applyUpdate(t.Context(), "cmd-1", tc.au)
		if res.GetSuccess() || res.GetCommandId() != "cmd-1" || !strings.Contains(res.GetError(), tc.want) {
			t.Errorf("%s: result = %+v, want an error with %q", name, res, tc.want)
		}
		if tc.opts.UpdateDir != "" {
			if entries, _ := os.ReadDir(tc.opts.UpdateDir); len(entries) != 0 {
				t.Errorf("%s: left files behind: %v", name, entries)
			}
		}
	}
}

func TestAgentDoesNotQueueASecondUpdate(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(update.ApplyDir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := update.WriteRequest(update.ApplyDir(dir), update.ApplyRequest{Version: "0.2.0"}); err != nil {
		t.Fatal(err)
	}
	a := &agent{opts: Options{Version: "0.1.0", UpdateDir: dir}, log: slog.New(slog.DiscardHandler)}
	v := mustVersion(t, "0.2.0")
	res := a.applyUpdate(t.Context(), "c", &agentv1.ApplyUpdate{Version: "0.2.0", Archive: update.ArchiveName(v, goos(), goarch())})
	if res.GetSuccess() || !strings.Contains(res.GetError(), "already waiting") {
		t.Errorf("result = %+v", res)
	}
}

func TestMarkConnectedWritesTheFileOnlyWhenUpdatesAreOn(t *testing.T) {
	dir := t.TempDir()
	(&agent{opts: Options{Version: "0.1.0"}, log: slog.New(slog.DiscardHandler)}).markConnected()
	if _, ok := update.ReadConnected(dir); ok {
		t.Error("written without an update directory")
	}
	(&agent{opts: Options{Version: "0.1.0", UpdateDir: dir}, log: slog.New(slog.DiscardHandler)}).markConnected()
	if c, ok := update.ReadConnected(dir); !ok || c.Version != "0.1.0" || c.At.IsZero() {
		t.Errorf("connected = %+v, %v", c, ok)
	}
}

func mustVersion(t *testing.T, s string) update.Version {
	t.Helper()
	v, err := update.ParseVersion(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
