package config

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
		if s.Key == "agent_listen" || s.Secret {
			continue // empty by default; a secret is never set through Normalize
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
		if e.Key == "agent_listen" || e.Key == "console.tui" || strings.HasPrefix(e.Key, "database.") {
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

func TestDatabasePasswordIsNeverShownOrStored(t *testing.T) {
	const secret = "s3cret-pw"
	env := map[string]string{"REGULUS_DATABASE_PASSWORD": secret, "REGULUS_DATABASE_TYPE": "postgres", "REGULUS_DATABASE_HOST": "db.example"}
	getenv := func(k string) string { return env[k] }

	cfg, entries, err := Resolve("", getenv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Password != secret || cfg.Database.Host != "db.example" {
		t.Fatalf("environment not read: %+v", cfg.Database)
	}
	found := false
	for _, e := range entries {
		if strings.Contains(e.Value, secret) {
			t.Errorf("%s shows the password: %q", e.Key, e.Value)
		}
		if e.Key == "database.password" {
			found = true
			if e.Value != Masked || e.Origin != OriginEnv || !e.Secret {
				t.Errorf("database.password entry = %+v", e)
			}
		}
	}
	if !found {
		t.Error("database.password is not in the list")
	}
	if v, _ := cfg.Get("database.password"); v != Masked {
		t.Errorf("Get = %q", v)
	}
	if v, _ := Default().Get("database.password"); v != "" {
		t.Errorf("an unset password shows %q", v)
	}

	// Nothing writes it to the file, and the file cannot hold it.
	path := copyPackaged(t)
	if _, err := SetFile(path, "database.password", secret); err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("SetFile accepted the password or echoed it: %v", err)
	}
	if err := os.WriteFile(path, []byte("database:\n  password: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load([]string{"--config", path}, noEnv, io.Discard); err == nil {
		t.Error("a password in the YAML file was accepted")
	}
	// The storage description used in logs has no password either.
	if d := cfg.Storage().Describe(); strings.Contains(d, secret) {
		t.Errorf("Describe = %q", d)
	}
}

func TestDatabaseSettings(t *testing.T) {
	path := copyPackaged(t)
	// The file is checked as a whole after each edit, so the type comes last.
	for _, kv := range [][2]string{
		{"database.host", "db.example"}, {"database.port", "5433"},
		{"database.name", "hub"}, {"database.user", "hubuser"}, {"database.sslmode", "verify-full"},
		{"database.sslrootcert", "/etc/ssl/ca.pem"}, {"database.schema", "regulus"}, {"database.type", "postgres"},
	} {
		if _, err := SetFile(path, kv[0], kv[1]); err != nil {
			t.Fatalf("%s: %v", kv[0], err)
		}
	}
	cfg, _, err := Resolve(path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	want := Database{Type: "postgres", Host: "db.example", Port: 5433, Name: "hub", User: "hubuser", SSLMode: "verify-full", SSLRootCert: "/etc/ssl/ca.pem", Schema: "regulus"}
	if cfg.Database != want {
		t.Errorf("database = %+v, want %+v", cfg.Database, want)
	}

	for _, tc := range []struct{ key, in string }{
		{"database.type", "mysql"}, {"database.port", "0"}, {"database.port", "x"},
		{"database.sslmode", "maybe"}, {"database.schema", "1bad"}, {"database.name", ""},
	} {
		if _, err := Normalize(tc.key, tc.in); err == nil {
			t.Errorf("Normalize(%s, %q) accepted", tc.key, tc.in)
		}
	}

	// PostgreSQL without a host is refused at load time.
	c := Default()
	c.Database.Type = "postgres"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "database.host") {
		t.Errorf("Validate = %v", err)
	}
}

// A write that fails in the middle must leave the old file, whole, and no
// temporary file behind. Writing in place (truncate, then write) would leave an
// empty or cut-short file, which loads as "all defaults".
func TestSetFileLeavesTheOldFileWhenTheWriteFails(t *testing.T) {
	path := copyPackaged(t)
	before, _ := os.ReadFile(path)
	oldRename := renameFile
	renameFile = func(string, string) error { return io.ErrShortWrite } // the last step fails
	t.Cleanup(func() { renameFile = oldRename })

	if _, err := SetFile(path, "listen", "9090"); err == nil {
		t.Fatal("the failure was not reported")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Errorf("the file changed although the write failed:\n%s", after)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("left behind: %s", e.Name())
		}
	}
}

func TestSetFileKeepsTheModeOfTheFile(t *testing.T) {
	path := copyPackaged(t)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := SetFile(path, "listen", "9090"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", info.Mode().Perm())
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), `listen: ":9090"`) {
		t.Errorf("the value was not written:\n%s", got)
	}
}

// A directory that takes no new file still allows the edit, in place.
func TestSetFileFallsBackToWritingInPlace(t *testing.T) {
	path := copyPackaged(t)
	old := createTemp
	createTemp = func(string, string) (*os.File, error) { return nil, fs.ErrPermission }
	t.Cleanup(func() { createTemp = old })
	if _, err := SetFile(path, "listen", "9090"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), `listen: ":9090"`) {
		t.Errorf("the value was not written:\n%s", got)
	}
}

