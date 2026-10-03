package vault

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/talvor/otman/internal/fsutil"
	"github.com/talvor/otman/internal/item"
)

// TemplatesDir is the folder under Projects/<KEY>/ that holds Templates,
// never Items.
const TemplatesDir = "Templates"

// ItemFile is an Item file found on disk. Its identity is its filename
// prefix <KEY>-<n> (ADR 0005).
type ItemFile struct {
	Key    string
	Number int
	Path   string // Vault-relative, slash-separated
}

// ID is the Item's qualified ID, such as OTM-12.
func (f ItemFile) ID() string { return fmt.Sprintf("%s-%d", f.Key, f.Number) }

// Name is the file name.
func (f ItemFile) Name() string { return path.Base(f.Path) }

// ItemFiles lists Project key's Item files: the files anywhere under
// Projects/<KEY>/, except Templates/, whose names carry the <KEY>-<n>
// prefix. They are sorted by number, then path.
func (v *Vault) ItemFiles(key string) ([]ItemFile, error) {
	dir := filepath.Join(v.Root, ProjectsDir, key)
	var files []ItemFile
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return err
		}
		rel, _ := filepath.Rel(v.Root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == path.Join(ProjectsDir, key, TemplatesDir) {
				return fs.SkipDir
			}
			return nil
		}
		if k, n, _, ok := item.ParseFilename(d.Name()); ok && k == key {
			files = append(files, ItemFile{Key: key, Number: n, Path: rel})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Number != files[j].Number {
			return files[i].Number < files[j].Number
		}
		return files[i].Path < files[j].Path
	})
	return files, nil
}

// highestNumber is the highest Item number among files, or 0.
func highestNumber(files []ItemFile) int {
	n := 0
	for _, f := range files {
		n = max(n, f.Number)
	}
	return n
}

// NewItem is what create writes.
type NewItem struct {
	Title    string
	Kind     item.Kind
	Body     string
	Author   *string
	Assignee *string
	Now      time.Time
}

