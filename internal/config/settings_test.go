package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const packaged = "../../packaging/config.yaml"

func copyPackaged(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(packaged)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func noEnv(string) string { return "" }

func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ key, in, want string }{
		{"listen", "9090", ":9090"},
		{"listen", " 127.0.0.1:80 ", "127.0.0.1:80"},
		{"listen", ":8080", ":8080"},
		{"listen", "[::1]:8080", "[::1]:8080"},
		{"agent_listen", "", ""},
		{"agent_listen", "off", ""},
		{"agent_listen", "8443", ":8443"},
		{"builtin_agent", "FALSE", "false"},
		{"console.tui", "1", "true"},
		{"metrics_interval", "30s", "30s"},
		{"metrics_interval", "1m", "1m0s"},
		{"log.level", "WARN", "warn"},
		{"log.format", "json", "json"},
		{"data_dir", " /srv/regulus ", "/srv/regulus"},
	} {
		got, err := Normalize(tc.key, tc.in)
		if err != nil || got != tc.want {
			t.Errorf("Normalize(%s, %q) = %q, %v; want %q", tc.key, tc.in, got, err, tc.want)
		}
	}
	for _, tc := range []struct{ key, in, wantErr string }{
		{"listen", "", "must not be empty"},
		{"listen", "0", "1 to 65535"},
		{"listen", "70000", "1 to 65535"},
		{"listen", "localhost", "host:port"},
		{"agent_listen", "nope", "host:port"},
		{"data_dir", " ", "must not be empty"},
		{"builtin_agent", "maybe", "true or false"},
		{"metrics_interval", "soon", "duration"},
		{"metrics_interval", "100ms", "too short"},
		{"log.level", "loud", "one of"},
		{"log.format", "xml", "one of"},
		{"colour", "red", "unknown setting"},
	} {
		if _, err := Normalize(tc.key, tc.in); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("Normalize(%s, %q) error = %v, want one containing %q", tc.key, tc.in, err, tc.wantErr)
		}
	}
}

func TestEveryKeyHasAGetterAndANormalizer(t *testing.T) {
	cfg := Default()
	for _, s := range Settings() {
		v, ok := cfg.Get(s.Key)
		if !ok {
			t.Errorf("%s has no getter", s.Key)
			continue
		}
		if s.Key == "agent_listen" {
			continue // empty by default
		}
		if got, err := Normalize(s.Key, v); err != nil || got != v {
			t.Errorf("the default of %s (%q) does not survive Normalize: %q, %v", s.Key, v, got, err)
		}
	}
}

