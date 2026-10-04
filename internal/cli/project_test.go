package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/talvor/otman/internal/cli"
	"golang.org/x/sys/unix"
)

// vaultConfig points the user config at the test's Vault.
var vaultConfig = map[string]string{"config/otman/config.toml": "vault = \"$WORK/vault\"\n"}

// withFiles returns vaultConfig plus extra files.
func withFiles(extra map[string]string) map[string]string { return withConfig(vaultConfig, extra) }

const webNote = "---\nname: otman website\nkind: project\n---\n"

// The first Vault operation creates .otman/ with its .gitignore, lock and
// db. A missing or unconfigured Vault is an error, and nothing is created.
func TestVaultBootstrap(t *testing.T) {
	runGolden(t, goldenCase{
		name: "vault-bootstrap",
		steps: []step{
			{args: []string{"project", "list", "--json"}},
			{args: []string{"project", "list", "--vault", "$WORK/missing", "--json"}},
			{args: []string{"project", "list", "--vault", "$WORK/home/.bashrc"}, tty: true},
			{args: []string{"project", "list", "--vault", "$WORK/vault"}, tty: true},
			{args: []string{"project", "list", "--vault", "$WORK/vault", "--json"}},
		},
		files: map[string]string{"home/.bashrc": ""},
	})
}

// A deleted db is rebuilt from the markdown without a word, and the
// existing Project folders are adopted into it.
func TestVaultRebuildDeletedDB(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "vault-rebuild-deleted",
		fixture: "basic",
		files: withFiles(map[string]string{
			"vault/.otman/.gitignore": "*\n",
			"vault/.otman/lock":       "",
		}),
		steps: []step{{args: []string{"project", "list", "--json"}}},
	})
}

// A db that fails quick_check is replaced, with a db_rebuilt warning on
// the run that rebuilt it only.
func TestVaultRebuildCorruptDB(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "vault-rebuild-corrupt",
		fixture: "basic",
		files: withFiles(map[string]string{
			"vault/.otman/.gitignore": "*\n",
			"vault/.otman/lock":       "",
			"vault/.otman/otman.db":   "this is not a SQLite database\n",
		}),
		steps: []step{
			{args: []string{"project", "list", "--json"}},
			{args: []string{"project", "list"}},
		},
	})
}

func TestProjectCreate(t *testing.T) {
	runGolden(t, goldenCase{
		name:  "project-create",
		files: vaultConfig,
		steps: []step{
			{args: []string{"project", "create", "OTM", "--name", "otman"}, tty: true},
			{args: []string{"project", "create", "OTM", "--name", "other", "--json"}},
			{args: []string{"project", "create", "web", "--name", "website"}},
			{args: []string{"project", "create", "WEB"}, tty: true},
			{args: []string{"project", "create", "WEB", "--name", "two\nlines"}},
			{args: []string{"project", "create", "WEB", "--name", "Web site: v2", "--json"}},
			{args: []string{"project", "list"}, tty: true},
			{args: []string{"project", "list"}},
			{args: []string{"project", "list", "--json"}},
			{args: []string{"project", "view", "OTM"}, tty: true},
			{args: []string{"project", "view", "WEB"}},
			{args: []string{"project", "view", "WEB", "--json"}},
			{args: []string{"project", "view", "NOPE", "--json"}},
			{args: []string{"project", "view", "nope"}},
		},
	})
}

// Project folders already in the Vault are Projects, with or without a
// Project note. Folders that are not Project keys are skipped.
func TestProjectAdopt(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "project-adopt",
		fixture: "basic",
		files: withFiles(map[string]string{
			"vault/Projects/WEB/Issues/WEB-1 Landing page.md": "---\nid: WEB-1\n---\n",
			"vault/Projects/BAD/BAD.md":                       "---\nname: [unterminated\n---\n",
			"vault/Projects/notes/ideas.md":                   "loose notes\n",
			"vault/Projects/README.md":                        "not a Project\n",
		}),
		steps: []step{
			{args: []string{"project", "list", "--json"}},
			{args: []string{"project", "list"}, tty: true},
			{args: []string{"project", "view", "WEB", "--json"}},
			{args: []string{"project", "view", "OTM", "--format", "axi"}},
		},
	})
}

