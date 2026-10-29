// Package storage opens the hub database and applies its migrations
// (migrations/*.sql, embedded, applied in order and recorded). Only SQLite is
// implemented today, through a pure Go driver; PostgreSQL is planned (#122).
// The other packages receive a *sql.DB and own their own tables.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure Go driver, keeps the binary static (no cgo)
)

// FileName is the name of the database file inside the data directory.
const FileName = "regulus.db"

// Open opens (creating it if needed) the database in dataDir and applies all
// pending migrations.
func Open(ctx context.Context, dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	db, err := sql.Open("sqlite", dsn(filepath.Join(dataDir, FileName)))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// A single connection avoids SQLITE_BUSY between concurrent writers. The
	// hub is not write-heavy; revisit if that changes.
	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := Migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func dsn(path string) string {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	return (&url.URL{Scheme: "file", Opaque: filepath.ToSlash(path), RawQuery: q.Encode()}).String()
}
