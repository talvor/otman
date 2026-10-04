package cli_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/talvor/otman/internal/cli"
)

// Duplicate numbers, from a second device, a copied file or a git merge,
// and Items hand-moved into another Project's folder are detected and
// repaired (ADR 0003, ADR 0005). The duplicates fixture's Project DUP has
// two Items numbered DUP-1, the later-created first by filename, and two
// numbered DUP-2, created at the same time. The misplaced fixture has Items
// of Project HOM filed in Project AWY's folders, one of them under a KEY no
// Project has.

// dupConfig selects the duplicates fixture's Vault and Project DUP, with an
// actor.
var dupConfig = map[string]string{
	"config/otman/config.toml": "vault = \"$WORK/vault\"\nproject = \"DUP\"\nactor = \"talvor\"\n",
}

// misplacedConfig selects the misplaced fixture's Vault and Project HOM,
// with an actor.
var misplacedConfig = map[string]string{
	"config/otman/config.toml": "vault = \"$WORK/vault\"\nproject = \"HOM\"\nactor = \"talvor\"\n",
}

// Every command that reads a Project holding duplicate numbers warns
// duplicate_number, once per number, naming every file that holds it.
func TestDuplicateWarnings(t *testing.T) {
	runGolden(t, goldenCase{
		name: "duplicate-warnings", fixture: "duplicates", files: dupConfig,
		steps: []step{
			{args: []string{"list"}, tty: true},
			{args: []string{"list", "--json"}},
			{args: []string{"view", "DUP-3"}},
			{args: []string{"frontier", "--json"}},
			{args: []string{"label", "list"}},
			{args: []string{"create", "--title", "Fresh", "--json"}},
			{args: []string{"comment", "DUP-5", "--body", "Noted."}},
			{args: []string{"close", "DUP-5"}},
		},
	})
}

// A bare ambiguous ID or number fails with exit 4, listing the candidate
// paths and suggesting doctor --fix, for reads, writes and relation
// targets alike. A full filename or an exact path still resolves.
func TestDuplicateAmbiguousRef(t *testing.T) {
	runGolden(t, goldenCase{
		name: "duplicate-ambiguous-ref", fixture: "duplicates", files: dupConfig,
		steps: []step{
			{args: []string{"view", "DUP-1"}, tty: true},
			{args: []string{"view", "1", "--json"}},
			{args: []string{"edit", "DUP-2", "--add-label", "bug"}},
			{args: []string{"block", "DUP-5", "--by", "DUP-1"}},
			{args: []string{"view", "DUP-1 First take.md", "--json"}},
			{args: []string{"view", "Projects/DUP/Specs/DUP-2 Tie b.md"}},
			{args: []string{"claim", "DUP-1 Second take.md"}},
		},
	})
}

// doctor reports the later of each pair of duplicates; --fix keeps the
// number on the Item created earlier, whatever the filenames say, and on
// the first by filename when they were created at the same time. The other
// takes the next number from the allocator, above the highest on disk: its
// file is renamed, its id reset, every link to it rewritten through the
// journal, and a comment records the old number. The old → new mapping is
// printed.
func TestDuplicateRenumber(t *testing.T) {
	runGolden(t, goldenCase{
		name: "duplicate-renumber", fixture: "duplicates", files: dupConfig,
		steps: []step{
			{args: []string{"doctor"}, tty: true},
			{args: []string{"doctor", "--json"}},
			{args: []string{"doctor", "--fix"}, tty: true},
			{args: []string{"doctor", "--json"}},
			{args: []string{"list"}},
			{args: []string{"create", "--title", "After the repair"}},
		},
	})
}

// A REF narrows doctor to one Item: the duplicate named by its path is
// renumbered, and the Item that keeps the number has nothing to report.
func TestDuplicateRenumberRef(t *testing.T) {
	runGolden(t, goldenCase{
		name: "duplicate-renumber-ref", fixture: "duplicates", files: dupConfig,
		steps: []step{
			{args: []string{"doctor", "DUP-1 Second take.md"}},
			{args: []string{"doctor", "Projects/DUP/Specs/DUP-2 Tie b.md", "--fix", "--json"}},
		},
	})
}

// An Item without a created timestamp gives way to one that has one, and
// one whose frontmatter cannot be read is reported but never renumbered.
func TestDuplicateRenumberUnreadable(t *testing.T) {
	runGolden(t, goldenCase{
		name: "duplicate-renumber-unreadable", fixture: "duplicates", files: dupConfig,
		steps: []step{
			{write: map[string]string{
				"vault/Projects/DUP/Issues/DUP-5 No date.md": "---\nid: DUP-5\ntitle: No date\nkind: issue\nstatus: open\n---\n<!-- otman:comments -->\n## Comments\n",
				"vault/Projects/DUP/Issues/DUP-3 Flow.md":    "---\n{id: DUP-3, title: Flow}\n---\n",
			}, args: []string{"doctor", "--fix"}},
			{args: []string{"doctor", "--json"}},
		},
	})
}

