// Package fsutil holds otman's file-system primitives.
package fsutil

import (
	"os"
	"path/filepath"
)

// WriteFile replaces path with data atomically: it writes a temporary file
// in the same folder, fsyncs it, renames it over path and fsyncs the
// folder, so a crash leaves either the old file or the new one, never a
// truncated one. The folder must exist.
func WriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
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
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
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
