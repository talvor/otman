package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileNewFileIs0644(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.md")
	if err := WriteFile(path, []byte("a\n")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o644 {
		t.Errorf("mode = %o, want 644", got)
	}
}

func TestWriteFileKeepsExistingMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o664} {
		path := filepath.Join(t.TempDir(), "item.md")
		if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if err := WriteFile(path, []byte("new\n")); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != mode {
			t.Errorf("mode = %o, want %o", got, mode)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "new\n" {
			t.Errorf("data = %q, want %q", data, "new\n")
		}
	}
}