// An Item filed in another Project's folder is read under the Project its
// filename prefix names, with a misplaced_item warning, and writes to it
// are allowed. Its own Project lists it and resolves links to it.
func TestMisplacedRead(t *testing.T) {
	runGolden(t, goldenCase{
		name: "misplaced-read", fixture: "misplaced", files: misplacedConfig,
		steps: []step{
			{args: []string{"view", "HOM-1"}, tty: true},
			{args: []string{"view", "HOM-4", "--json"}},
			{args: []string{"list", "--all-projects"}, tty: true},
			{args: []string{"view", "Projects/AWY/Issues/HOM-1 Wandered off.md", "--project", "AWY"}},
			{args: []string{"comment", "HOM-1", "--body", "Still here.", "--json"}},
			{args: []string{"edit", "HOM-1", "--title", "Wandered far"}},
			{args: []string{"view", "Projects/AWY/Issues/ZZZ-1 Unknown.md"}},
		},
	})
}

// doctor reports each misplaced Item under its own Project, and the file
// with a KEY no Project has under the Project whose folder holds it.
// --fix moves each misplaced Item into its own Project's Kind folder,
// rewriting the links that name its path. Where its number is taken there,
// the duplicate rules decide which Item is renumbered first: the later
// created, whether it is the misplaced one or not. The unknown KEY stays,
// report-only.
func TestMisplacedFix(t *testing.T) {
	runGolden(t, goldenCase{
		name: "misplaced-fix", fixture: "misplaced", files: misplacedConfig,
		steps: []step{
			{args: []string{"doctor", "--all-projects"}, tty: true},
			{args: []string{"doctor", "--project", "AWY", "--json"}},
			{args: []string{"doctor", "--all-projects", "--fix"}, tty: true},
			{args: []string{"doctor", "--all-projects", "--json"}},
		},
	})
}

// runIn runs one otman command against vault, with Project project and
// fault injected.
func runIn(vault, project string, fault func(int) error, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := cli.Run(cli.Options{
		Args:   append(args, "--vault", vault, "--project", project),
		Env:    []string{"XDG_CONFIG_HOME=" + filepath.Join(filepath.Dir(vault), "config"), "OTM_ACTOR=talvor"},
		Dir:    filepath.Dir(vault),
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Now:    func() time.Time { return fixedNow },
		Fault:  fault,
	})
	return code, stdout.String(), stderr.String()
}

// fixtureVault copies the fixture name to a new temporary Vault.
func fixtureVault(t *testing.T, name string) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "vault")
	copyTree(t, filepath.Join("testdata", "vaults", name), vault)
	return vault
}

// crashResume interrupts the doctor --fix that args run on fixture, in
// Project project, at each journal step boundary, steps of them in all.
// The interrupted run warns repair_blocked and leaves the journal, and
// the next ordinary command finishes it, warns resumed_operation and
// leaves the tree an uninterrupted run leaves. The interrupted run exits 4
// for the finding it left, or 0 when the file had already moved, which
// doctor counts as repaired.
func crashResume(t *testing.T, fixture, project string, steps int, args ...string) {
	t.Helper()
	want := fixtureVault(t, fixture)
	if code, stdout, stderr := runIn(want, project, nil, args...); code != 0 {
		t.Fatalf("uninterrupted run: exit %d: %s%s", code, stdout, stderr)
	}
	wantTree := treeOf(t, want)

	for n := 0; ; n++ {
		vault := fixtureVault(t, fixture)
		fired := false
		code, stdout, stderr := runIn(vault, project, crashAt(n, &fired), args...)
		if !fired {
			if code != 0 {
				t.Fatalf("doctor --fix: exit %d: %s%s", code, stdout, stderr)
			}
			if n != steps {
				t.Fatalf("the repair had %d journal step boundaries, want %d", n, steps)
			}
			break
		}
		t.Run(fmt.Sprintf("step %d", n), func(t *testing.T) {
			if code != 0 && code != 4 || !strings.Contains(stdout, `"code": "repair_blocked"`) ||
				!strings.Contains(stdout, errCrash.Error()) {
				t.Fatalf("interrupted repair: exit %d, stdout %s, stderr %s", code, stdout, stderr)
			}
			if journals, _ := filepath.Glob(filepath.Join(vault, ".otman", "journal", "*.json")); len(journals) != 1 {
				t.Fatalf("pending journals: %v", journals)
			}
			code, stdout, stderr := runIn(vault, project, nil, "list", "--json")
			if code != 0 {
				t.Fatalf("list after the crash: exit %d: %s", code, stderr)
			}
			if !strings.Contains(stdout, `"code": "resumed_operation"`) {
				t.Errorf("list did not warn resumed_operation:\n%s", stdout)
			}
			if got := treeOf(t, vault); got != wantTree {
				t.Errorf("resumed tree differs from an uninterrupted repair\n--- want\n%s\n--- got\n%s", wantTree, got)
			}
			if _, stdout, _ := runIn(vault, project, nil, "list", "--json"); strings.Contains(stdout, "resumed_operation") {
				t.Errorf("a second command resumed again:\n%s", stdout)
			}
		})
	}
}

// A renumber interrupted at any journal step, or after the last, is
// finished by the next command. Its journal renames DUP-1 First take,
// rewrites it with its new id and comment, and rewrites the one Item that
// links to it.
func TestRenumberCrashResume(t *testing.T) {
	crashResume(t, "duplicates", "DUP", 4,
		"doctor", "Projects/DUP/Issues/DUP-1 First take.md", "--fix", "--json")
}

// A misplaced-item move interrupted at any journal step, or after the
// last, is finished by the next command. Its journal moves HOM-1 into
// Project HOM and rewrites the Item that links to it by its old path.
func TestMisplacedMoveCrashResume(t *testing.T) {
	crashResume(t, "misplaced", "HOM", 3, "doctor", "HOM-1", "--fix", "--json")
}
