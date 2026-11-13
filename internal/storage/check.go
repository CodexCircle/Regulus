package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgconn"
)

// MinPostgresVersion is the oldest PostgreSQL major version Regulus supports:
// 14, the one in Ubuntu 22.04. Newer ones (16 in Ubuntu 24.04) work too.
const MinPostgresVersion = 14

// Report is what Check found out about a database.
type Report struct {
	// Backend is "SQLite" or "PostgreSQL".
	Backend string
	// Where names the database, without any password.
	Where string
	// Version is the version of the database server (or SQLite library).
	Version string
	// Schema is the highest migration applied, 0 for a database not migrated.
	Schema int
	// Known is the highest migration this binary knows.
	Known int
	// Created is false for a SQLite file that does not exist yet.
	Created bool
}

// Check connects with the given settings and reports on the database without
// changing anything that remains: it does not run migrations and does not
// create a SQLite file. It tries once and returns at the first problem, with
// a message that says what to fix (see Explain). The installer uses it.
func Check(ctx context.Context, cfg Config) (Report, error) {
	rep := Report{Where: cfg.Describe()}
	if err := cfg.Validate(); err != nil {
		return rep, err
	}
	list, err := loadMigrations(migrationFiles, dialects[backendType(cfg)].dir)
	if err != nil {
		return rep, err
	}
	rep.Known = list[len(list)-1].version

	if !cfg.IsPostgres() {
		return checkSQLite(ctx, cfg, rep)
	}
	rep.Backend = "PostgreSQL"
	db, err := openPostgres(cfg)
	if err != nil {
		return rep, err
	}
	defer func() { _ = db.Close() }()
	return checkPostgres(ctx, db, cfg, rep)
}

func backendType(cfg Config) string {
	if cfg.IsPostgres() {
		return TypePostgres
	}
	return TypeSQLite
}

func checkSQLite(ctx context.Context, cfg Config, rep Report) (Report, error) {
	rep.Backend = "SQLite"
	path := filepath.Join(cfg.DataDir, FileName)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			rep.Version = "-"
			return rep, nil // it is created at the first start
		}
		return rep, fmt.Errorf("cannot read %s: %w", path, err)
	}
	rep.Created = true
	db, err := openPool(cfg)
	if err != nil {
		return rep, err
	}
	defer func() { _ = db.Close() }()
	if err := db.QueryRowContext(ctx, `SELECT sqlite_version()`).Scan(&rep.Version); err != nil {
		return rep, fmt.Errorf("cannot read %s: %s", path, Explain(err))
	}
	return rep, schemaVersion(ctx, db, &rep)
}

func checkPostgres(ctx context.Context, db *sql.DB, cfg Config, rep Report) (Report, error) {
	rep.Created = true
	var versionNum int
	if err := db.QueryRowContext(ctx, `SELECT current_setting('server_version_num')::int, current_setting('server_version')`).Scan(&versionNum, &rep.Version); err != nil {
		return rep, errors.New(Explain(err))
	}
	if versionNum/10000 < MinPostgresVersion {
		return rep, fmt.Errorf("PostgreSQL %s is too old: Regulus needs version %d or newer", rep.Version, MinPostgresVersion)
	}

	schema, err := verifySchema(ctx, db, cfg)
	if err != nil {
		return rep, err
	}

	// The migrations create the citext extension under an advisory lock, and
	// CREATE EXTENSION IF NOT EXISTS is not safe against a concurrent creation
	// (the second session fails with a duplicate key on pg_extension). Take the
	// same lock so the check cannot collide with a hub that is migrating or with
	// another check on the same database.
	unlock, err := lockPostgres(ctx, db)
	if err != nil {
		return rep, errors.New(Explain(err))
	}
	defer unlock()

	// Try, inside a transaction that is rolled back, what the migrations will
	// do, so a missing privilege is found now and not at the first start.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return rep, errors.New(Explain(err))
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE regulus_privilege_check (id INTEGER)`); err != nil {
		return rep, fmt.Errorf("the user cannot create tables in schema %q: %s (grant CREATE on the schema, or make the user the owner of the database)", schema, Explain(err))
	}
	if _, err := tx.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS citext WITH SCHEMA public`); err != nil {
		return rep, fmt.Errorf("the extension citext is not available to this user: %s (as a superuser run: CREATE EXTENSION citext; in this database, and install postgresql-contrib if it is missing)", Explain(err))
	}
	_ = tx.Rollback()

	return rep, schemaVersion(ctx, db, &rep)
}

// schemaVersion fills in the highest applied migration and refuses a database
// written by a newer Regulus.
func schemaVersion(ctx context.Context, db *sql.DB, rep *Report) error {
	var present bool
	probe := `SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations')`
	if rep.Backend == "PostgreSQL" {
		probe = `SELECT to_regclass('schema_migrations') IS NOT NULL`
	}
	if err := db.QueryRowContext(ctx, probe).Scan(&present); err != nil {
		return errors.New(Explain(err))
	}
	if !present {
		return nil
	}
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return errors.New(Explain(err))
	}
	rep.Schema = int(v.Int64)
	if rep.Schema > rep.Known {
		return fmt.Errorf("the database is at schema version %d but this Regulus knows up to %d: it was written by a newer version", rep.Schema, rep.Known)
	}
	return nil
}

// Explain turns an error from the database into a sentence that says what to
// fix. It never includes a password.
func Explain(err error) string {
	if err == nil {
		return ""
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "28P01":
			return "authentication failed: wrong password for this user"
		case "28000":
			return "the server refused the connection (the user does not exist, or pg_hba.conf does not allow this host): " + pg.Message
		case "3D000":
			return "the database does not exist: create it first (see the PostgreSQL prerequisites in the documentation)"
		case "42501":
			return "missing privilege: " + pg.Message
		case "57P03":
			return "PostgreSQL is starting up and does not accept connections yet"
		}
		return pg.Message
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return fmt.Sprintf("cannot resolve the host name %q", dns.Name)
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "connection refused: PostgreSQL is not running there, or not listening on that host and port (check listen_addresses, the port and the firewall)"
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() || errors.Is(err, context.DeadlineExceeded) {
		return "timed out: the host does not answer (check the host, the port and the firewall)"
	}
	msg := err.Error()
	if strings.Contains(msg, "tls:") || strings.Contains(msg, "SSL") || strings.Contains(msg, "certificate") {
		return "TLS problem: " + msg + " (check database.sslmode and database.sslrootcert)"
	}
	return msg
}

// verifySchema returns the schema the hub's tables go in. With a schema named
// in the settings it must exist: PostgreSQL would otherwise quietly use the
// next one on the search path and the tables would land in public.
func verifySchema(ctx context.Context, db *sql.DB, cfg Config) (string, error) {
	var current sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&current); err != nil {
		return "", errors.New(Explain(err))
	}
	want := cfg.Schema
	if want == "" {
		want = current.String
	}
	if !current.Valid || current.String != want {
		return "", fmt.Errorf("the schema %q does not exist (or the user cannot see it): create it with CREATE SCHEMA, or leave database.schema empty to use public", cfg.Schema)
	}
	return current.String, nil
}
