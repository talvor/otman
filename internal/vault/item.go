package vault

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
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

// folderKind is the Kind whose folder, directly under the folder of the
// Project that holds the file, holds it, and false when the file is
// anywhere else. A misplaced file's folder is another Project's.
func (f ItemFile) folderKind() (item.Kind, bool) {
	dir, folder := path.Split(path.Dir(f.Path))
	if dir != path.Join(ProjectsDir, f.FolderKey())+"/" {
		return "", false
	}
	return item.KindOfFolder(folder)
}

// Derived is what the file's name and folder say about the Item.
func (f ItemFile) Derived() item.Derived {
	_, _, title, _ := item.ParseFilename(f.Name())
	d := item.Derived{ID: f.ID(), Title: title}
	if k, ok := f.folderKind(); ok {
		d.Kind = &k
	}
	return d
}

// FolderKey is the key of the Project whose folder holds the file. It
// differs from Key when the file is misplaced: filed by hand under another
// Project's folder.
func (f ItemFile) FolderKey() string { return strings.Split(f.Path, "/")[1] }

// Misplaced reports whether the file is in another Project's folder than
// the one its prefix names.
func (f ItemFile) Misplaced() bool { return f.FolderKey() != f.Key }

// ItemFiles lists Project key's Item files: the files whose names carry
// the <KEY>-<n> prefix anywhere under Projects/, except in Templates/. A
// file filed under another Project's folder is read under the Project its
// prefix names (see Misplaced). They are sorted by number, then path.
// The Vault remembers each Project it lists (see Scanned).
func (v *Vault) ItemFiles(key string) ([]ItemFile, error) {
	if v.scanned == nil {
		v.scanned = map[string]bool{}
	}
	v.scanned[key] = true
	return v.scanProject(key)
}

