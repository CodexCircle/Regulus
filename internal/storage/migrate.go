package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// Each backend has its own directory: the two SQL dialects differ in types,
// defaults and how a table is rebuilt. Versions and names match, and a test
// compares the schemas they produce.
//
//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationFiles embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

// Migrate applies, in order and each in its own transaction, the embedded
// migrations that are not yet recorded in schema_migrations. It refuses to
// run on a database that has migrations this binary does not know, which
// means the database was written by a newer version of Regulus.
//
// typ is TypeSQLite (also when empty) or TypePostgres. On PostgreSQL the whole
// run holds an advisory lock, so two hubs that start together on one database
// do not migrate it at the same time: the second waits, then finds nothing to do.
func Migrate(ctx context.Context, db *sql.DB, typ string) error {
	d := dialects[TypeSQLite]
	if typ == TypePostgres {
		d = dialects[TypePostgres]
	}
	migrations, err := loadMigrations(migrationFiles, d.dir)
	if err != nil {
		return err
	}
	return apply(ctx, db, d, migrations)
}

// dialect holds what differs between the backends in the migration runner.
type dialect struct {
	dir              string
	createMigrations string
	postgres         bool
}

var dialects = map[string]dialect{
	TypeSQLite: {
		dir: "migrations/sqlite",
		createMigrations: `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
	) STRICT`,
	},
	TypePostgres: {
		dir:      "migrations/postgres",
		postgres: true,
		createMigrations: `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    BIGINT PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TEXT NOT NULL DEFAULT to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
	)`,
	},
}

func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var list []migration
	for _, e := range entries {
		file := e.Name()
		prefix, rest, ok := strings.Cut(strings.TrimSuffix(file, ".sql"), "_")
		version, convErr := strconv.Atoi(prefix)
		if !ok || convErr != nil || version <= 0 {
			return nil, fmt.Errorf("migration %q: name must look like 0001_description.sql", file)
		}
		body, err := fs.ReadFile(fsys, dir+"/"+file)
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", file, err)
		}
		list = append(list, migration{version: version, name: rest, sql: string(body)})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].version < list[j].version })
	for i := 1; i < len(list); i++ {
		if list[i].version == list[i-1].version {
			return nil, fmt.Errorf("duplicate migration version %d", list[i].version)
		}
	}
	return list, nil
}

func apply(ctx context.Context, db *sql.DB, d dialect, migrations []migration) error {
	if d.postgres {
		unlock, err := lockPostgres(ctx, db)
		if err != nil {
			return err
		}
		defer unlock()
	}
	if _, err := db.ExecContext(ctx, d.createMigrations); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := map[int]bool{}
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return fmt.Errorf("read schema_migrations: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	_ = rows.Close()

	known := map[int]bool{}
	for _, m := range migrations {
		known[m.version] = true
	}
	for v := range applied {
		if !known[v] {
			return fmt.Errorf("database has migration %d that this version of Regulus does not know: it was created by a newer version", v)
		}
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := applyOne(ctx, db, d, m); err != nil {
			return fmt.Errorf("migration %04d_%s: %w", m.version, m.name, err)
		}
	}
	return nil
}

// lockPostgres takes a session advisory lock for the database, on a
// connection of its own, and returns the function that releases it.
func lockPostgres(ctx context.Context, db *sql.DB) (func(), error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock database for migration: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext('regulus-migrate'))`); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("lock database for migration: %w", err)
	}
	return func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtext('regulus-migrate'))`)
		_ = conn.Close()
	}, nil
}

// noForeignKeys marks a migration that rebuilds a table other tables refer to.
// SQLite cannot change a CHECK constraint in place, so such a migration creates
// the new table, copies the rows, drops the old one and renames the new one;
// with foreign keys on, dropping the old table would delete the rows that
// refer to it. The pragma cannot change inside a transaction, so the migration
// runs with foreign keys off, and the references are checked before it commits.
const noForeignKeys = "-- regulus:no-foreign-keys"

func applyOne(ctx context.Context, db *sql.DB, d dialect, m migration) (err error) {
	// One connection for the pragma and the transaction.
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if !d.postgres && strings.HasPrefix(m.sql, noForeignKeys) {
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
			return err
		}
		defer func() {
			if _, perr := conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys = ON`); perr != nil && err == nil {
				err = perr
			}
		}()
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful commit

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	if !d.postgres && strings.HasPrefix(m.sql, noForeignKeys) {
		var table sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT "table" FROM pragma_foreign_key_check LIMIT 1`).Scan(&table)
		if err == nil {
			return fmt.Errorf("the migration left rows that refer to missing rows in %s", table.String)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name) VALUES (?, ?)`, m.version, m.name); err != nil {
		return err
	}
	return tx.Commit()
}
