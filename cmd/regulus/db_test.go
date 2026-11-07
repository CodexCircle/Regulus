package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodexCircle/Regulus/internal/storage"
	"github.com/CodexCircle/Regulus/internal/storage/storagetest"
)

func runDBCheck(t *testing.T, env map[string]string, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = dbCommand(append([]string{"check", "--env-file", filepath.Join(t.TempDir(), "none.env")}, args...), func(k string) string { return env[k] }, &o, &e)
	return code, o.String(), e.String()
}

func TestDBCheckSQLiteDefault(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{"REGULUS_DATA_DIR": dir}
	code, out, errOut := runDBCheck(t, env)
	if code != 0 || !strings.Contains(out, "SQLite") || !strings.Contains(out, "created at the first start") {
		t.Errorf("empty dir: %d\n%s%s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, storage.FileName)); err == nil {
		t.Error("db check created the database")
	}
	db, err := storage.OpenSQLite(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	code, out, errOut = runDBCheck(t, env)
	if code != 0 || !strings.Contains(out, "Status:   ok") || !strings.Contains(out, "Schema:   version") {
		t.Errorf("migrated: %d\n%s%s", code, out, errOut)
	}
}

func TestDBCheckPostgresNeverPrintsThePassword(t *testing.T) {
	const secret = "hunter2-not-printed"
	env := map[string]string{
		"REGULUS_DATABASE_TYPE": "postgres", "REGULUS_DATABASE_HOST": "127.0.0.1", "REGULUS_DATABASE_PORT": "1",
		"REGULUS_DATABASE_SSLMODE": "disable", "REGULUS_DATABASE_PASSWORD": secret,
	}
	code, out, errOut := runDBCheck(t, env)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\n%s%s", code, out, errOut)
	}
	if strings.Contains(out+errOut, secret) {
		t.Errorf("the password was printed:\n%s%s", out, errOut)
	}
	if !strings.Contains(errOut, "refused") || !strings.Contains(out, "PostgreSQL") {
		t.Errorf("unhelpful output:\n%s%s", out, errOut)
	}

	// A usage error is 2.
	if code, _, _ := runDBCheck(t, map[string]string{"REGULUS_DATABASE_TYPE": "mysql"}); code != 2 {
		t.Errorf("invalid type: exit code = %d, want 2", code)
	}
	if code := dbCommand([]string{"nope"}, noEnv, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Errorf("unknown subcommand: %d", code)
	}
}

func TestDBCheckReadsThePasswordFromTheEnvFile(t *testing.T) {
	if !storagetest.Postgres() {
		t.Skip(storagetest.EnvURL + " is not set")
	}
	dir := t.TempDir()
	storagetest.Open(t, dir) // creates and migrates the schema
	env := storagetest.Env(t, dir)
	envFile := filepath.Join(t.TempDir(), "regulus.env")
	var content strings.Builder
	content.WriteString("# written by the installer\n")
	for k, v := range env {
		content.WriteString(k + "=\"" + v + "\"\n")
	}
	if err := os.WriteFile(envFile, []byte(content.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	var o, e bytes.Buffer
	if code := dbCommand([]string{"check", "--env-file", envFile}, func(string) string { return "" }, &o, &e); code != 0 {
		t.Fatalf("exit code = %d\n%s%s", code, o.String(), e.String())
	}
	if !strings.Contains(o.String(), "PostgreSQL") || !strings.Contains(o.String(), "Status:   ok") {
		t.Errorf("output:\n%s", o.String())
	}
}

func TestReadEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e.env")
	if err := os.WriteFile(path, []byte("# c\n\nA=1\nB = \"two words\"\nC='x'\nD=\"p\\\"a\\\\ss $word#1\"\nbad line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readEnvFile(path)
	if got["A"] != "1" || got["B"] != "two words" || got["C"] != "x" || got["D"] != `p"a\ss $word#1` || len(got) != 4 {
		t.Errorf("got %v", got)
	}
	if len(readEnvFile(filepath.Join(t.TempDir(), "missing"))) != 0 {
		t.Error("a missing file gave values")
	}
}
