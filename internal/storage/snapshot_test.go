package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// openLive opens the database the way the hub does (migrated, WAL).
func openLive(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenSQLite(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, dir
}

func count(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSnapshotRestoreBringsBackTheDatabaseBeforeANewMigration(t *testing.T) {
	ctx := context.Background()
	db, dir := openLive(t)
	if _, err := db.Exec(`CREATE TABLE keepme (v TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO keepme VALUES ('before')`); err != nil {
		t.Fatal(err)
	}
	before, err := SQLiteSchemaVersion(ctx, dir)
	if err != nil || before == 0 {
		t.Fatalf("schema version = %d, %v", before, err)
	}

	snap := filepath.Join(t.TempDir(), "pre-update.db")
	got, err := SnapshotSQLite(ctx, dir, snap) // the hub is running and has the database open
	if err != nil || got != before {
		t.Fatalf("snapshot: version %d, %v (want %d)", got, err, before)
	}
	if info, _ := os.Stat(snap); info.Mode().Perm() != 0o600 {
		t.Errorf("the copy has mode %v, want 0600", info.Mode().Perm())
	}

	// The new version migrates and writes, then turns out not to work.
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, name) VALUES (9999, 'from_the_future')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO keepme VALUES ('after')`); err != nil {
		t.Fatal(err)
	}
	after, _ := SQLiteSchemaVersion(ctx, dir)
	if after != 9999 {
		t.Fatalf("the live schema version = %d, want 9999", after)
	}
	_ = db.Close() // the hub is stopped

	if err := RestoreSQLite(dir, snap); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenSQLite(ctx, dir) // the old version opens it again: it must not see migration 9999
	if err != nil {
		t.Fatalf("the old version cannot open the restored database: %v", err)
	}
	defer func() { _ = restored.Close() }()
	if n := count(t, restored, `SELECT COUNT(*) FROM keepme`); n != 1 {
		t.Errorf("rows after the restore = %d, want only the one from before", n)
	}
	if v := count(t, restored, `SELECT MAX(version) FROM schema_migrations`); v != before {
		t.Errorf("schema version after the restore = %d, want %d", v, before)
	}
}

// What a crash left in the write-ahead log must not be applied to the
// restored file.
func TestRestoreRemovesTheOldWriteAheadLog(t *testing.T) {
	ctx := context.Background()
	db, dir := openLive(t)
	snap := filepath.Join(t.TempDir(), "s.db")
	if _, err := SnapshotSQLite(ctx, dir, snap); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	for _, s := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(SQLitePath(dir)+s, []byte("left by a crash"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RestoreSQLite(dir, snap); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(SQLitePath(dir) + s); err == nil {
			t.Errorf("%s was kept", s)
		}
	}
	reopened, err := OpenSQLite(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = reopened.Close()
}

func TestRestoreKeepsTheModeAndFailsWithoutTouchingTheDatabase(t *testing.T) {
	ctx := context.Background()
	db, dir := openLive(t)
	snap := filepath.Join(t.TempDir(), "s.db")
	if _, err := SnapshotSQLite(ctx, dir, snap); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if err := os.Chmod(SQLitePath(dir), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := RestoreSQLite(dir, snap); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(SQLitePath(dir)); info.Mode().Perm() != 0o640 {
		t.Errorf("mode after the restore = %v, want 0640", info.Mode().Perm())
	}
	before, _ := os.ReadFile(SQLitePath(dir))
	if err := RestoreSQLite(dir, filepath.Join(t.TempDir(), "missing.db")); err == nil {
		t.Error("restoring a missing copy was not an error")
	}
	if after, _ := os.ReadFile(SQLitePath(dir)); string(after) != string(before) {
		t.Error("the database changed although the restore failed")
	}
}

func TestSnapshotOfAMissingDatabaseFails(t *testing.T) {
	if HasSQLite(t.TempDir()) {
		t.Fatal("an empty directory has a database")
	}
	if _, err := SnapshotSQLite(context.Background(), t.TempDir(), filepath.Join(t.TempDir(), "x.db")); err == nil {
		t.Error("no error for a missing database")
	}
}
