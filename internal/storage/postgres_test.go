package storage_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodexCircle/Regulus/internal/storage"
	"github.com/CodexCircle/Regulus/internal/storage/storagetest"
)

func TestRebind(t *testing.T) {
	for in, want := range map[string]string{
		`SELECT 1`:                                   `SELECT 1`,
		`SELECT * FROM t WHERE a = ? AND b = ?`:      `SELECT * FROM t WHERE a = $1 AND b = $2`,
		`SELECT '?' , ? FROM t`:                      `SELECT '?' , $1 FROM t`,
		`SELECT "a?b" FROM t WHERE x = ?`:            `SELECT "a?b" FROM t WHERE x = $1`,
		`INSERT INTO t VALUES (?, ?, ?)`:             `INSERT INTO t VALUES ($1, $2, $3)`,
		`SELECT 'it''s ?' WHERE a = ?`:               `SELECT 'it''s ?' WHERE a = $1`,
		`UPDATE t SET a = ? WHERE b IN (?, ?, ?, ?)`: `UPDATE t SET a = $1 WHERE b IN ($2, $3, $4, $5)`,
	} {
		if got := storage.Rebind(in); got != want {
			t.Errorf("Rebind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigValidateAndDescribe(t *testing.T) {
	ok := storage.Config{Type: "postgres", Host: "db", Port: 5432, Name: "regulus", User: "regulus", SSLMode: "prefer", Password: "hunter2"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if d := ok.Describe(); strings.Contains(d, "hunter2") || !strings.Contains(d, "regulus@db:5432/regulus") {
		t.Errorf("Describe = %q", d)
	}
	for name, mutate := range map[string]func(*storage.Config){
		"no host":    func(c *storage.Config) { c.Host = "" },
		"bad port":   func(c *storage.Config) { c.Port = 70000 },
		"bad mode":   func(c *storage.Config) { c.SSLMode = "sometimes" },
		"bad schema": func(c *storage.Config) { c.Schema = "a;drop table users" },
		"bad type":   func(c *storage.Config) { c.Type = "mysql" },
	} {
		c := ok
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%s: the error shows the password", name)
		}
	}
}

// A database that cannot be reached is reported in words, without the password.
func TestUnreachablePostgresIsExplained(t *testing.T) {
	cfg := storage.Config{Type: "postgres", DataDir: t.TempDir(), Host: "127.0.0.1", Port: 1, Name: "regulus", User: "regulus", SSLMode: "disable", Password: "hunter2"}
	rep, err := storage.Check(context.Background(), cfg)
	if err == nil {
		t.Fatal("check passed against a closed port")
	}
	if strings.Contains(err.Error(), "hunter2") || strings.Contains(rep.Where, "hunter2") {
		t.Errorf("the password leaked: %v / %s", err, rep.Where)
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Errorf("error = %v, want it to say the connection was refused", err)
	}
}

func TestCheckSQLite(t *testing.T) {
	dir := t.TempDir()
	cfg := storage.Config{Type: "sqlite", DataDir: dir}
	rep, err := storage.Check(context.Background(), cfg)
	if err != nil || rep.Created {
		t.Fatalf("empty dir: %+v, %v", rep, err)
	}
	if _, err := os.Stat(filepath.Join(dir, storage.FileName)); err == nil {
		t.Fatal("Check created the database file")
	}
	db, err := storage.OpenSQLite(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	rep, err = storage.Check(context.Background(), cfg)
	if err != nil || !rep.Created || rep.Schema != rep.Known || rep.Schema < 9 {
		t.Fatalf("migrated: %+v, %v", rep, err)
	}
}

func requirePostgres(t *testing.T) {
	t.Helper()
	if !storagetest.Postgres() {
		t.Skip(storagetest.EnvURL + " is not set")
	}
}

func TestCheckPostgres(t *testing.T) {
	requirePostgres(t)
	cfg := storagetest.Config(t, t.TempDir())
	ctx := context.Background()

	rep, err := storage.Check(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Backend != "PostgreSQL" || rep.Schema != 0 || rep.Known < 9 || rep.Version == "" {
		t.Errorf("before migrating: %+v", rep)
	}
	db, err := storage.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if rep, err = storage.Check(ctx, cfg); err != nil || rep.Schema != rep.Known {
		t.Errorf("after migrating: %+v, %v", rep, err)
	}

	// A database written by a newer Regulus is refused by the check too.
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, name) VALUES (9999, 'future')`); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Check(ctx, cfg); err == nil || !strings.Contains(err.Error(), "newer version") {
		t.Errorf("newer schema: %v", err)
	}
	if _, err := storage.Open(ctx, cfg); err == nil || !strings.Contains(err.Error(), "newer version") {
		t.Errorf("Open on a newer schema: %v", err)
	}

	bad := cfg
	bad.Name = "does_not_exist_regulus"
	if _, err := storage.Check(ctx, bad); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing database: %v", err)
	}
	bad = cfg
	bad.Schema = "no_such_schema"
	if _, err := storage.Check(ctx, bad); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Errorf("missing schema: %v", err)
	}
}

// Check creates the citext extension to test the privilege, and that is not
// safe against a concurrent creation, so it must wait for the same advisory
// lock the migrations hold. A flaky "duplicate key ... pg_extension_name_index"
// in CI was this race. The test holds the lock and checks that Check waits.
func TestCheckWaitsForTheMigrationLock(t *testing.T) {
	requirePostgres(t)
	cfg := storagetest.Config(t, t.TempDir())
	ctx := context.Background()

	db, err := storage.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext('regulus-migrate'))`); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := storage.Check(ctx, cfg)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("Check finished while the migration lock was held: %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock(hashtext('regulus-migrate'))`); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Check after the lock was released: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Check did not finish after the lock was released")
	}
}

// Two hubs that start together on one database migrate it once, one after the
// other, because of the advisory lock.
func TestConcurrentMigrationOnPostgres(t *testing.T) {
	requirePostgres(t)
	cfg := storagetest.Config(t, t.TempDir())
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, err := storage.Open(context.Background(), cfg)
			if err == nil {
				err = db.Close()
			}
			errs[i] = err
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("hub %d: %v", i, err)
		}
	}
	rep, err := storage.Check(context.Background(), cfg)
	if err != nil || rep.Schema != rep.Known {
		t.Fatalf("%+v, %v", rep, err)
	}
}