func TestProjectListPaging(t *testing.T) {
	runGolden(t, goldenCase{
		name: "project-paging",
		files: withFiles(map[string]string{
			"vault/Projects/CCC/CCC.md": "---\nname: third\nkind: project\n---\n",
			"vault/Projects/AAA/AAA.md": "---\nname: first\nkind: project\n---\n",
			"vault/Projects/BBB/BBB.md": "---\nname: second\nkind: project\n---\n",
		}),
		steps: []step{
			{args: []string{"project", "list", "--limit", "2"}, tty: true},
			{args: []string{"project", "list", "--limit", "2", "--offset", "2", "--json"}},
			{args: []string{"project", "list", "--offset", "5", "--json"}},
			{args: []string{"project", "list", "--limit", "9223372036854775807", "--offset", "1", "--json"}},
			{args: []string{"project", "list", "--all", "--json"}},
			{args: []string{"project", "list", "--all", "--limit", "1"}},
			{args: []string{"project", "list", "--limit", "0"}},
			{args: []string{"project", "list", "--offset", "-1", "--json"}},
		},
	})
}

// project link writes the pointer at the git root, from any subdirectory,
// or in the working directory outside git. Repointing needs --force.
func TestProjectLink(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "project-link",
		fixture: "basic",
		files: withFiles(map[string]string{
			"vault/Projects/WEB/WEB.md": webNote,
			"repo/.git/HEAD":            "ref: refs/heads/main\n",
			"repo/sub/dir/.keep":        "",
			"plain/sub/.keep":           "",
		}),
		steps: []step{
			{args: []string{"project", "link", "OTM"}, dir: "repo/sub/dir", tty: true},
			{args: []string{"project", "link", "OTM", "--json"}, dir: "repo/sub/dir"},
			{args: []string{"project", "link", "WEB", "--json"}, dir: "repo"},
			{args: []string{"project", "link", "WEB", "--force"}, dir: "repo", tty: true},
			{args: []string{"project", "link", "NOPE", "--force"}, dir: "repo"},
			{args: []string{"project", "link", "web"}, dir: "repo"},
			{args: []string{"project", "link", "OTM", "--json"}, dir: "plain/sub"},
			{args: []string{"config", "show", "--json"}, dir: "repo/sub/dir"},
			{args: []string{"config", "show", "--vault", "/from/flag"}, dir: "repo/sub/dir", tty: true},
			{args: []string{"project", "view", "--json"}, dir: "repo/sub/dir"},
		},
	})
}

// A pointer otman cannot read blocks only the commands that would use it,
// and project link replaces it only with --force.
func TestProjectLinkInvalidPointer(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "project-link-invalid-pointer",
		fixture: "basic",
		files: withFiles(map[string]string{
			"repo/.git/HEAD":      "ref: refs/heads/main\n",
			"repo/.otman.toml":    "project = [\n",
			"other/.otman.toml":   "name = \"OTM\"\n",
			"other/.git/HEAD":     "ref: refs/heads/main\n",
			"other/sub/dir/.keep": "",
		}),
		steps: []step{
			{args: []string{"config", "show", "--json"}, dir: "repo"},
			{args: []string{"project", "view"}, dir: "other/sub/dir"},
			{args: []string{"project", "view", "--json"}, dir: "repo", env: map[string]string{"OTM_PROJECT": "OTM"}},
			{args: []string{"project", "link", "OTM"}, dir: "repo"},
			{args: []string{"project", "link", "OTM", "--force", "--json"}, dir: "repo"},
		},
	})
}

// The selected Project is --project, else OTM_PROJECT, else the nearest
// pointer up to the git root, else config. An unknown or missing Project
// fails; otman never falls back to another Project.
func TestProjectResolution(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "project-resolution",
		fixture: "basic",
		files: map[string]string{
			"config/otman/config.toml":  "vault = \"$WORK/vault\"\nproject = \"OTM\"\n",
			"vault/Projects/WEB/WEB.md": webNote,
			"repo/.git/HEAD":            "ref: refs/heads/main\n",
			"repo/.otman.toml":          "project = \"WEB\"\n",
			"repo/sub/.keep":            "",
			"outer/.otman.toml":         "project = \"WEB\"\n",
			"outer/inner/.git/HEAD":     "ref: refs/heads/main\n",
		},
		steps: []step{
			{args: []string{"config", "show", "--json"}, dir: "repo/sub"},
			{args: []string{"project", "view", "--json"}, dir: "repo/sub"},
			{args: []string{"project", "view", "--json"}, dir: "repo/sub", env: map[string]string{"OTM_PROJECT": "OTM"}},
			{args: []string{"project", "view", "--json", "--project", "WEB"}, dir: "repo/sub", env: map[string]string{"OTM_PROJECT": "OTM"}},
			{args: []string{"config", "show", "--json"}, dir: "outer/inner"},
			{args: []string{"project", "view", "--json"}},
			{args: []string{"project", "view", "--json", "--project", "NOPE"}},
			{args: []string{"project", "view"}, env: map[string]string{"OTM_PROJECT": "lower"}},
		},
	})
}