// CreateItem files a new open Item in Project key, which must exist. Its
// number is max(high-water mark, highest number on disk) + 1, and the
// high-water mark is raised to it before the file is written, so the
// number is never issued again even if the write fails. The caller holds
// the Vault lock, which serialises the scan, the allocation and the write.
func (v *Vault) CreateItem(key string, n NewItem) (ItemFile, []byte, error) {
	files, err := v.ItemFiles(key)
	if err != nil {
		return ItemFile{}, nil, err
	}
	var number int
	err = inImmediateTx(v.db, func(tx *sql.Tx) error {
		var hw int
		err := tx.QueryRow(`SELECT high_water FROM projects WHERE key = ?`, key).Scan(&hw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		number = max(hw, highestNumber(files)) + 1
		return raiseHighWater(tx, key, number)
	})
	if err != nil {
		return ItemFile{}, nil, err
	}

	f := ItemFile{Key: key, Number: number}
	now := n.Now.UTC()
	data, err := item.Render(item.Fields{
		ID: f.ID(), Title: n.Title, Kind: n.Kind, Status: item.Open,
		Author: n.Author, Assignee: n.Assignee,
		Created: now, Updated: now,
	}, n.Body)
	if err != nil {
		return ItemFile{}, nil, err
	}
	project := filepath.Join(v.Root, ProjectsDir, key)
	folder := filepath.Join(project, n.Kind.Folder())
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return ItemFile{}, nil, err
	}
	name := item.Filename(key, number, n.Title)
	target := filepath.Join(folder, name)
	if _, err := os.Lstat(target); err == nil {
		return ItemFile{}, nil, fmt.Errorf("%s already exists", target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return ItemFile{}, nil, err
	}
	if err := fsutil.WriteFile(target, data); err != nil {
		return ItemFile{}, nil, err
	}
	if err := fsutil.SyncDir(project); err != nil {
		return ItemFile{}, nil, err
	}
	f.Path = path.Join(ProjectsDir, key, n.Kind.Folder(), name)
	return f, data, nil
}

// ReadItemFile returns the bytes of an Item file.
func (v *Vault) ReadItemFile(f ItemFile) ([]byte, error) {
	return os.ReadFile(filepath.Join(v.Root, filepath.FromSlash(f.Path)))
}

// WriteItemFile replaces the bytes of an Item file atomically.
func (v *Vault) WriteItemFile(f ItemFile, data []byte) error {
	return fsutil.WriteFile(filepath.Join(v.Root, filepath.FromSlash(f.Path)), data)
}

// ItemAt returns the Item file at the Vault-relative path p, and false
// when p is not an Item file: outside Projects/<KEY>/, in Templates/,
// without the <KEY>-<n> prefix, or missing.
func (v *Vault) ItemAt(p string) (ItemFile, bool, error) {
	p = filepath.ToSlash(p)
	if path.IsAbs(p) || p != path.Clean(p) {
		return ItemFile{}, false, nil
	}
	parts := strings.Split(p, "/")
	if len(parts) < 3 || parts[0] != ProjectsDir || !ValidKey(parts[1]) || parts[2] == TemplatesDir {
		return ItemFile{}, false, nil
	}
	key, n, _, ok := item.ParseFilename(parts[len(parts)-1])
	if !ok || key != parts[1] {
		return ItemFile{}, false, nil
	}
	fi, err := os.Stat(filepath.Join(v.Root, filepath.FromSlash(p)))
	if errors.Is(err, os.ErrNotExist) {
		return ItemFile{}, false, nil
	}
	if err != nil {
		return ItemFile{}, false, err
	}
	if !fi.Mode().IsRegular() {
		return ItemFile{}, false, nil
	}
	return ItemFile{Key: key, Number: n, Path: p}, true, nil
}

// TargetExistsError refuses to move an Item file onto Target, a
// Vault-relative path that already exists.
type TargetExistsError struct{ Target string }

func (e *TargetExistsError) Error() string { return e.Target + " already exists" }

// KindPath is the Vault-relative path Item file f has in the folder for
// Kind kind under its Project.
func (f ItemFile) KindPath(kind item.Kind) string {
	return path.Join(ProjectsDir, f.Key, kind.Folder(), f.Name())
}

// checkMove fails with a *TargetExistsError when moving Item file f to
// the Vault-relative path to would replace another file.
func (v *Vault) checkMove(f ItemFile, to string) error {
	if to == f.Path {
		return nil
	}
	_, err := os.Lstat(filepath.Join(v.Root, filepath.FromSlash(to)))
	switch {
	case err == nil:
		return &TargetExistsError{Target: to}
	case errors.Is(err, os.ErrNotExist):
		return nil
	default:
		return err
	}
}

// MoveItemFile replaces the bytes of Item file f atomically and then
// renames it to the Vault-relative path to, creating its folder. A crash
// between the two leaves the new bytes at the old path. It fails with a
// *TargetExistsError, before writing anything, when to is another file.
func (v *Vault) MoveItemFile(f ItemFile, to string, data []byte) (ItemFile, error) {
	if err := v.checkMove(f, to); err != nil {
		return f, err
	}
	if err := v.WriteItemFile(f, data); err != nil {
		return f, err
	}
	if to == f.Path {
		return f, nil
	}
	from := filepath.Join(v.Root, filepath.FromSlash(f.Path))
	target := filepath.Join(v.Root, filepath.FromSlash(to))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return f, err
	}
	if err := fsutil.SyncDir(filepath.Dir(filepath.Dir(target))); err != nil {
		return f, err
	}
	if err := os.Rename(from, target); err != nil {
		return f, err
	}
	if err := fsutil.SyncDir(filepath.Dir(target)); err != nil {
		return f, err
	}
	if err := fsutil.SyncDir(filepath.Dir(from)); err != nil {
		return f, err
	}
	f.Path = to
	return f, nil
}
