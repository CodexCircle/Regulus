package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Copies of the SQLite database for the update helper. Before it replaces the
// program the helper (which runs as root) keeps a consistent copy of the
// database; if the new version migrated the schema and then turned out not to
// work, the old version cannot read the migrated database (it refuses a
// schema it does not know), so the copy is put back together with the old
// program. PostgreSQL cannot be copied from here: see docs/updates.md.

// SQLitePath is the database file inside the data directory.
func SQLitePath(dataDir string) string { return filepath.Join(dataDir, FileName) }

// HasSQLite tells whether the data directory holds a SQLite database.
func HasSQLite(dataDir string) bool {
	info, err := os.Stat(SQLitePath(dataDir))
	return err == nil && info.Mode().IsRegular()
}

// openReadOnly opens the database without ever changing it.
func openReadOnly(path string) (*sql.DB, error) {
	q := url.Values{}
	q.Add("mode", "ro")
	q.Add("_pragma", "busy_timeout(5000)")
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Opaque: filepath.ToSlash(path), RawQuery: q.Encode()}).String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// SQLiteSchemaVersion returns the highest migration applied to the SQLite
// database in dataDir, or 0 for a database without any. The hub may be
// running: the database is opened read-only.
func SQLiteSchemaVersion(ctx context.Context, dataDir string) (int, error) {
	path := SQLitePath(dataDir)
	defer matchOwner(path)
	db, err := openReadOnly(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()
	return highestMigration(ctx, db)
}

func highestMigration(ctx context.Context, db *sql.DB) (int, error) {
	var v sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil
		}
		return 0, fmt.Errorf("read schema_migrations: %w", err)
	}
	return int(v.Int64), nil
}

// SnapshotSQLite writes a consistent copy of the SQLite database in dataDir
// to dest (VACUUM INTO: it is safe while the hub runs and writes) and returns
// the schema version the copy has. dest is replaced if it exists, and is
// readable by its owner only.
func SnapshotSQLite(ctx context.Context, dataDir, dest string) (int, error) {
	path := SQLitePath(dataDir)
	defer matchOwner(path)
	db, err := openReadOnly(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()
	if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	if _, err := db.ExecContext(ctx, `VACUUM INTO '`+strings.ReplaceAll(dest, `'`, `''`)+`'`); err != nil {
		return 0, fmt.Errorf("copy the database: %w", err)
	}
	if err := os.Chmod(dest, 0o600); err != nil {
		return 0, err
	}
	if f, err := os.Open(dest); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
	snap, err := openReadOnly(dest)
	if err != nil {
		return 0, err
	}
	defer func() { _ = snap.Close() }()
	var ok string
	if err := snap.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&ok); err != nil || ok != "ok" {
		return 0, fmt.Errorf("the copy of the database is damaged (%s, %v)", ok, err)
	}
	return highestMigration(ctx, snap)
}

// RestoreSQLite puts the copy at snapshot in place of the SQLite database in
// dataDir, which nothing may have open (stop the hub first). The write-ahead
// log and its shared-memory file belong to the database being replaced and are
// removed: applied to another file they would corrupt it. The restored file
// keeps the owner and mode of the one it replaces. It is written next to it,
// synced and renamed, so a failure leaves the old file.
func RestoreSQLite(dataDir, snapshot string) error {
	dest := SQLitePath(dataDir)
	src, err := os.Open(snapshot)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	tmp, err := os.CreateTemp(dataDir, ".restore-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(dest); err == nil {
		mode = info.Mode().Perm()
		copyOwner(tmp, info)
	} else if info, err := os.Stat(dataDir); err == nil {
		copyOwner(tmp, info) // a hub that lost its database still owns its directory
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Remove(dest + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return err
	}
	if d, err := os.Open(dataDir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
