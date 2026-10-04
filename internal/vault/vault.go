// Package vault opens the Vault on disk: it bootstraps .otman/, holds the
// writer lock, opens the per-device db (ADR 0003), rebuilding it when
// needed and adopting Project folders into its registry, and reads and
// creates Projects.
package vault

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/talvor/otman/internal/fsutil"
	"github.com/talvor/otman/internal/output"
	"golang.org/x/sys/unix"
)

var (
	// ErrNotFound means the Vault directory does not exist.
	ErrNotFound = errors.New("vault directory does not exist")
	// ErrNotDir means the Vault path is not a directory.
	ErrNotDir = errors.New("vault path is not a directory")
	// ErrLockTimeout means another otman command held .otman/lock for
	// longer than the lock timeout.
	ErrLockTimeout = errors.New("timed out waiting for the vault lock")
)

// DefaultLockTimeout is how long a command waits for another otman command
// on this device to release .otman/lock before giving up. otman commands
// hold the lock for well under a second, so a longer wait means a stuck
// or runaway process.
const DefaultLockTimeout = 30 * time.Second

// lockPoll is how often a waiting command retries the lock.
const lockPoll = 25 * time.Millisecond

// StateDir is the per-device folder at the Vault root. It never reaches
// git: otman writes it a .gitignore of "*".
const StateDir = ".otman"

// Vault is an open Vault. It holds the lock on .otman/lock and one db
// connection until Close.
type Vault struct {
	Root string
	lock *os.File
	db   *sql.DB

	// scanned holds the keys of the Projects whose Item files have been
	// listed (see Scanned).
	scanned map[string]bool

	// Fault is a test-only fault injector for journaled operations, such
	// as a retitle: it is called before journal step i with i, and with
	// the number of steps after the last one, and an error from it aborts
	// the operation there, as a crash would, leaving the journal for the
	// next command to finish. nil injects nothing. Open resumes pending
	// journals before the caller can set it, so a resume is never
	// interrupted.
	Fault func(step int) error
}

// Open bootstraps .otman/ if needed, takes the lock, opens the db
// (rebuilding it from the markdown if it is missing or fails quick_check),
// adopts Project folders into it and finishes any pending journal. It is
// the first step of every Vault operation. The warnings report anything
// otman repaired or finished on the way; a journal it cannot finish fails
// with a *JournalConflictError.
//
// Every Vault command holds the lock for its whole run, so a writer's
// scan, allocation and write are serialised against every other otman
// command on this device. A command that cannot take the lock within
// lockTimeout (DefaultLockTimeout when zero) fails with ErrLockTimeout.
func Open(root string, lockTimeout time.Duration) (*Vault, []output.Problem, error) {
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
	if lockTimeout <= 0 {
		lockTimeout = DefaultLockTimeout
	}
	lock, err := acquireLock(filepath.Join(state, "lock"), lockTimeout)
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
// missing, opens the db, adopts Project folders and resumes pending
// journals.
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
	resumed, err := v.resume()
	if err != nil {
		return nil, err
	}
	return append(warnings, resumed...), nil
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
// exclusive flock on it, waiting up to timeout for any other holder.
func acquireLock(path string, timeout time.Duration) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			f.Close()
			return nil, err
		}
		if !time.Now().Before(deadline) {
			f.Close()
			return nil, ErrLockTimeout
		}
		time.Sleep(min(lockPoll, time.Until(deadline)))
	}
}