// The behaviour the other packages rely on is the same on both backends.
func TestBackendsBehaveTheSame(t *testing.T) {
	db := storagetest.Open(t, t.TempDir())
	ctx := context.Background()

	// Names compare without regard to case, and are unique that way.
	if _, err := db.ExecContext(ctx, `INSERT INTO users (username, password_hash, role, created_at) VALUES (?, 'h', 'admin', 't')`, "Alice"); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ?`, "aLiCe").Scan(&id); err != nil || id == 0 {
		t.Fatalf("lookup without case: %d, %v", id, err)
	}
	_, err := db.ExecContext(ctx, `INSERT INTO users (username, password_hash, role, created_at) VALUES (?, 'h', 'viewer', 't')`, "ALICE")
	if !storage.IsUniqueViolation(err) {
		t.Errorf("duplicate name differing in case: %v", err)
	}
	// Any role name is accepted since migration 9, compared without case.
	if _, err := db.ExecContext(ctx, `INSERT INTO roles (name, created_at) VALUES ('Ops', 't')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO role_permissions (role, permission) VALUES ('ops', 'x')`); err != nil {
		t.Fatalf("a role name in another case must match the role: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, "OPS", id); err != nil {
		t.Fatal(err)
	}
	// Deleting a user removes what refers to it.
	if _, err := db.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES ('s', ?, 't', 't')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil || n != 0 {
		t.Errorf("sessions left after deleting the user: %d, %v", n, err)
	}
	// A reference to a missing user is refused.
	if _, err := db.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES ('s2', 999, 't', 't')`); err == nil {
		t.Error("foreign keys are not enforced")
	}
	// The upsert form used for password resets.
	for i := range 2 {
		if _, err := db.ExecContext(ctx, `INSERT INTO users (username, password_hash, role, created_at) VALUES (?, 'h', 'admin', 't') ON CONFLICT (username) DO UPDATE SET password_hash = excluded.password_hash`, "bob"); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}
}

// Both backends end with the same tables, columns, keys and indexes.
func TestMigrationsGiveTheSameSchema(t *testing.T) {
	requirePostgres(t)
	ctx := context.Background()

	lite, err := storage.OpenSQLite(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lite.Close() }()

	cfg := storagetest.Config(t, t.TempDir())
	pg, err := storage.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pg.Close() }()

	want := sqliteShape(t, lite)
	got := postgresShape(t, pg)
	if diff := difference(want, got); diff != "" {
		t.Errorf("SQLite (-) and PostgreSQL (+) differ:\n%s", diff)
	}
}

