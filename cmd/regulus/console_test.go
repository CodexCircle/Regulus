package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CodexCircle/Regulus/internal/console"
	"github.com/CodexCircle/Regulus/internal/localstatus"
)

type fixture struct {
	env    env
	out    *bytes.Buffer
	errOut *bytes.Buffer
	opened *console.Options
	file   string
}

// newFixture is a machine with a configuration file, a hub that runs (or not)
// and a terminal (or not).
func newFixture(t *testing.T, running, terminal bool, vars map[string]string) *fixture {
	t.Helper()
	data, err := os.ReadFile("../../packaging/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	text := strings.Replace(string(data), "/var/lib/regulus", filepath.Join(dir, "data"), 1)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fixture{out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, file: file}
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	f.env = env{
		getenv: func(k string) string { return vars[k] },
		stdout: f.out,
		stderr: f.errOut,
		system: console.System{
			Getenv:       func(k string) string { return vars[k] },
			Now:          func() time.Time { return now },
			ServiceState: func(context.Context) string { return "active" },
			Fetch: func(context.Context, string) (localstatus.Status, error) {
				if !running {
					return localstatus.Status{}, errors.New("refused")
				}
				return localstatus.Status{Version: "v1.2.3", StartedAt: now.Add(-time.Hour), Addr: "[::]:8080", Agents: []localstatus.Agent{{ID: "local", Status: "connected", LastHeartbeat: now.Add(-3 * time.Second)}}}, nil
			},
			Addrs: func() []string { return nil },
			Logs:  func(context.Context, int) ([]string, error) { return nil, nil },
		},
		run: func(o console.Options) error { f.opened = &o; return nil },
	}
	if terminal {
		f.env.terminal = terminalInfo{interactive: true, width: 120, height: 40}
	}
	return f
}

func TestStatusFormats(t *testing.T) {
	f := newFixture(t, true, false, nil)
	if code := consoleCommand([]string{"--config", f.file, "--json"}, f.env, false); code != 0 {
		t.Fatalf("exit %d: %s", code, f.errOut)
	}
	var snap console.Snapshot
	if err := json.Unmarshal(f.out.Bytes(), &snap); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, f.out)
	}
	if !snap.Running || snap.Version != "v1.2.3" || snap.Port != "8080" || snap.URL != "http://localhost:8080" || len(snap.Agents) != 1 {
		t.Errorf("snapshot = %+v", snap)
	}

	f.out.Reset()
	if code := consoleCommand([]string{"--config", f.file, "--plain"}, f.env, false); code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"running=true\n", "port=8080\n", "url=http://localhost:8080\n", "agents=1\n", "agent.local=connected 3s ago\n"} {
		if !strings.Contains(f.out.String(), want) {
			t.Errorf("plain output lacks %q:\n%s", want, f.out)
		}
	}

	f.out.Reset()
	if code := consoleCommand([]string{"--config", f.file}, f.env, false); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(f.out.String(), "http://localhost:8080") || !strings.Contains(f.out.String(), "running, up 1h 0m") {
		t.Errorf("text output:\n%s", f.out)
	}
	if f.opened != nil {
		t.Error("status opened the console")
	}
}

func TestStatusExitCodeWhenTheHubIsNotRunning(t *testing.T) {
	f := newFixture(t, false, false, nil)
	if code := consoleCommand([]string{"--config", f.file, "--plain"}, f.env, false); code != exitNotRunning {
		t.Errorf("exit = %d, want %d", code, exitNotRunning)
	}
	if !strings.Contains(f.out.String(), "running=false") {
		t.Errorf("output:\n%s", f.out)
	}
}