// Scanned is the keys of the Projects whose Item files the Vault has listed
// since it was opened, sorted.
func (v *Vault) Scanned() []string {
	keys := make([]string, 0, len(v.scanned))
	for k := range v.scanned {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// scanProject is ItemFiles without remembering key, for the Vault's own
// scans, such as reconciling the high-water mark.
func (v *Vault) scanProject(key string) ([]ItemFile, error) {
	all, err := v.scanItems()
	if err != nil {
		return nil, err
	}
	var files []ItemFile
	for _, f := range all {
		if f.Key == key {
			files = append(files, f)
		}
	}
	return files, nil
}

// StrayFiles lists the files under Project key's folder whose names carry
// the prefix of a Project the Vault does not have, such as ZZZ-1 when
// there is no Projects/ZZZ/. They are no Project's Items. They are sorted
// by path.
func (v *Vault) StrayFiles(key string) ([]ItemFile, error) {
	all, err := v.scanItems()
	if err != nil {
		return nil, err
	}
	var files []ItemFile
	for _, f := range all {
		if f.FolderKey() != key || f.Key == key {
			continue
		}
		if ok, err := v.hasProject(f.Key); err != nil {
			return nil, err
		} else if !ok {
			files = append(files, f)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// scanItems lists every file under a Project folder, outside its
// Templates/, whose name carries a <KEY>-<n> prefix, whatever Project the
// prefix names, sorted by number, then path. Folders under Projects/ whose
// names are not Project keys are skipped. The listing is kept until the
// Vault changes the files under Projects/.
func (v *Vault) scanItems() ([]ItemFile, error) {
	if v.items != nil {
		return v.items, nil
	}
	root := filepath.Join(v.Root, ProjectsDir)
	var files []ItemFile
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return err
		}
		rel, _ := filepath.Rel(v.Root, p)
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		if d.IsDir() {
			if len(parts) == 2 && !ValidKey(parts[1]) || len(parts) == 3 && parts[2] == TemplatesDir {
				return fs.SkipDir
			}
			return nil
		}
		if len(parts) < 3 {
			return nil
		}
		if k, n, _, ok := item.ParseFilename(d.Name()); ok {
			files = append(files, ItemFile{Key: k, Number: n, Path: rel})
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
	if files == nil {
		files = []ItemFile{}
	}
	v.items = files
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
	Labels   []string
	// Parent and BlockedBy are relation wikilinks; nil for none.
	Parent    *string
	BlockedBy []string
	Now       time.Time
}

// CreateItem files a new open Item in Project key, which must exist. Its
// number is max(high-water mark, highest number on disk) + 1, and the
// high-water mark is raised to it before the file is written, so the
// number is never issued again even if the write fails. The caller holds
// the Vault lock, which serialises the scan, the allocation and the write.
func (v *Vault) CreateItem(key string, n NewItem) (ItemFile, []byte, error) {
	number, err := v.Allocate(key)
	if err != nil {
		return ItemFile{}, nil, err
	}

	f := ItemFile{Key: key, Number: number}
	now := n.Now.UTC()
	data, err := item.Render(item.Fields{
		ID: f.ID(), Title: n.Title, Kind: n.Kind, Status: item.Open,
		Author: n.Author, Assignee: n.Assignee, Labels: n.Labels,
		Parent: n.Parent, BlockedBy: n.BlockedBy, Created: now, Updated: now,
	}, n.Body)
	if err != nil {
		return ItemFile{}, nil, err
	}
	project := filepath.Join(v.Root, ProjectsDir, key)
	folder := filepath.Join(project, n.Kind.Folder())
	v.items = nil
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
	return f, data, v.recordSnapshot(f.Path, f.Path, nil, data)
}

// Allocate issues the next number of Project key: max(high-water mark,
// highest number on disk) + 1. The high-water mark is raised to it at
// once, so the number is never issued again, even if what it was issued
// for fails. The caller holds the Vault lock, which serialises the scan,
// the allocation and the write that uses it.
func (v *Vault) Allocate(key string) (int, error) {
	files, err := v.scanProject(key)
	if err != nil {
		return 0, err
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
	return number, err
}

// ReadItemFile returns the bytes of an Item file.
func (v *Vault) ReadItemFile(f ItemFile) ([]byte, error) {
	return os.ReadFile(v.abs(f.Path))
}

// WriteItemFile replaces the bytes of an Item file atomically and records
// its snapshot.
func (v *Vault) WriteItemFile(f ItemFile, data []byte) error {
	return v.writeFile(f.Path, data)
}

// writeFile replaces the bytes of the file at the Vault-relative path p
// atomically and, when it is an Item file, records its snapshot.
func (v *Vault) writeFile(p string, data []byte) error {
	prev, err := os.ReadFile(v.abs(p))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	v.items = nil
	if err := fsutil.WriteFile(v.abs(p), data); err != nil {
		return err
	}
	return v.recordSnapshot(p, p, prev, data)
}

// abs is the file-system path of the Vault-relative path p.
func (v *Vault) abs(p string) string { return filepath.Join(v.Root, filepath.FromSlash(p)) }

// ItemAt returns the Item file at the Vault-relative path p, and false
// when p is not an Item file: outside a Project folder, in Templates/,
// without the <KEY>-<n> prefix, misplaced under a prefix no Project has,
// or missing.
func (v *Vault) ItemAt(p string) (ItemFile, bool, error) {
	p = filepath.ToSlash(p)
	if path.IsAbs(p) || p != path.Clean(p) {
		return ItemFile{}, false, nil
	}
	key, n, ok := itemPath(p)
	if !ok {
		return ItemFile{}, false, nil
	}
	if f := (ItemFile{Key: key, Number: n, Path: p}); f.Misplaced() {
		// A misplaced file is an Item of the Project its prefix names,
		// and of none when there is no such Project.
		if ok, err := v.hasProject(key); err != nil || !ok {
			return ItemFile{}, false, err
		}
	}
	fi, err := os.Stat(v.abs(p))
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

// CheckMove fails with a *TargetExistsError when moving Item file f to
// the Vault-relative path to would replace another file. A target that
// is f itself under another name, as a case-only rename finds it on a
// case-insensitive file system, is not another file.
func (v *Vault) CheckMove(f ItemFile, to string) error {
	if to == f.Path {
		return nil
	}
	_, err := os.Lstat(v.abs(to))
	switch {
	case err == nil:
		same, err := v.sameFile(f.Path, to)
		if err != nil || same {
			return err
		}
		return &TargetExistsError{Target: to}
	case errors.Is(err, os.ErrNotExist):
		return nil
	default:
		return err
	}
}

// sameFile reports whether the Vault-relative paths a and b name the same
// file, as two spellings of one name do on a case-insensitive file
// system.
func (v *Vault) sameFile(a, b string) (bool, error) {
	fa, err := os.Lstat(v.abs(a))
	if err != nil {
		return false, err
	}
	fb, err := os.Lstat(v.abs(b))
	if err != nil {
		return false, err
	}
	return os.SameFile(fa, fb), nil
}

// listed reports whether the folder of the Vault-relative path p lists an
// entry spelled exactly as p's name, which a case-insensitive file system
// does not tell from os.Lstat.
func (v *Vault) listed(p string) (bool, error) {
	entries, err := os.ReadDir(filepath.Dir(v.abs(p)))
	if err != nil {
		return false, err
	}
	name := path.Base(p)
	return slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return e.Name() == name }), nil
}
