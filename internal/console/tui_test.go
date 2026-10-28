package console

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/CodexCircle/Regulus/internal/config"
	"github.com/CodexCircle/Regulus/internal/localstatus"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii) // no escape codes: the goldens are plain text
	os.Exit(m.Run())
}

var fixedNow = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func fakeSystem() System {
	return System{
		Getenv:       func(string) string { return "" },
		Now:          func() time.Time { return fixedNow },
		ServiceState: func(context.Context) string { return "active" },
		Fetch: func(context.Context, string) (localstatus.Status, error) {
			return localstatus.Status{
				Version:      "v0.3.0",
				StartedAt:    fixedNow.Add(-(3*time.Hour + 12*time.Minute)),
				Addr:         "[::]:8080",
				AgentAddr:    ":8443",
				BuiltinAgent: true,
				Agents: []Agent{
					{ID: "local", Hostname: "ubuntu01", Version: "v0.3.0", Status: "connected", LastHeartbeat: fixedNow.Add(-4 * time.Second)},
					{ID: "web1", Hostname: "web1.lan", Version: "v0.3.0", Status: "stale", LastHeartbeat: fixedNow.Add(-95 * time.Second)},
				},
			}, nil
		},
		Addrs: func() []string { return []string{"192.168.1.20"} },
		Logs: func(context.Context, int) ([]string, error) {
			return []string{
				`time=2026-10-25T11:58:01Z level=INFO msg="hub listening" addr=[::]:8080`,
				`time=2026-10-25T11:58:02Z level=DEBUG msg="agent heartbeat" agent=local`,
				`time=2026-10-25T11:59:10Z level=WARN msg="agent is stale" agent=web1`,
				`time=2026-10-25T11:59:40Z level=ERROR msg="cannot reach the agent" agent=web1 err="timeout"`,
			}, nil
		},
	}
}

// golden compares got with testdata/name.golden.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	got = strings.TrimRight(got, " \n") + "\n"
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run the tests with -update to create it)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the golden file; run with -update if the change is intended.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// trimLines removes the padding at the end of each line, which the goldens do not keep.
func trimLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

func sendKey(t *testing.T, m Model, keys ...tea.KeyMsg) Model {
	t.Helper()
	for _, k := range keys {
		next, cmd := m.Update(k)
		m = next.(Model)
		m = settle(t, m, cmd)
	}
	return m
}

// settle runs the command a key produced and feeds the result back, so the
// tests see the screen after the work is done.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = settle(t, m, c)
		}
		return m
	}
	if _, quit := msg.(tea.QuitMsg); quit || msg == nil {
		return m
	}
	next, cmd := m.Update(msg)
	return settle(t, next.(Model), cmd)
}

