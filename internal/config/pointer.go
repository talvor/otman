package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/talvor/otman/internal/fsutil"
)

// PointerFile is the repo pointer's file name. The pointer holds only a
// Project key, never a machine-specific path, so it can be committed.
const PointerFile = ".otman.toml"

// FindPointer returns the nearest repo pointer at or above dir, searching
// up to the git root, or to the filesystem root outside git. It returns ""
// when there is none.
func FindPointer(dir string) (string, error) {
	for d := filepath.Clean(dir); ; {
		p := filepath.Join(d, PointerFile)
		if ok, err := exists(p); err != nil || ok {
			return p, err
		}
		if ok, err := exists(filepath.Join(d, ".git")); err != nil || ok {
			return "", err
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", nil
		}
		d = parent
	}
}

// LinkDir is where project link writes the pointer: the git root above
// dir, or dir itself outside git.
func LinkDir(dir string) (string, error) {
	for d := filepath.Clean(dir); ; {
		if ok, err := exists(filepath.Join(d, ".git")); err != nil || ok {
			return d, err
		}
		parent := filepath.Dir(d)
		if parent == d {
			return filepath.Clean(dir), nil
		}
		d = parent
	}
}

// ReadPointer returns the Project key the pointer at path holds.
func ReadPointer(path string) (string, error) {
	var doc map[string]any
	if _, err := toml.DecodeFile(path, &doc); err != nil {
		var perr toml.ParseError
		if errors.As(err, &perr) {
			return "", &ParseError{path, err}
		}
		return "", err
	}
	key, ok := doc[Project.Name].(string)
	if !ok || key == "" {
		return "", &ParseError{path, fmt.Errorf("%s must be a non-empty string", Project.Name)}
	}
	return key, nil
}

// WritePointer writes a pointer holding only key to path.
func WritePointer(path, key string) error {
	data, err := encode(map[string]any{Project.Name: key})
	if err != nil {
		return err
	}
	return fsutil.WriteFile(path, data)
}

func exists(p string) (bool, error) {
	_, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
