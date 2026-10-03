// Package fsutil holds otman's file-system primitives.
package fsutil

import (
	"errors"
	"os"
	"path/filepath"
)

// WriteFile replaces path with data atomically: it writes a temporary file
// in the same folder, fsyncs it, renames it over path and fsyncs the
// folder, so a crash leaves either the old file or the new one, never a
// truncated one. A replaced file keeps its permission bits; a new file
// gets 0644. The folder must exist.
func WriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".otman-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return SyncDir(dir)
}

// SyncDir fsyncs a folder, making renames and creations in it durable.
func SyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		d.Close()
		return err
	}
	return d.Close()
}