func TestStatusWithABrokenConfigurationFails(t *testing.T) {
	f := newFixture(t, true, false, nil)
	if err := os.WriteFile(f.file, []byte("listen: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := consoleCommand([]string{"--config", f.file, "--plain"}, f.env, false); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(f.out.String(), "problem=") {
		t.Errorf("output:\n%s", f.out)
	}
}

func TestConsoleOpensInATerminalOnly(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		vars     map[string]string
		terminal bool
		open     bool
		code     int
	}{
		{"a terminal", nil, nil, true, true, 0},
		{"a pipe", nil, nil, false, false, 0},
		{"--no-tui", []string{"--no-tui"}, nil, true, false, 0},
		{"--notui", []string{"--notui"}, nil, true, false, 0},
		{"--no-input", []string{"--non-interactive"}, nil, true, false, 0},
		{"--json", []string{"--json"}, nil, true, false, 0},
		{"REGULUS_NO_TUI", nil, map[string]string{"REGULUS_NO_TUI": "1"}, true, false, 0},
		{"CI", nil, map[string]string{"CI": "true"}, true, false, 0},
		{"TERM=dumb", nil, map[string]string{"TERM": "dumb"}, true, false, 0},
		{"--tui without a terminal", []string{"--tui"}, nil, false, true, 0},
		{"--tui beats REGULUS_NO_TUI", []string{"--tui"}, map[string]string{"REGULUS_NO_TUI": "1"}, true, true, 0},
		{"--tui and --no-tui", []string{"--tui", "--no-tui"}, nil, true, false, 2},
		{"--tui and --json", []string{"--tui", "--json"}, nil, true, false, 2},
		{"a stray argument", []string{"now"}, nil, true, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, true, tc.terminal, tc.vars)
			args := append([]string{"--config", f.file}, tc.args...)
			if code := consoleCommand(args, f.env, true); code != tc.code {
				t.Fatalf("exit = %d, want %d (%s)", code, tc.code, f.errOut)
			}
			if (f.opened != nil) != tc.open {
				t.Errorf("opened = %v, want %v", f.opened != nil, tc.open)
			}
			if tc.code == 0 && !tc.open && !strings.Contains(f.out.String(), "http://localhost:8080") {
				t.Errorf("no status printed instead:\n%s", f.out)
			}
		})
	}
}

func TestConsoleHonoursTheConfigurationFile(t *testing.T) {
	f := newFixture(t, true, true, nil)
	if code := configCommand([]string{"set", "console.tui", "false", "--config", f.file}, f.env); code != 0 {
		t.Fatalf("set: %d %s", code, f.errOut)
	}
	if code := consoleCommand([]string{"--config", f.file}, f.env, true); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if f.opened != nil {
		t.Error("the console opened although console.tui is false")
	}
	// The flag beats the file.
	if code := consoleCommand([]string{"--config", f.file, "--tui"}, f.env, true); code != 0 || f.opened == nil {
		t.Errorf("--tui: exit %d, opened = %v", code, f.opened != nil)
	}
}

func TestConsolePassesWhatItNeeds(t *testing.T) {
	f := newFixture(t, true, true, map[string]string{"LANG": "en_US.UTF-8"})
	if code := consoleCommand([]string{"--config", f.file}, f.env, true); code != 0 || f.opened == nil {
		t.Fatalf("exit %d", code)
	}
	o := f.opened
	if o.ConfigFile != f.file || !o.Unicode || o.Refresh <= 0 || o.Hooks.Restart == nil || o.Hooks.ResetAdmin == nil {
		t.Errorf("options = %+v", o)
	}
}

