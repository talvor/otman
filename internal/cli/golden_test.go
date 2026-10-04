package cli_test

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/talvor/otman/internal/cli"
)

var update = flag.Bool("update", false, "rewrite golden files")

// fixedNow is the clock every golden run sees.
var fixedNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// step is one otman invocation inside a golden case.
type step struct {
	args  []string
	env   map[string]string // extra environment; values may use $WORK
	stdin string
	tty   bool
	// dir is the working directory, relative to $WORK ("" means $WORK).
	dir string
	// rm lists files to delete before the step, relative to $WORK.
	rm []string
	// fault is the run's fault injector, such as crashAt(n).
	fault func(step int) error
}

// goldenCase copies a testdata/vaults fixture to $WORK/vault, gives the run a
// fresh $WORK/config (XDG_CONFIG_HOME) and $WORK/home (HOME), runs each step
// through cli.Run and compares the transcript and the final tree with
// testdata/golden/<name>.golden.
type goldenCase struct {
	name    string
	fixture string            // directory under testdata/vaults; "" means an empty vault
	config  string            // initial config.toml contents; "" means none
	files   map[string]string // extra files to create, paths relative to $WORK
	steps   []step
}

func runGolden(t *testing.T, gc goldenCase) {
	t.Helper()
	work := t.TempDir()
	vault := filepath.Join(work, "vault")
	if gc.fixture != "" {
		copyTree(t, filepath.Join("testdata", "vaults", gc.fixture), vault)
	} else if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"config", "home"} {
		if err := os.MkdirAll(filepath.Join(work, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if gc.config != "" {
		writeFile(t, filepath.Join(work, "config", "otman", "config.toml"), gc.config)
	}
	for p, c := range gc.files {
		writeFile(t, filepath.Join(work, p), strings.ReplaceAll(c, "$WORK", work))
	}

	var transcript bytes.Buffer
	for _, s := range gc.steps {
		for _, p := range s.rm {
			if err := os.Remove(filepath.Join(work, p)); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&transcript, "$ rm %q\n\n", p)
		}
		env := []string{
			"XDG_CONFIG_HOME=" + filepath.Join(work, "config"),
			"HOME=" + filepath.Join(work, "home"),
		}
		for k, v := range s.env {
			env = append(env, k+"="+strings.ReplaceAll(v, "$WORK", work))
		}
		args := make([]string, len(s.args))
		for i, a := range s.args {
			args[i] = strings.ReplaceAll(a, "$WORK", work)
		}
		var stdout, stderr bytes.Buffer
		code := cli.Run(cli.Options{
			Args:   args,
			Env:    env,
			Dir:    filepath.Join(work, s.dir),
			Stdin:  strings.NewReader(s.stdin),
			Stdout: &stdout,
			Stderr: &stderr,
			Now:    func() time.Time { return fixedNow },
			IsTTY:  s.tty,
			Fault:  s.fault,
		})

		shown := make([]string, len(s.args))
		for i, a := range s.args {
			shown[i] = a
			if a == "" {
				shown[i] = `""`
			}
		}
		fmt.Fprintf(&transcript, "$ otman %s\n", strings.Join(shown, " "))
		if len(s.env) > 0 {
			fmt.Fprintf(&transcript, "env: %s\n", sortedEnv(s.env))
		}
		if s.tty {
			fmt.Fprintf(&transcript, "tty: true\n")
		}
		if s.fault != nil {
			fmt.Fprintf(&transcript, "fault: injected\n")
		}
		fmt.Fprintf(&transcript, "exit: %d\n", code)
		fmt.Fprintf(&transcript, "-- stdout --\n%s", ensureNewline(stdout.String()))
		fmt.Fprintf(&transcript, "-- stderr --\n%s\n", ensureNewline(stderr.String()))
	}
	fmt.Fprintf(&transcript, "== tree ==\n")
	writeTree(t, &transcript, work)

	got := strings.ReplaceAll(transcript.String(), work, "$WORK")
	path := filepath.Join("testdata", "golden", gc.name+".golden")
	if *update {
		writeFile(t, path, got)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run go test -update): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch for %s\n--- want\n%s\n--- got\n%s", path, want, got)
	}
}

func sortedEnv(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	// small n; insertion sort keeps this dependency-free and deterministic
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + env[k]
	}
	return strings.Join(parts, " ")
}

func ensureNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n[no trailing newline]\n"
}

// writeTree lists every file under root (sorted, as WalkDir yields) with its
// contents, so a golden captures the whole resulting file tree.
func writeTree(t *testing.T, w *bytes.Buffer, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			fmt.Fprintf(w, "%s/\n", rel)
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if bytes.HasPrefix(b, []byte(sqliteHeader)) {
			// The db's bytes are not stable or readable; that it is a
			// real SQLite file is what a golden can usefully pin.
			fmt.Fprintf(w, "-- %s --\n[sqlite database]\n", rel)
			return nil
		}
		fmt.Fprintf(w, "-- %s --\n%s", rel, ensureNewline(string(b)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

const sqliteHeader = "SQLite format 3\x00"

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