func sqliteShape(t *testing.T, db *sql.DB) []string {
	t.Helper()
	var out []string
	tables := column(t, db, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	for _, tb := range tables {
		rows, err := db.Query(`SELECT name, "notnull", pk FROM pragma_table_info(?)`, tb)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name string
			var notnull, pk int
			if err := rows.Scan(&name, &notnull, &pk); err != nil {
				t.Fatal(err)
			}
			out = append(out, fmt.Sprintf("column %s.%s notnull=%t pk=%t", tb, name, notnull == 1 || pk > 0, pk > 0))
		}
		_ = rows.Close()
		fks, err := db.Query(`SELECT "from", "table", "to", on_delete, on_update FROM pragma_foreign_key_list(?)`, tb)
		if err != nil {
			t.Fatal(err)
		}
		for fks.Next() {
			var from, to, ref, del, upd string
			if err := fks.Scan(&from, &ref, &to, &del, &upd); err != nil {
				t.Fatal(err)
			}
			out = append(out, fmt.Sprintf("fk %s.%s -> %s.%s delete=%s update=%s", tb, from, ref, to, strings.ToLower(del), strings.ToLower(upd)))
		}
		_ = fks.Close()
	}
	for _, idx := range column(t, db, `SELECT tbl_name || '.' || name FROM sqlite_master WHERE type = 'index' AND name NOT LIKE 'sqlite_autoindex%'`) {
		out = append(out, "index "+idx)
	}
	sort.Strings(out)
	return out
}

func postgresShape(t *testing.T, db *sql.DB) []string {
	t.Helper()
	var out []string
	rows, err := db.Query(`
		SELECT c.table_name, c.column_name, c.is_nullable = 'NO',
		       EXISTS (SELECT 1 FROM information_schema.table_constraints tc
		               JOIN information_schema.key_column_usage k USING (constraint_name, table_schema, table_name)
		               WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = c.table_schema
		                 AND tc.table_name = c.table_name AND k.column_name = c.column_name)
		FROM information_schema.columns c WHERE c.table_schema = current_schema()`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var tb, name string
		var notnull, pk bool
		if err := rows.Scan(&tb, &name, &notnull, &pk); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("column %s.%s notnull=%t pk=%t", tb, name, notnull, pk))
	}
	_ = rows.Close()
	fks, err := db.Query(`
		SELECT cl.relname, a.attname, rcl.relname, ra.attname, con.confdeltype, con.confupdtype
		FROM pg_constraint con
		JOIN pg_class cl ON cl.oid = con.conrelid
		JOIN pg_class rcl ON rcl.oid = con.confrelid
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = con.conkey[1]
		JOIN pg_attribute ra ON ra.attrelid = con.confrelid AND ra.attnum = con.confkey[1]
		WHERE con.contype = 'f' AND cl.relnamespace = current_schema()::regnamespace`)
	if err != nil {
		t.Fatal(err)
	}
	action := map[string]string{"a": "no action", "c": "cascade", "n": "set null", "d": "set default", "r": "restrict"}
	for fks.Next() {
		var tb, col, ref, rcol, del, upd string
		if err := fks.Scan(&tb, &col, &ref, &rcol, &del, &upd); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("fk %s.%s -> %s.%s delete=%s update=%s", tb, col, ref, rcol, action[del], action[upd]))
	}
	_ = fks.Close()
	for _, idx := range column(t, db, `SELECT tablename || '.' || indexname FROM pg_indexes WHERE schemaname = current_schema() AND indexname !~ '(_pkey|_key)$'`) {
		out = append(out, "index "+idx)
	}
	sort.Strings(out)
	return out
}

func column(t *testing.T, db *sql.DB, q string) []string {
	t.Helper()
	rows, err := db.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func difference(want, got []string) string {
	in := func(list []string) map[string]bool {
		m := map[string]bool{}
		for _, s := range list {
			m[s] = true
		}
		return m
	}
	w, g := in(want), in(got)
	var b strings.Builder
	for _, s := range want {
		if !g[s] {
			b.WriteString("- " + s + "\n")
		}
	}
	for _, s := range got {
		if !w[s] {
			b.WriteString("+ " + s + "\n")
		}
	}
	return b.String()
}