func TestSetFileKeepsCommentsAndOtherLines(t *testing.T) {
	path := copyPackaged(t)
	before, _ := os.ReadFile(path)

	if got, err := SetFile(path, "listen", "9090"); err != nil || got != ":9090" {
		t.Fatalf("listen: %q, %v", got, err)
	}
	if _, err := SetFile(path, "metrics_interval", "30s"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetFile(path, "log.level", "debug"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetFile(path, "log.format", "json"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	want := strings.NewReplacer(
		`listen: ":8080"`, `listen: ":9090"`,
		`metrics_interval: 10s`, `metrics_interval: 30s`,
		`level: info # debug, info, warn, error`, `level: debug # debug, info, warn, error`,
		`format: text # text, json`, `format: json # text, json`,
	).Replace(string(before))
	if string(after) != want {
		t.Errorf("the file changed beyond the four values:\n%s", after)
	}

	cfg, entries, err := Resolve(path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9090" || cfg.MetricsInterval.String() != "30s" || cfg.Log.Level != "debug" || cfg.Log.Format != "json" {
		t.Errorf("resolved %+v", cfg)
	}
	for _, e := range entries {
		wantOrigin := OriginFile
		if e.Key == "agent_listen" || e.Key == "console.tui" {
			wantOrigin = OriginDefault
		}
		if e.Origin != wantOrigin {
			t.Errorf("%s comes from %s, want %s", e.Key, e.Origin, wantOrigin)
		}
	}
}

func TestSetFileUncommentsTheExampleAndAddsMissingKeys(t *testing.T) {
	path := copyPackaged(t)
	if got, err := SetFile(path, "agent_listen", "8443"); err != nil || got != ":8443" {
		t.Fatalf("agent_listen: %q, %v", got, err)
	}
	if _, err := SetFile(path, "console.tui", "false"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if !strings.Contains(text, "\nagent_listen: \":8443\"\n") {
		t.Errorf("the example was not turned into the setting:\n%s", text)
	}
	if strings.Contains(text, "# agent_listen:") {
		t.Errorf("the commented example is still there:\n%s", text)
	}
	if !strings.HasSuffix(text, "console:\n  tui: false\n") {
		t.Errorf("console.tui was not added in its own block:\n%s", text)
	}
	cfg, _, err := Resolve(path, noEnv)
	if err != nil || cfg.AgentListen != ":8443" || cfg.Console.TUI {
		t.Fatalf("resolved %+v, %v", cfg, err)
	}

	// A second change of console.tui edits the line that is there now.
	if _, err := SetFile(path, "console.tui", "true"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Count(string(data), "tui:") != 1 || !strings.Contains(string(data), "tui: true") {
		t.Errorf("tui was not edited in place:\n%s", data)
	}
	// Turning agent_listen off writes an empty string, which keeps the listener off.
	if _, err := SetFile(path, "agent_listen", "off"); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = Resolve(path, noEnv)
	if err != nil || cfg.AgentListen != "" {
		t.Fatalf("after off: %+v, %v", cfg, err)
	}
}

func TestSetFileRefusesBadInputWithoutTouchingTheFile(t *testing.T) {
	path := copyPackaged(t)
	before, _ := os.ReadFile(path)
	for _, tc := range [][2]string{{"listen", "nope"}, {"metrics_interval", "0s"}, {"log.level", "loud"}, {"colour", "red"}} {
		if _, err := SetFile(path, tc[0], tc[1]); err == nil {
			t.Errorf("%s=%s was accepted", tc[0], tc[1])
		}
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Error("the file changed")
	}
	if _, err := SetFile(filepath.Join(t.TempDir(), "missing.yaml"), "listen", "9090"); err == nil {
		t.Error("a missing file was accepted")
	}
}

func TestSetFileRefusesFormsItCannotEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	text := "log: {level: info, format: text}\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SetFile(path, "log.level", "debug"); err == nil {
		t.Error("an inline mapping was edited blindly")
	}
	if data, _ := os.ReadFile(path); string(data) != text {
		t.Errorf("the file changed: %s", data)
	}
}

func TestResolveReportsWhereValuesComeFrom(t *testing.T) {
	path := copyPackaged(t)
	getenv := func(k string) string {
		if k == "REGULUS_LOG_LEVEL" {
			return "error"
		}
		return ""
	}
	cfg, entries, err := Resolve(path, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Log.Level != "error" {
		t.Errorf("level = %s", cfg.Log.Level)
	}
	got := map[string]string{}
	for _, e := range entries {
		got[e.Key] = e.Origin
	}
	if got["log.level"] != OriginEnv || got["listen"] != OriginFile || got["agent_listen"] != OriginDefault {
		t.Errorf("origins = %v", got)
	}
	if _, _, err := Resolve("", noEnv); err != nil {
		t.Errorf("no file: %v", err)
	}
}

func TestFindFile(t *testing.T) {
	if got := FindFile("/x.yaml", func(string) string { return "/y.yaml" }); got != "/x.yaml" {
		t.Errorf("explicit = %q", got)
	}
	if got := FindFile("", func(string) string { return "/y.yaml" }); got != "/y.yaml" {
		t.Errorf("env = %q", got)
	}
}