func typeText(s string) []tea.KeyMsg {
	var keys []tea.KeyMsg
	for _, r := range s {
		keys = append(keys, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return keys
}

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func rune1(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// copyConfig gives a test its own copy of the packaged configuration, with
// the data directory pointing at a temporary one.
func copyConfig(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../packaging/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	text := strings.Replace(string(data), "/var/lib/regulus", filepath.Join(dir, "data"), 1)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newModel(t *testing.T, configFile string, unicode bool, w, h int) Model {
	t.Helper()
	m := New(Options{System: fakeSystem(), ConfigFile: configFile, Version: "v0.3.0", Unicode: unicode})
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(Model)
	return settle(t, m, m.collect())
}

// goldenModel is a model on the packaged configuration, with a stable path.
func goldenModel(t *testing.T, unicode bool, w, h int) Model {
	t.Helper()
	m := newModel(t, "testdata/config.yaml", unicode, w, h)
	return m
}

func TestScreens(t *testing.T) {
	tests := []struct {
		name    string
		unicode bool
		w, h    int
		keys    []tea.KeyMsg
	}{
		{"status", true, 100, 30, nil},
		{"status-80x24", true, 80, 24, nil},
		{"status-ascii", false, 80, 24, nil},
		{"settings", true, 100, 30, []tea.KeyMsg{rune1('2')}},
		{"settings-second", true, 100, 30, append([]tea.KeyMsg{rune1('2')}, key(tea.KeyDown))},
		{"settings-edit-valid", true, 100, 30, append([]tea.KeyMsg{rune1('2'), key(tea.KeyEnter), key(tea.KeyCtrlU)}, typeText("9090")...)},
		{"settings-edit-invalid", true, 100, 30, append([]tea.KeyMsg{rune1('2'), key(tea.KeyEnter), key(tea.KeyCtrlU)}, typeText("nope")...)},
		{"settings-choices", true, 100, 30, append([]tea.KeyMsg{rune1('2')}, key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyEnter))},
		{"logs", true, 100, 30, []tea.KeyMsg{rune1('3')}},
		{"logs-errors", true, 100, 30, []tea.KeyMsg{rune1('3'), rune1('f'), rune1('f'), rune1('f')}},
		{"health", true, 100, 30, []tea.KeyMsg{rune1('4')}},
		{"restart-confirm", true, 100, 30, []tea.KeyMsg{rune1('r')}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := goldenModel(t, tc.unicode, tc.w, tc.h)
			m = sendKey(t, m, tc.keys...)
			view := trimLines(m.View())
			if n := strings.Count(view, "\n") + 1; n != tc.h {
				t.Errorf("the screen is %d lines, want %d", n, tc.h)
			}
			for i, l := range strings.Split(view, "\n") {
				if w := lipgloss.Width(l); w > tc.w {
					t.Errorf("line %d is %d columns wide, want at most %d: %q", i, w, tc.w, l)
				}
			}
			golden(t, tc.name, view)
		})
	}
}

func TestFirstRunAndStoppedHub(t *testing.T) {
	sys := fakeSystem()
	sys.Fetch = func(context.Context, string) (localstatus.Status, error) {
		return localstatus.Status{Version: "v0.3.0", StartedAt: fixedNow.Add(-time.Minute), Addr: "[::]:8080", SetupToken: "abc123", Agents: []Agent{}}, nil
	}
	m := New(Options{System: sys, ConfigFile: "testdata/config.yaml", Version: "v0.3.0", Unicode: true})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = settle(t, next.(Model), next.(Model).collect())
	golden(t, "first-run", trimLines(m.View()))

	sys.Fetch = func(context.Context, string) (localstatus.Status, error) {
		return localstatus.Status{}, errors.New("no socket")
	}
	sys.ServiceState = func(context.Context) string { return "inactive" }
	m = New(Options{System: sys, ConfigFile: "testdata/config.yaml", Version: "v0.3.0", Unicode: true})
	next, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = settle(t, next.(Model), next.(Model).collect())
	golden(t, "stopped", trimLines(m.View()))
}

func TestEditingASettingWritesTheFile(t *testing.T) {
	path := copyConfig(t)
	m := newModel(t, path, true, 100, 30)

	m = sendKey(t, m, rune1('2'), key(tea.KeyEnter), key(tea.KeyCtrlU))
	m = sendKey(t, m, typeText("nope")...)
	if m.editErr == "" {
		t.Error("an invalid port was accepted while typing")
	}
	m = sendKey(t, m, key(tea.KeyEnter))
	if !m.editing {
		t.Error("enter saved an invalid value")
	}
	m = sendKey(t, m, key(tea.KeyCtrlU))
	m = sendKey(t, m, typeText("9090")...)
	if m.editErr != "" || m.editVal != ":9090" {
		t.Errorf("valid port: err = %q, value = %q", m.editErr, m.editVal)
	}
	m = sendKey(t, m, key(tea.KeyEnter))
	if m.editing {
		t.Fatalf("still editing: %s", m.editErr)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `listen: ":9090"`) {
		t.Errorf("the file was not changed:\n%s", data)
	}
	if !m.needsRestart || !strings.Contains(m.notice, "restart") {
		t.Errorf("no restart hint: %q", m.notice)
	}
	if m.snap.Listen != ":9090" || m.snap.Port != "9090" || m.snap.URL != "http://localhost:9090" {
		t.Errorf("the snapshot was not reloaded: %+v", m.snap)
	}
}

func TestEditingWithEscapeChangesNothing(t *testing.T) {
	path := copyConfig(t)
	before, _ := os.ReadFile(path)
	m := newModel(t, path, true, 100, 30)
	m = sendKey(t, m, rune1('2'), key(tea.KeyEnter), key(tea.KeyCtrlU))
	m = sendKey(t, m, typeText("9090")...)
	m = sendKey(t, m, key(tea.KeyEsc))
	if m.editing {
		t.Error("still editing")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Error("the file changed")
	}
}

func TestTabCyclesThroughChoices(t *testing.T) {
	path := copyConfig(t)
	m := newModel(t, path, true, 100, 30)
	// log.level is the sixth setting.
	keys := []tea.KeyMsg{rune1('2')}
	for i := 0; i < 5; i++ {
		keys = append(keys, key(tea.KeyDown))
	}
	m = sendKey(t, m, append(keys, key(tea.KeyEnter), key(tea.KeyTab))...)
	if m.input.Value() != "warn" {
		t.Errorf("after tab from info: %q, want warn", m.input.Value())
	}
	m = sendKey(t, m, key(tea.KeyEnter))
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "level: warn # debug") {
		t.Errorf("not saved:\n%s", data)
	}
}

func TestNoConfigFileCannotBeEdited(t *testing.T) {
	m := newModel(t, "", true, 100, 30)
	m = sendKey(t, m, rune1('2'), key(tea.KeyEnter), key(tea.KeyCtrlU))
	m = sendKey(t, m, typeText("9090")...)
	m = sendKey(t, m, key(tea.KeyEnter))
	if !m.editing || !strings.Contains(m.editErr, "--config") {
		t.Errorf("editing = %v, err = %q", m.editing, m.editErr)
	}
}

func TestRestartAndResetNeedConfirmation(t *testing.T) {
	restarted, reset := 0, 0
	m := New(Options{System: fakeSystem(), ConfigFile: "testdata/config.yaml", Unicode: true, Hooks: Hooks{
		Restart:    func(context.Context) error { restarted++; return nil },
		ResetAdmin: func(context.Context) (string, error) { reset++; return "http://localhost:8080/#/reset/tok", nil },
	}})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = settle(t, next.(Model), next.(Model).collect())

	m = sendKey(t, m, rune1('r'), rune1('n'))
	if restarted != 0 {
		t.Error("restarted without a yes")
	}
	m = sendKey(t, m, rune1('r'), rune1('y'))
	if restarted != 1 || !strings.Contains(m.notice, "restarted") {
		t.Errorf("restarted = %d, notice = %q", restarted, m.notice)
	}

	m = sendKey(t, m, rune1('p'), rune1('y'))
	if reset != 1 || m.resetLink == "" || !strings.Contains(m.View(), "/#/reset/tok") {
		t.Errorf("reset = %d, link = %q", reset, m.resetLink)
	}
}

func TestFailedRestartSaysWhatToRun(t *testing.T) {
	m := New(Options{System: fakeSystem(), Unicode: true, Hooks: Hooks{
		Restart: func(context.Context) error { return errors.New("Access denied") },
	}})
	m = sendKey(t, m, rune1('r'), rune1('y'))
	if !m.noticeIsErr || !strings.Contains(m.notice, "sudo systemctl restart regulus") {
		t.Errorf("notice = %q", m.notice)
	}
}

func TestQuit(t *testing.T) {
	m := newModel(t, "", true, 100, 30)
	for _, k := range []tea.KeyMsg{rune1('q'), key(tea.KeyCtrlC)} {
		_, cmd := m.Update(k)
		if cmd == nil {
			t.Fatalf("%v did not quit", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%v did not quit", k)
		}
	}
	// q is text while editing a setting.
	m = sendKey(t, m, rune1('2'), key(tea.KeyEnter))
	_, cmd := m.Update(rune1('q'))
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Error("q quit while editing")
		}
	}
}