// Two edits at the same time must both survive: each reads the file after the
// other has written it.
func TestSetFileEditsAtTheSameTimeAreNotLost(t *testing.T) {
	path := copyPackaged(t)
	edits := []struct{ key, raw, want string }{
		{"listen", "9091", `listen: ":9091"`},
		{"metrics_interval", "31s", `metrics_interval: 31s`},
		{"log.level", "warn", `level: warn`},
		{"log.format", "json", `format: json`},
		{"updates.check", "false", `check: false`},
		{"console.tui", "false", `tui: false`},
	}
	var wg sync.WaitGroup
	for _, e := range edits {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := SetFile(path, e.key, e.raw); err != nil {
				t.Errorf("%s: %v", e.key, err)
			}
		}()
	}
	wg.Wait()
	got, _ := os.ReadFile(path)
	for _, e := range edits {
		if !strings.Contains(string(got), e.want) {
			t.Errorf("the edit of %s was lost (want %q in the file)", e.key, e.want)
		}
	}
}

// data_dir is where the database, the secret key and the CA are. Pointing it
// at an empty directory does not move them.
func TestSetFileRefusesToLeaveTheHubDataBehind(t *testing.T) {
	old, fresh := t.TempDir(), t.TempDir()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("data_dir: \""+old+"\"\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	// Nothing in the old directory yet: a new installation may move freely.
	if _, err := SetFile(path, "data_dir", fresh); err != nil {
		t.Fatalf("a directory without data: %v", err)
	}
	if _, err := SetFile(path, "data_dir", old); err != nil {
		t.Fatal(err)
	}

	for _, file := range []string{"regulus.db", "secret.key", "ca.key"} {
		if err := os.WriteFile(filepath.Join(old, file), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(path)
		_, err := SetFile(path, "data_dir", fresh)
		if err == nil || !strings.Contains(err.Error(), "--force") || !strings.Contains(err.Error(), "does not move") {
			t.Errorf("with %s in the old directory: err = %v, want a refusal that names --force", file, err)
		}
		if after, _ := os.ReadFile(path); string(after) != string(before) {
			t.Errorf("the file changed although the change was refused")
		}
		_ = os.Remove(filepath.Join(old, file))
	}

	// To a directory that has the data too (copied by hand), and with --force.
	if err := os.WriteFile(filepath.Join(old, "regulus.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fresh, "regulus.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SetFile(path, "data_dir", fresh); err != nil {
		t.Errorf("to a directory that holds the data: %v", err)
	}
	other := t.TempDir()
	if _, err := SetFile(path, "data_dir", other); err == nil {
		t.Error("moving off a directory with data to an empty one was allowed")
	}
	if _, err := SetFileForce(path, "data_dir", other); err != nil {
		t.Errorf("--force: %v", err)
	}
}

// What a write that died in the middle leaves is an empty file, and that would
// silently run the hub on defaults.
func TestLoadRefusesAnEmptyConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Resolve(path, noEnv); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Errorf("Resolve on an empty file: err = %v, want a message that says it is empty", err)
	}
	if _, err := Load([]string{"--config", path}, noEnv, io.Discard); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Errorf("Load on an empty file: err = %v", err)
	}
	// A file with only comments is a deliberate "all defaults".
	if err := os.WriteFile(path, []byte("# nothing here\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Load([]string{"--config", path}, noEnv, io.Discard); err != nil {
		t.Errorf("a file with only comments: %v", err)
	}
}
