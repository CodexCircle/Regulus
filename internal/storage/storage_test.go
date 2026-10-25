package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestOpenCreatesAndMigrates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	db, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var version int
	if err := db.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 1 {
		t.Fatalf("version = %d, want >= 1", version)
	}
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('k', 'v')`); err != nil {
		t.Fatalf("settings table not usable: %v", err)
	}
}

func TestOpenIsIdempotentAndKeepsData(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('k', 'v')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'k'`).Scan(&v); err != nil || v != "v" {
		t.Fatalf("value = %q, err = %v", v, err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if want := mustLoad(t).count; n != want {
		t.Fatalf("recorded %d migrations, want %d", n, want)
	}
}

type loaded struct{ count int }

func mustLoad(t *testing.T) loaded {
	t.Helper()
	list, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	return loaded{count: len(list)}
}

func TestRefusesNewerDatabase(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, name) VALUES (9999, 'from_the_future')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if _, err := Open(ctx, dir); err == nil || !strings.Contains(err.Error(), "newer version") {
		t.Fatalf("error = %v, want a 'newer version' error", err)
	}
}

func TestApplyOrderAndRollback(t *testing.T) {
	db := memoryDB(t)
	ctx := context.Background()

	err := apply(ctx, db, []migration{
		{version: 1, name: "a", sql: `CREATE TABLE a (x INTEGER)`},
		{version: 2, name: "broken", sql: `CREATE TABLE b (x INTEGER); THIS IS NOT SQL`},
		{version: 3, name: "c", sql: `CREATE TABLE c (x INTEGER)`},
	})
	if err == nil || !strings.Contains(err.Error(), "0002_broken") {
		t.Fatalf("error = %v, want it to name the broken migration", err)
	}
	if tableExists(t, db, "b") {
		t.Error("table b exists: the failed migration was not rolled back")
	}
	if !tableExists(t, db, "a") {
		t.Error("table a missing: migration 1 should have been applied")
	}
	if tableExists(t, db, "c") {
		t.Error("table c exists: migrations after a failure must not run")
	}
}

func TestLoadMigrationsValidatesNames(t *testing.T) {
	for name, file := range map[string]string{
		"missing version": "migrations/init.sql",
		"version zero":    "migrations/0000_zero.sql",
	} {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{file: {Data: []byte("SELECT 1")}}
			if _, err := loadMigrations(fsys); err == nil {
				t.Fatal("expected an error")
			}
		})
	}

	t.Run("duplicate version", func(t *testing.T) {
		fsys := fstest.MapFS{
			"migrations/0001_a.sql": {Data: []byte("SELECT 1")},
			"migrations/0001_b.sql": {Data: []byte("SELECT 1")},
		}
		if _, err := loadMigrations(fsys); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("error = %v, want a duplicate error", err)
		}
	})
}

func memoryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}