func TestSectionsByKeyAndMouse(t *testing.T) {
	m := newModel(t, "", true, 100, 30)
	m = sendKey(t, m, key(tea.KeyTab))
	if m.tab != tabSettings {
		t.Errorf("tab key: %d", m.tab)
	}
	m = sendKey(t, m, key(tea.KeyShiftTab), key(tea.KeyShiftTab))
	if m.tab != tabActions {
		t.Errorf("shift+tab wraps: %d", m.tab)
	}
	// Click on "Logs" in the tab bar (third line).
	next, cmd := m.Update(tea.MouseMsg{X: 25, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	m = settle(t, next.(Model), cmd)
	if m.tab != tabLogs {
		t.Errorf("click: %d", m.tab)
	}
}

func TestWindowTooSmallStillDraws(t *testing.T) {
	m := newModel(t, "", true, 20, 6)
	if v := m.View(); v == "" {
		t.Error("nothing drawn")
	}
}

func TestLogFilter(t *testing.T) {
	lines := fakeLogs()
	if got := filterLogs(lines, 0); len(got) != 4 {
		t.Errorf("all: %d", len(got))
	}
	if got := filterLogs(lines, 1); len(got) != 3 {
		t.Errorf("info and above: %d", len(got))
	}
	if got := filterLogs(lines, 2); len(got) != 2 {
		t.Errorf("warnings and errors: %d", len(got))
	}
	if got := filterLogs(lines, 3); len(got) != 1 || !strings.Contains(got[0], "cannot reach") {
		t.Errorf("errors: %v", got)
	}
	for line, want := range map[string]int{
		`{"time":"x","level":"WARN","msg":"y"}`: 2,
		`level=debug msg=x`:                     0,
		`a stack trace line`:                    -1,
	} {
		if got := logSeverity(line); got != want {
			t.Errorf("logSeverity(%q) = %d, want %d", line, got, want)
		}
	}
}

func fakeLogs() []string {
	lines, _ := fakeSystem().Logs(context.Background(), 10)
	return lines
}

func TestEnvironmentOverrideIsFlagged(t *testing.T) {
	sys := fakeSystem()
	sys.Getenv = func(k string) string {
		if k == "REGULUS_LISTEN" {
			return ":9999"
		}
		return ""
	}
	m := New(Options{System: sys, ConfigFile: "testdata/config.yaml", Unicode: true})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = settle(t, next.(Model), next.(Model).collect())
	m = sendKey(t, m, rune1('2'))
	view := m.View()
	if !strings.Contains(view, "environment") || !strings.Contains(view, "overrides the file") {
		t.Errorf("the override is not shown:\n%s", view)
	}
	if m.snap.Port != "9999" {
		t.Errorf("port = %s", m.snap.Port)
	}
}

func TestCollect(t *testing.T) {
	cfgFile := copyConfig(t)
	cfg, _, _ := config.Resolve(cfgFile, func(string) string { return "" })
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}

	snap := Collect(context.Background(), fakeSystem(), cfgFile, "v0.0.1")
	if !snap.Running || snap.Service != "active" || snap.Version != "v0.3.0" || snap.Uptime != "3h 12m" {
		t.Errorf("running hub: %+v", snap)
	}
	if snap.Port != "8080" || snap.URL != "http://localhost:8080" || len(snap.LANURLs) != 1 || snap.LANURLs[0] != "http://192.168.1.20:8080" {
		t.Errorf("addresses: %+v", snap)
	}
	if len(snap.Agents) != 2 || snap.AgentListen != ":8443" {
		t.Errorf("agents: %+v", snap.Agents)
	}
	checks := map[string]Check{}
	for _, c := range snap.Health {
		checks[c.Name] = c
	}
	if checks["Configuration"].Status != OK || checks["Hub"].Status != OK || checks["Dashboard port"].Status != OK || checks["Data directory"].Status != OK {
		t.Errorf("checks: %+v", snap.Health)
	}

	// Not running: the port check really tries the port.
	sys := fakeSystem()
	sys.Fetch = func(context.Context, string) (localstatus.Status, error) {
		return localstatus.Status{}, errors.New("refused")
	}
	sys.ServiceState = func(context.Context) string { return "inactive" }
	snap = Collect(context.Background(), sys, cfgFile, "v0.0.1")
	if snap.Running || snap.Version != "v0.0.1" || len(snap.Agents) != 0 {
		t.Errorf("stopped hub: %+v", snap)
	}
}

func TestCollectReportsABrokenConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("listen: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snap := Collect(context.Background(), fakeSystem(), path, "v1")
	if snap.Problem == "" || len(snap.Settings) == 0 {
		t.Errorf("problem = %q, settings = %d", snap.Problem, len(snap.Settings))
	}
	var bad bool
	for _, c := range snap.Health {
		if c.Name == "Configuration" && c.Status == Fail {
			bad = true
		}
	}
	if !bad {
		t.Errorf("health: %+v", snap.Health)
	}
}

// systemWith returns the fake system with the running hub's agent address and
// CA fingerprint replaced.
func systemWith(agentAddr, fingerprint string) System {
	sys := fakeSystem()
	fetch := sys.Fetch
	sys.Fetch = func(ctx context.Context, dir string) (localstatus.Status, error) {
		st, err := fetch(ctx, dir)
		st.AgentAddr, st.CAFingerprint = agentAddr, fingerprint
		return st, err
	}
	return sys
}

func TestActionsTabListsWhatTheConsoleCanDo(t *testing.T) {
	m := New(Options{System: systemWith(":8443", "sha256:abcd"), ConfigFile: "testdata/config.yaml", Unicode: true})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = settle(t, next.(Model), next.(Model).collect())
	m = sendKey(t, m, rune1('5'))
	if m.tab != tabActions {
		t.Fatalf("tab = %d", m.tab)
	}
	view := m.View()
	for _, want := range []string{
		"5 Actions",
		"r  Restart the service", "p  Reset the administrator password", "a  Turn on remote agents",
		"already on (:8443)",           // agents are on
		"sudo regulus hub reset-admin", // no reset hook here
		"Adding a server", "192.168.1.20:8443", "sha256:abcd", "Servers → Add a server",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the Actions tab does not show %q:\n%s", want, view)
		}
	}
	// Turning agents on again is refused with the reason.
	m = sendKey(t, m, rune1('a'))
	if m.confirm != "" || !m.noticeIsErr || !strings.Contains(m.notice, "already on") {
		t.Errorf("confirm = %q, notice = %q", m.confirm, m.notice)
	}
}

