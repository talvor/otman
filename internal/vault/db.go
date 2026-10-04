package vault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/talvor/otman/internal/output"
	_ "modernc.org/sqlite" // registers the cgo-free "sqlite" driver
)

// schemaVersion is stored in PRAGMA user_version. A db at an older
// version in migrations is migrated; one at any other version is rebuilt,
// which is safe because the db is disposable.
const schemaVersion = 2

// schema is the per-device db. projects is the registry of Projects seen on
// this device, each with its high-water mark: the highest Item number it
// has issued, which never goes down (ADR 0003).
var schema = fmt.Sprintf(`
CREATE TABLE projects (
	key        TEXT PRIMARY KEY,
	high_water INTEGER NOT NULL DEFAULT 0
);
%s
PRAGMA user_version = %d;
`, snapshotsTable, schemaVersion)

// snapshotsTable holds, per Item, the filename, title, Kind and folder
// otman last wrote (ADR 0005), so doctor can tell which side of a
// disagreement changed. title and kind are NULL when the file had none.
const snapshotsTable = `
CREATE TABLE snapshots (
	key      TEXT NOT NULL,
	number   INTEGER NOT NULL,
	filename TEXT NOT NULL,
	title    TEXT,
	kind     TEXT,
	folder   TEXT NOT NULL,
	PRIMARY KEY (key, number)
);`

// migrations bring a db at the version they are keyed by up to the next.
var migrations = map[int]string{
	1: snapshotsTable + "\nPRAGMA user_version = 2;",
}

// openDB opens the db at path with one connection. A missing db is created;
// one that fails quick_check or has an unknown schema is deleted and
// rebuilt, with a db_rebuilt warning. Either way the caller then
// reconciles it from the markdown.
func openDB(path string) (*sql.DB, []output.Problem, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		db, err := createDB(path)
		return db, nil, err
	}
	if err != nil {
		return nil, nil, err
	}
	db, err := connect(path)
	if err != nil {
		return nil, nil, err
	}
	reason := unusableReason(db)
	if reason == "" {
		return db, nil, nil
	}
	db.Close()
	for _, p := range []string{path, path + "-journal"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
	}
	db, err = createDB(path)
	if err != nil {
		return nil, nil, err
	}
	return db, []output.Problem{output.Warning("db_rebuilt",
		"rebuilt the per-device db from the markdown: "+reason,
		map[string]any{"path": path, "reason": reason},
		"nothing to do; the db is a per-device cache rebuilt from the markdown")}, nil
}

// unusableReason returns why the db is unusable, or "" when it is sound,
// migrating a db at an older schema version on the way.
func unusableReason(db *sql.DB) string {
	var result string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return "quick_check failed: " + err.Error()
	}
	if result != "ok" {
		return "quick_check failed: " + result
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return "cannot read schema version: " + err.Error()
	}
	for version != schemaVersion {
		step, ok := migrations[version]
		if !ok {
			return fmt.Sprintf("unknown schema version %d", version)
		}
		if err := inImmediateTx(db, func(tx *sql.Tx) error {
			_, err := tx.Exec(step)
			return err
		}); err != nil {
			return fmt.Sprintf("cannot migrate schema version %d: %v", version, err)
		}
		version++
	}
	return ""
}

func createDB(path string) (*sql.DB, error) {
	db, err := connect(path)
	if err != nil {
		return nil, err
	}
	if err := inImmediateTx(db, func(tx *sql.Tx) error {
		_, err := tx.Exec(schema)
		return err
	}); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// connect opens one connection with ADR 0003's settings: no WAL, full
// sync, and BEGIN IMMEDIATE for every transaction.
func connect(path string) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(DELETE)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Add("_pragma", "busy_timeout(10000)")
	q.Set("_txlock", "immediate")
	dsn := (&url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: q.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// inImmediateTx runs fn in one short BEGIN IMMEDIATE transaction.
func inImmediateTx(db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