func TestConfigListGetSetPath(t *testing.T) {
	f := newFixture(t, false, false, nil)
	run := func(args ...string) (int, string, string) {
		f.out.Reset()
		f.errOut.Reset()
		code := configCommand(append(args, "--config", f.file), f.env)
		return code, f.out.String(), f.errOut.String()
	}

	code, out, _ := run("path")
	if code != 0 || strings.TrimSpace(out) != f.file {
		t.Errorf("path: %d %q", code, out)
	}
	code, out, _ = run("get", "listen")
	if code != 0 || out != ":8080\n" {
		t.Errorf("get: %d %q", code, out)
	}

	// The same change the console makes.
	code, out, errOut := run("set", "listen", "9090")
	if code != 0 || out != "listen = :9090\n" || !strings.Contains(errOut, "systemctl restart regulus") {
		t.Errorf("set: %d %q %q", code, out, errOut)
	}
	code, out, _ = run("set", "metrics_interval", "30s")
	if code != 0 || out != "metrics_interval = 30s\n" {
		t.Errorf("set interval: %d %q", code, out)
	}
	_, out, _ = run("get", "listen")
	if out != ":9090\n" {
		t.Errorf("get after set: %q", out)
	}
	code, out, _ = run("list")
	if code != 0 || !strings.Contains(out, "listen") || !strings.Contains(out, ":9090") || !strings.Contains(out, "30s") {
		t.Errorf("list: %d\n%s", code, out)
	}
	code, out, _ = run("list", "--json")
	var items []map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &items) != nil || len(items) != 8 || items[0]["key"] != "listen" || items[0]["source"] != "file" {
		t.Errorf("list --json: %d\n%s", code, out)
	}
	code, out, _ = run("list", "--plain")
	if code != 0 || !strings.Contains(out, "listen=:9090\n") || !strings.Contains(out, "console.tui=true\n") {
		t.Errorf("list --plain: %d\n%s", code, out)
	}

	// Mistakes change nothing and say what is wrong.
	before, _ := os.ReadFile(f.file)
	for _, args := range [][]string{
		{"set", "listen", "nope"},
		{"set", "metrics_interval", "0s"},
		{"set", "colour", "red"},
		{"set", "listen"},
		{"get", "colour"},
		{"get"},
		{"frobnicate"},
	} {
		if code, _, errOut := run(args...); code == 0 || errOut == "" {
			t.Errorf("%v: exit %d, stderr %q", args, code, errOut)
		}
	}
	after, _ := os.ReadFile(f.file)
	if string(before) != string(after) {
		t.Error("a refused change altered the file")
	}
}

func TestConfigSetWithoutAFile(t *testing.T) {
	f := newFixture(t, false, false, nil)
	if code := configCommand([]string{"set", "listen", "9090"}, f.env); code != 1 || !strings.Contains(f.errOut.String(), "--config") {
		t.Errorf("exit %d: %s", code, f.errOut)
	}
	if code := configCommand([]string{"path"}, f.env); code != 1 {
		t.Errorf("path exit %d", code)
	}
	// Reading works with the defaults.
	f.out.Reset()
	if code := configCommand([]string{"get", "listen"}, f.env); code != 0 || f.out.String() != ":8080\n" {
		t.Errorf("get exit %d: %q", code, f.out)
	}
}

func TestConfigNoteWhenTheEnvironmentOverrides(t *testing.T) {
	f := newFixture(t, false, false, map[string]string{"REGULUS_LISTEN": ":7000"})
	if code := configCommand([]string{"set", "listen", "9090", "--config", f.file}, f.env); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(f.errOut.String(), "REGULUS_LISTEN") {
		t.Errorf("no warning about the environment: %s", f.errOut)
	}
}

func TestConfigUsage(t *testing.T) {
	f := newFixture(t, false, false, nil)
	if code := configCommand(nil, f.env); code != 2 || !strings.Contains(f.errOut.String(), "listen") {
		t.Errorf("no arguments: %d %s", code, f.errOut)
	}
	f.errOut.Reset()
	if code := configCommand([]string{"--help"}, f.env); code != 0 {
		t.Errorf("--help: %d", code)
	}
}

func TestOwnerOf(t *testing.T) {
	dir := t.TempDir()
	uid, gid, ok := ownerOf(dir)
	if !ok || int(uid) != os.Geteuid() || int(gid) != os.Getegid() {
		t.Errorf("owner = %d:%d %v, want %d:%d", uid, gid, ok, os.Geteuid(), os.Getegid())
	}
	if _, _, ok := ownerOf(filepath.Join(dir, "missing")); ok {
		t.Error("a missing directory has an owner")
	}
}

func TestResetAdminLinkFromTheConsole(t *testing.T) {
	dir := hubWithAdmin(t)
	f := newFixture(t, false, false, map[string]string{"REGULUS_DATA_DIR": dir})
	link, err := resetAdminLink(context.Background(), f.env, f.file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link, "http://localhost:8080/#/reset/") {
		t.Errorf("link = %q", link)
	}
	f = newFixture(t, false, false, map[string]string{"REGULUS_DATA_DIR": t.TempDir()})
	if _, err := resetAdminLink(context.Background(), f.env, f.file); err == nil || !strings.Contains(err.Error(), "no hub database") {
		t.Errorf("without a database: %v", err)
	}
}