func TestProjectViewWithoutSelection(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "project-unselected",
		fixture: "basic",
		files:   vaultConfig,
		steps: []step{
			{args: []string{"project", "view"}, tty: true},
			{args: []string{"project", "view", "--json"}},
		},
	})
}

// Concurrent creates of one key on one device are serialised by the lock:
// exactly one succeeds and the rest conflict.
func TestProjectCreateConcurrent(t *testing.T) {
	work := t.TempDir()
	vault := filepath.Join(work, "vault")
	writeFile(t, filepath.Join(vault, ".keep"), "")
	const n = 8
	codes := make([]int, n)
	stderrs := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var stdout, stderr bytes.Buffer
			codes[i] = cli.Run(cli.Options{
				Args:   []string{"project", "create", "OTM", "--name", "otman", "--json", "--vault", vault},
				Env:    []string{"XDG_CONFIG_HOME=" + filepath.Join(work, "config")},
				Dir:    work,
				Stdin:  strings.NewReader(""),
				Stdout: &stdout,
				Stderr: &stderr,
				Now:    func() time.Time { return fixedNow },
			})
			stderrs[i] = stderr.String()
		}()
	}
	wg.Wait()
	created := 0
	for i, c := range codes {
		switch c {
		case cli.ExitOK:
			created++
		case cli.ExitConflict:
			if !strings.Contains(stderrs[i], `"project_exists"`) {
				t.Errorf("run %d: conflict without project_exists: %s", i, stderrs[i])
			}
		default:
			t.Errorf("run %d: exit %d: %s", i, c, stderrs[i])
		}
	}
	if created != 1 {
		t.Errorf("%d runs created the Project, want exactly 1", created)
	}
}

// holdLock takes .otman/lock in vault the way another otman command would,
// and returns its release.
func holdLock(t *testing.T, vault string) func() {
	t.Helper()
	writeFile(t, filepath.Join(vault, ".otman", "lock"), "")
	f, err := os.OpenFile(filepath.Join(vault, ".otman", "lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	return func() { f.Close() }
}

func runVault(vault string, timeout time.Duration, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := cli.Run(cli.Options{
		Args:        append(args, "--vault", vault),
		Env:         []string{"XDG_CONFIG_HOME=" + filepath.Join(filepath.Dir(vault), "config")},
		Dir:         filepath.Dir(vault),
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      &stderr,
		Now:         func() time.Time { return fixedNow },
		LockTimeout: timeout,
	})
	return code, stdout.String(), stderr.String()
}

// A command that cannot take the Vault lock gives up after the lock
// timeout with lock_timeout (exit 4), writing nothing; once the holder
// lets go, the same command succeeds.
func TestLockTimeout(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	release := holdLock(t, vault)

	const timeout = 200 * time.Millisecond
	start := time.Now()
	code, stdout, stderr := runVault(vault, timeout, "project", "create", "OTM", "--name", "otman", "--json")
	waited := time.Since(start)
	if code != cli.ExitConflict {
		t.Fatalf("exit %d, want %d; stderr: %s", code, cli.ExitConflict, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout not empty on failure: %s", stdout)
	}
	if !strings.Contains(stderr, `"code": "lock_timeout"`) || !strings.Contains(stderr, `"timeout": "200ms"`) {
		t.Errorf("stderr lacks lock_timeout with the timeout: %s", stderr)
	}
	if waited < timeout {
		t.Errorf("gave up after %v, before the %v timeout", waited, timeout)
	}
	if _, err := os.Stat(filepath.Join(vault, "Projects", "OTM")); !os.IsNotExist(err) {
		t.Errorf("Project folder written despite the lock timeout (stat err %v)", err)
	}

	release()
	if code, _, stderr := runVault(vault, timeout, "project", "create", "OTM", "--name", "otman", "--json"); code != cli.ExitOK {
		t.Fatalf("after release: exit %d; stderr: %s", code, stderr)
	}
}

// A command waits for a lock released within the timeout rather than
// failing at once.
func TestLockWaitsForRelease(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	release := holdLock(t, vault)
	time.AfterFunc(100*time.Millisecond, release)
	if code, _, stderr := runVault(vault, 10*time.Second, "project", "list", "--json"); code != cli.ExitOK {
		t.Fatalf("exit %d; stderr: %s", code, stderr)
	}
}
