package vault

import (
	"database/sql"
	"errors"
	"path"
	"strings"

	"github.com/talvor/otman/internal/item"
)

// Snapshot is what otman last wrote for an Item: its filename, title, Kind
// and folder (ADR 0005). doctor compares it with the file to tell which
// side of a title/filename or Kind/folder disagreement changed.
type Snapshot struct {
	Filename string
	Title    *string    // nil when the file had no title
	Kind     *item.Kind // nil when the file had no kind
	Folder   string     // Vault-relative, slash-separated
}

// Snapshot returns the snapshot of Item <key>-<n>, and false when otman
// has not written it on this device since the db was last rebuilt.
func (v *Vault) Snapshot(key string, n int) (Snapshot, bool, error) {
	var s Snapshot
	var title, kind sql.NullString
	err := v.db.QueryRow(`SELECT filename, title, kind, folder FROM snapshots WHERE key = ? AND number = ?`,
		key, n).Scan(&s.Filename, &title, &kind, &s.Folder)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, err
	}
	if title.Valid {
		s.Title = &title.String
	}
	if kind.Valid {
		k := item.Kind(kind.String)
		s.Kind = &k
	}
	return s, true, nil
}

// SetSnapshot replaces the snapshot of Item <key>-<n> with s, or removes it
// when s is nil. doctor uses it to keep the snapshot of Drift it leaves
// unrepaired, so that a later run can still tell which side changed.
func (v *Vault) SetSnapshot(key string, n int, s *Snapshot) error {
	return inImmediateTx(v.db, func(tx *sql.Tx) error {
		if s == nil {
			_, err := tx.Exec(`DELETE FROM snapshots WHERE key = ? AND number = ?`, key, n)
			return err
		}
		var kind *string
		if s.Kind != nil {
			k := string(*s.Kind)
			kind = &k
		}
		_, err := tx.Exec(`INSERT OR REPLACE INTO snapshots (key, number, filename, title, kind, folder)
			VALUES (?, ?, ?, ?, ?, ?)`, key, n, s.Filename, s.Title, kind, s.Folder)
		return err
	})
}

// recordSnapshot records the snapshot of the Item that a write leaves at
// the Vault-relative path to, data, moved from from. prev is the file's
// bytes before the write, or nil when they are not known or the file is
// new. Where an earlier snapshot exists and prev is known, a write records
// only the sides it changed: a title, Kind, filename or folder it left
// alone keeps the snapshot it had, so a hand edit it did not make still
// shows as Drift. Otherwise the write records what it leaves. A file that is
// not an Item file is ignored.
func (v *Vault) recordSnapshot(from, to string, prev, data []byte) error {
	key, n, ok := itemPath(to)
	if !ok {
		return nil
	}
	parsed := item.Parse(data)
	s := Snapshot{Filename: path.Base(to), Title: parsed.Title, Kind: parsed.Kind, Folder: path.Dir(to)}
	last, hasLast, err := v.Snapshot(key, n)
	if err != nil {
		return err
	}
	if hasLast && prev != nil {
		before := item.Parse(prev)
		if same(before.Title, parsed.Title) {
			s.Title = last.Title
		}
		if same(before.Kind, parsed.Kind) {
			s.Kind = last.Kind
		}
		if from == to {
			s.Filename, s.Folder = last.Filename, last.Folder
		}
	}
	var kind *string
	if s.Kind != nil {
		k := string(*s.Kind)
		kind = &k
	}
	return inImmediateTx(v.db, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT OR REPLACE INTO snapshots (key, number, filename, title, kind, folder)
			VALUES (?, ?, ?, ?, ?, ?)`, key, n, s.Filename, s.Title, kind, s.Folder)
		return err
	})
}

// same reports whether a and b are both nil or both hold the same value.
func same[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// itemPath reads the Vault-relative path p as an Item file's: under
// Projects/<KEY>/, outside Templates/, with the <KEY>-<n> prefix.
func itemPath(p string) (key string, n int, ok bool) {
	parts := strings.Split(p, "/")
	if len(parts) < 3 || parts[0] != ProjectsDir || !ValidKey(parts[1]) || parts[2] == TemplatesDir {
		return "", 0, false
	}
	key, n, _, ok = item.ParseFilename(parts[len(parts)-1])
	if !ok || key != parts[1] {
		return "", 0, false
	}
	return key, n, true
}
