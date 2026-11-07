// Package storagetest opens a hub database for tests. By default it is a
// SQLite file in the directory the test gives, as in production. When the
// environment variable REGULUS_TEST_DATABASE_URL names a PostgreSQL database
// (postgres://user:password@host:port/dbname), the same tests run against it:
// each test gets a schema of its own, dropped when the test ends, so tests do
// not see each other's rows. It is for tests only and never linked into the
// hub.
package storagetest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"strconv"
	"testing"

	"github.com/CodexCircle/Regulus/internal/storage"
)

// EnvURL is the variable that selects PostgreSQL for the tests.
const EnvURL = "REGULUS_TEST_DATABASE_URL"

// Open returns an open, migrated database for the test; it is closed (and, on
// PostgreSQL, its schema dropped) when the test ends. dir is the data
// directory; opening the same dir twice in one test gives the same database.
func Open(tb testing.TB, dir string) *sql.DB {
	tb.Helper()
	cfg, schema := config(tb, dir)
	if schema != "" {
		create(tb, cfg, schema)
	}
	db, err := storage.Open(context.Background(), cfg)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = db.Close() })
	if schema != "" {
		tb.Cleanup(func() { drop(tb, cfg, schema) })
	}
	return db
}

// Config returns the storage settings Open uses for dir, for tests that call
// storage.Open themselves.
func Config(tb testing.TB, dir string) storage.Config {
	tb.Helper()
	cfg, schema := config(tb, dir)
	if schema != "" {
		create(tb, cfg, schema)
		tb.Cleanup(func() { drop(tb, cfg, schema) })
	}
	return cfg
}

// Postgres reports whether the tests run against PostgreSQL.
func Postgres() bool { return urlFromEnv() != "" }

func config(tb testing.TB, dir string) (storage.Config, string) {
	raw := urlFromEnv()
	if raw == "" {
		return storage.Config{Type: storage.TypeSQLite, DataDir: dir}, ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		tb.Fatalf("%s is not a URL", EnvURL)
	}
	port := 5432
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	sum := sha256.Sum256([]byte(dir))
	schema := "t_" + hex.EncodeToString(sum[:8])
	pass, _ := u.User.Password()
	mode := u.Query().Get("sslmode")
	if mode == "" {
		mode = "disable"
	}
	return storage.Config{
		Type: storage.TypePostgres, DataDir: dir,
		Host: u.Hostname(), Port: port, Name: u.Path[1:], User: u.User.Username(), Password: pass,
		SSLMode: mode, Schema: schema,
	}, schema
}

func create(tb testing.TB, cfg storage.Config, schema string) {
	tb.Helper()
	admin := cfg
	admin.Schema = ""
	db, err := storage.OpenRaw(admin)
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE SCHEMA IF NOT EXISTS ` + schema); err != nil {
		tb.Fatalf("create schema %s: %v", schema, storage.Explain(err))
	}
}

// drop removes the schema of a test with a connection that does not use it.
func drop(tb testing.TB, cfg storage.Config, schema string) {
	admin := cfg
	admin.Schema = ""
	db, err := storage.OpenRaw(admin)
	if err != nil {
		tb.Logf("drop schema %s: %v", schema, err)
		return
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`); err != nil {
		tb.Logf("drop schema %s: %v", schema, err)
	}
}

func urlFromEnv() string { return os.Getenv(EnvURL) }

// Env returns the REGULUS_DATABASE_* variables that point the hub's own
// configuration code at the database Open gave dir, for tests of commands that
// load the configuration themselves. Without REGULUS_TEST_DATABASE_URL it is
// empty: SQLite in the data directory is the default.
func Env(tb testing.TB, dir string) map[string]string {
	tb.Helper()
	env := map[string]string{}
	if !Postgres() {
		return env
	}
	cfg, _ := config(tb, dir)
	for k, v := range map[string]string{
		"REGULUS_DATABASE_TYPE": storage.TypePostgres, "REGULUS_DATABASE_HOST": cfg.Host,
		"REGULUS_DATABASE_PORT": strconv.Itoa(cfg.Port), "REGULUS_DATABASE_NAME": cfg.Name,
		"REGULUS_DATABASE_USER": cfg.User, "REGULUS_DATABASE_PASSWORD": cfg.Password,
		"REGULUS_DATABASE_SSLMODE": cfg.SSLMode, "REGULUS_DATABASE_SCHEMA": cfg.Schema,
	} {
		env[k] = v
	}
	return env
}

// Getenv is Env as a lookup function.
func Getenv(tb testing.TB, dir string) func(string) string {
	tb.Helper()
	env := Env(tb, dir)
	return func(k string) string { return env[k] }
}
