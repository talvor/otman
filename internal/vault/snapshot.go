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

// recordSnapshot stores what otman just wrote at the Vault-relative path
// p, data, as the snapshot of the Item p names. Every write of an Item
// file goes through it; a file that is not an Item file is ignored.
func (v *Vault) recordSnapshot(p string, data []byte) error {
	key, n, ok := itemPath(p)
	if !ok {
		return nil
	}
	parsed := item.Parse(data)
	var kind *string
	if parsed.Kind != nil {
		k := string(*parsed.Kind)
		kind = &k
	}
	return inImmediateTx(v.db, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT OR REPLACE INTO snapshots (key, number, filename, title, kind, folder)
			VALUES (?, ?, ?, ?, ?, ?)`, key, n, path.Base(p), parsed.Title, kind, path.Dir(p))
		return err
	})
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
