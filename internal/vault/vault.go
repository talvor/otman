// Package vault opens the Vault on disk: it bootstraps .otman/, holds the
// writer lock, keeps the per-device db in step with the markdown (ADR 0003)
// and reads and creates Projects.
package vault

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	"github.com/talvor/otman/internal/fsutil"
	"github.com/talvor/otman/internal/output"
	"golang.org/x/sys/unix"
)

var (
	// ErrNotFound means the Vault directory does not exist.
	ErrNotFound = errors.New("vault directory does not exist")
	// ErrNotDir means the Vault path is not a directory.
	ErrNotDir = errors.New("vault path is not a directory")
)

// StateDir is the per-device folder at the Vault root. It never reaches
// git: otman writes it a .gitignore of "*".
const StateDir = ".otman"

// Vault is an open Vault. It holds the lock on .otman/lock and one db
// connection until Close.
type Vault struct {
	Root string
	lock *os.File
	db   *sql.DB
}

// Open bootstraps .otman/ if needed, takes the lock, opens the db
// (rebuilding it from the markdown if it is missing or fails quick_check)
// and adopts Project folders into it. It is the first step of every Vault
// operation. The warnings report anything otman repaired on the way.
//
// Every Vault command holds the lock for its whole run, so a writer's
// scan, allocation and write are serialised against every other otman
// command on this device.
func Open(root string) (*Vault, []output.Problem, error) {
	fi, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if !fi.IsDir() {
		return nil, nil, ErrNotDir
	}
	state := filepath.Join(root, StateDir)
	if err := os.Mkdir(state, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, nil, err
	}
	lock, err := acquireLock(filepath.Join(state, "lock"))
	if err != nil {
		return nil, nil, err
	}
	v := &Vault{Root: root, lock: lock}
	warnings, err := v.bootstrap()
	if err != nil {
		v.Close()
		return nil, nil, err
	}
	return v, warnings, nil
}

// bootstrap runs under the lock: it writes .otman/.gitignore if it is
// missing, opens the db and adopts Project folders.
func (v *Vault) bootstrap() ([]output.Problem, error) {
	state := filepath.Join(v.Root, StateDir)
	gitignore := filepath.Join(state, ".gitignore")
	if _, err := os.Lstat(gitignore); errors.Is(err, os.ErrNotExist) {
		if err := fsutil.WriteFile(gitignore, []byte("*\n")); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	db, warnings, err := openDB(filepath.Join(state, "otman.db"))
	if err != nil {
		return nil, err
	}
	v.db = db
	if err := v.adopt(); err != nil {
		return nil, err
	}
	return warnings, nil
}

// Close releases the db connection and the lock.
func (v *Vault) Close() error {
	var err error
	if v.db != nil {
		err = v.db.Close()
	}
	// Closing the file releases the flock.
	if cerr := v.lock.Close(); err == nil {
		err = cerr
	}
	return err
}

// acquireLock opens (creating if needed) the lock file and takes an
// exclusive flock on it, waiting for any other holder.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