func TestTurningRemoteAgentsOnFromTheConsole(t *testing.T) {
	file := copyConfig(t)
	restarted := 0
	m := New(Options{System: systemWith("", ""), ConfigFile: file, Unicode: true, Hooks: Hooks{
		Restart: func(context.Context) error { restarted++; return nil },
	}})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = settle(t, next.(Model), next.(Model).collect())

	// Select the action in the Actions tab and confirm it.
	m = sendKey(t, m, rune1('5'), key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyEnter))
	if m.confirm != "agents" || !strings.Contains(m.View(), "Turn on remote agents on port 8443") {
		t.Fatalf("confirm = %q\n%s", m.confirm, m.View())
	}
	m = sendKey(t, m, rune1('y'))
	if restarted != 1 || m.noticeIsErr || !strings.Contains(m.notice, "Remote agents are on") {
		t.Errorf("restarted = %d, notice = %q", restarted, m.notice)
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), `agent_listen: ":8443"`) {
		t.Errorf("agent_listen was not written:\n%s", data)
	}
}

func TestTurningRemoteAgentsOnWithoutRestartAsksForIt(t *testing.T) {
	file := copyConfig(t)
	m := New(Options{System: systemWith("", ""), ConfigFile: file, Unicode: true})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = settle(t, next.(Model), next.(Model).collect())
	m = sendKey(t, m, rune1('a'), rune1('y'))
	if !m.needsRestart || !strings.Contains(m.notice, "agent_listen") {
		t.Errorf("needsRestart = %v, notice = %q", m.needsRestart, m.notice)
	}
}
