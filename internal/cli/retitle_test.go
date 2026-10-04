package cli_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/talvor/otman/internal/cli"
)

// edit --title updates title, and the title in aliases, and renames the
// file to the new projected filename, keeping its folder.
func TestRetitle(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-retitle",
		fixture: "items",
		files:   withOTM(nil),
		steps: []step{
			{args: []string{"edit", "OTM-1", "--title", "Detect: sync collisions?"}, tty: true},
			{args: []string{"view", "OTM-1", "--json"}},
		},
	})
}

// retConfig selects the retitle fixture Vault and Project RET. In the
// fixture every other note links to RET-1 Old title: RET-2 in every link
// form and in its relations, RET-3 inside and outside code, RET-4 by path,
// and notes in another Project, outside Projects and in a hidden folder.
var retConfig = map[string]string{
	"config/otman/config.toml": "vault = \"$WORK/vault\"\nproject = \"RET\"\n",
}

// withRET returns retConfig plus extra files.
func withRET(extra map[string]string) map[string]string { return withConfig(retConfig, extra) }

// A retitle rewrites every wikilink to the Item across the Vault, in
// frontmatter relations and in bodies and comments, including the Item's
// own. Only the target changes: the alias, heading, block reference,
// embed marker and a table's escaped \| are kept, a ".md" extension is
// kept, and a path keeps its form, naming the new filename. Links inside
// fenced, indented and inline code, in frontmatter keys that are not
// relations, in hidden folders and to other notes are left alone, and so
// are the linking files' updated times.
func TestRetitleLinks(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-retitle-links",
		fixture: "retitle",
		files:   withRET(nil),
		steps: []step{
			{args: []string{"edit", "RET-1", "--title", "New title: rewritten", "--json"}},
			{args: []string{"view", "RET-2", "--json"}},
		},
	})
}

// A Kind change moves the file to the new Kind's folder through the same
// journal, rewriting the links that name it by path; links by name still
// resolve and are left alone. A retitle with a Kind change does both.
func TestRetitleKind(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-retitle-kind",
		fixture: "retitle",
		files:   withRET(nil),
		steps: []step{
			{args: []string{"edit", "RET-1", "--kind", "prd", "--json"}},
			{args: []string{"view", "RET-4", "--json"}},
			{args: []string{"edit", "RET-1", "--title", "Moved and renamed", "--kind", "spec"}, tty: true},
		},
	})
}

// longTitle is cut at a word boundary to "Detect duplicate numbers when two
// devices sync the same" in the filename.
const longTitle = "Detect duplicate numbers when two devices sync the same Vault folder overnight"

// A retitle that changes the part of a long title the filename carries
// renames the file and rewrites the links; one that changes only the part
// the filename cuts off keeps the filename, so nothing is renamed and no
// link is rewritten.
func TestRetitleTruncated(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-retitle-truncated",
		fixture: "retitle",
		files: withRET(map[string]string{
			"vault/Projects/RET/Issues/RET-5 Detect duplicate numbers when two devices sync the same.md": "---\nid: RET-5\ntitle: " +
				longTitle + "\naliases:\n  - " + longTitle + "\nkind: issue\nstatus: open\n---\n<!-- otman:comments -->\n## Comments\n",
			"vault/Projects/RET/Issues/RET-6 Links to the long one.md": relationItem("RET-6", "Links to the long one",
				`"[[RET-5 Detect duplicate numbers when two devices sync the same]]"`, "[]"),
		}),
		steps: []step{
			{args: []string{"edit", "RET-5", "--title", "Detect duplicate numbers when three devices sync the same Vault folder overnight", "--json"}},
			{args: []string{"edit", "RET-5", "--title", "Detect duplicate numbers when three devices sync the same Vault folder at once", "--json"}},
		},
	})
}

// A blank or multi-line title fails with exit 2, and a retitle to the
// current title is a no-op. A rename onto an existing file fails with
// move_target_exists, and one that would have to rewrite a link in
// frontmatter otman cannot splice fails with unsafe_write naming that
// file. Nothing is written by a failed retitle.
func TestRetitleErrors(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-retitle-errors",
		fixture: "retitle",
		files: withRET(map[string]string{
			"vault/Projects/RET/Issues/RET-5 Free.md":       relationItem("RET-5", "Free", "null", "[]"),
			"vault/Projects/RET/Issues/RET-5 Taken.md/keep": "a folder in the way of the rename\n",
			"vault/Projects/RET/Issues/RET-6 Flow style.md": "---\n{id: RET-6, title: Flow style, kind: issue, status: open, parent: \"[[RET-1 Old title]]\"}\n---\n<!-- otman:comments -->\n## Comments\n",
		}),
		steps: []step{
			{args: []string{"edit", "RET-1", "--title", ""}, tty: true},
			{args: []string{"edit", "RET-1", "--title", "  ", "--json"}},
			{args: []string{"edit", "RET-1", "--title", "Two\nlines"}},
			{args: []string{"edit", "RET-1", "--title", "Old title", "--json"}},
			{args: []string{"edit", "RET-5", "--title", "Taken", "--json"}},
			{args: []string{"edit", "RET-1", "--title", "Blocked by a flow-style link", "--json"}},
			{args: []string{"edit", "RET-1", "--title", "Blocked by a flow-style link"}, tty: true},
		},
	})
}

// errCrash is the failure crashAt injects.
var errCrash = errors.New("injected crash")

// crashAt is a fault injector that aborts a journaled operation at journal
// step n, as a crash would. *fired, when fired is not nil, records whether
// it did.
func crashAt(n int, fired *bool) func(int) error {
	return func(step int) error {
		if step != n {
			return nil
		}
		if fired != nil {
			*fired = true
		}
		return fmt.Errorf("%w at journal step %d", errCrash, n)
	}
}

// runFault runs one otman command against vault, with Project RET and
// fault injected.
func runFault(vault string, fault func(int) error, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := cli.Run(cli.Options{
		Args:   append(args, "--vault", vault, "--project", "RET"),
		Env:    []string{"XDG_CONFIG_HOME=" + filepath.Join(filepath.Dir(vault), "config")},
		Dir:    filepath.Dir(vault),
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Now:    func() time.Time { return fixedNow },
		Fault:  fault,
	})
	return code, stdout.String(), stderr.String()
}

// retitleVault copies the retitle fixture to a new temporary Vault.
func retitleVault(t *testing.T) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "vault")
	copyTree(t, filepath.Join("testdata", "vaults", "retitle"), vault)
	return vault
}

// treeOf is the whole file tree under vault, as a golden shows it.
func treeOf(t *testing.T, vault string) string {
	t.Helper()
	var b bytes.Buffer
	writeTree(t, &b, vault)
	return b.String()
}

// retitleArgs retitles RET-1 in the retitle fixture. Its journal has a
// rename, a write of RET-1 and a write of each of the seven other notes
// that link to it.
var retitleArgs = []string{"edit", "RET-1", "--title", "New title: rewritten", "--json"}

const retitleSteps = 9

// A retitle interrupted at any journal step, or after the last one, is
// finished by the next ordinary command, which warns resumed_operation:
// the final tree is the one an uninterrupted retitle leaves.
func TestRetitleCrashResume(t *testing.T) {
	want := retitleVault(t)
	if code, _, stderr := runFault(want, nil, retitleArgs...); code != 0 {
		t.Fatalf("uninterrupted retitle: exit %d: %s", code, stderr)
	}
	wantTree := treeOf(t, want)

	for n := 0; ; n++ {
		vault := retitleVault(t)
		fired := false
		code, stdout, stderr := runFault(vault, crashAt(n, &fired), retitleArgs...)
		if !fired {
			if code != 0 {
				t.Fatalf("retitle: exit %d: %s", code, stderr)
			}
			if n != retitleSteps+1 {
				t.Fatalf("the retitle had %d journal step boundaries, want %d", n, retitleSteps+1)
			}
			break
		}
		t.Run(fmt.Sprintf("step %d", n), func(t *testing.T) {
			if code != 1 || stdout != "" || !strings.Contains(stderr, errCrash.Error()) {
				t.Fatalf("interrupted retitle: exit %d, stdout %q, stderr %s", code, stdout, stderr)
			}
			if journals, _ := filepath.Glob(filepath.Join(vault, ".otman", "journal", "*.json")); len(journals) != 1 {
				t.Fatalf("pending journals: %v", journals)
			}
			code, stdout, stderr := runFault(vault, nil, "list", "--json")
			if code != 0 {
				t.Fatalf("list after the crash: exit %d: %s", code, stderr)
			}
			if !strings.Contains(stdout, `"code": "resumed_operation"`) {
				t.Errorf("list did not warn resumed_operation:\n%s", stdout)
			}
			if got := treeOf(t, vault); got != wantTree {
				t.Errorf("resumed tree differs from an uninterrupted retitle\n--- want\n%s\n--- got\n%s", wantTree, got)
			}
			// The journal is finished: the next command warns nothing.
			if _, stdout, _ := runFault(vault, nil, "list", "--json"); strings.Contains(stdout, "resumed_operation") {
				t.Errorf("a second command resumed again:\n%s", stdout)
			}
		})
	}
}

// The resumed_operation warning in each format: three retitles crash at
// different journal steps, and the next command finishes each.
func TestRetitleResumeWarning(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-retitle-resume",
		fixture: "retitle",
		files:   withRET(nil),
		steps: []step{
			{args: []string{"edit", "RET-1", "--title", "New title: rewritten"}, tty: true, fault: crashAt(1, nil)},
			{args: []string{"view", "RET-3"}, tty: true},
			{args: []string{"edit", "RET-3", "--title", "Code links", "--json"}, fault: crashAt(0, nil)},
			{args: []string{"list", "--json"}},
			{args: []string{"edit", "RET-3", "--title", "Links in code", "--json"}, fault: crashAt(3, nil)},
			{args: []string{"list"}},
		},
	})
}

// A resume that finds a file a pending step would rewrite changed since
// the operation began stops with unsafe_write, naming the file and the
// journal, and every command fails the same way until the file is
// restored or the journal deleted. Steps before it are applied; nothing
// after it is.
func TestRetitleResumeUnsafeWrite(t *testing.T) {
	vault := retitleVault(t)
	// Steps run in Vault order: rename, write RET-1, then the linking
	// notes, RET-3 at step 6.
	if code, _, stderr := runFault(vault, crashAt(5, nil), retitleArgs...); code != 1 {
		t.Fatalf("interrupted retitle: exit %d: %s", code, stderr)
	}
	ret3 := filepath.Join(vault, "Projects", "RET", "Issues", "RET-3 Links in code.md")
	original, err := os.ReadFile(ret3)
	if err != nil {
		t.Fatal(err)
	}
	handEdited := append(bytes.Clone(original), "A hand edit.\n"...)
	if err := os.WriteFile(ret3, handEdited, 0o644); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		code, stdout, stderr := runFault(vault, nil, "list", "--json")
		if code != 4 || stdout != "" {
			t.Fatalf("list: exit %d, stdout %q, stderr %s", code, stdout, stderr)
		}
		for _, want := range []string{`"code": "unsafe_write"`, `"path": "Projects/RET/Issues/RET-3 Links in code.md"`,
			`"journal": ".otman/journal/retitle-RET-1.json"`} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr lacks %s:\n%s", want, stderr)
			}
		}
	}
	if after, _ := os.ReadFile(ret3); !bytes.Equal(after, handEdited) {
		t.Errorf("the resume overwrote the hand edit:\n%s", after)
	}
	ret2, err := os.ReadFile(filepath.Join(vault, "Projects", "RET", "Issues", "RET-2 Every link form.md"))
	if err != nil || !strings.Contains(string(ret2), "[[RET-1 New title rewritten]]") {
		t.Errorf("the step before the conflict was not applied: %v\n%s", err, ret2)
	}
	ret4, err := os.ReadFile(filepath.Join(vault, "Projects", "RET", "Specs", "RET-4 Path links.md"))
	if err != nil || strings.Contains(string(ret4), "New title") {
		t.Errorf("a step after the conflict was applied: %v\n%s", err, ret4)
	}

	// Restoring the file lets the next command finish the journal.
	if err := os.WriteFile(ret3, original, 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runFault(vault, nil, "list", "--json")
	if code != 0 || !strings.Contains(stdout, "resumed_operation") {
		t.Fatalf("list after restoring: exit %d, stdout %s, stderr %s", code, stdout, stderr)
	}
	want := retitleVault(t)
	runFault(want, nil, retitleArgs...)
	if got, wantTree := treeOf(t, vault), treeOf(t, want); got != wantTree {
		t.Errorf("tree differs from an uninterrupted retitle\n--- want\n%s\n--- got\n%s", wantTree, got)
	}
}

// Deleting a journal that cannot be finished abandons the rest of its
// operation: the next command runs, and what was applied stays.
func TestRetitleAbandonJournal(t *testing.T) {
	vault := retitleVault(t)
	if code, _, stderr := runFault(vault, crashAt(5, nil), retitleArgs...); code != 1 {
		t.Fatalf("interrupted retitle: exit %d: %s", code, stderr)
	}
	ret3 := filepath.Join(vault, "Projects", "RET", "Issues", "RET-3 Links in code.md")
	if err := os.WriteFile(ret3, []byte("Replaced by hand.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runFault(vault, nil, "list", "--json"); code != 4 {
		t.Fatalf("list with an unfinishable journal: exit %d", code)
	}
	if err := os.Remove(filepath.Join(vault, ".otman", "journal", "retitle-RET-1.json")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runFault(vault, nil, "view", "RET-1", "--json")
	if code != 0 || strings.Contains(stdout, "resumed_operation") || !strings.Contains(stdout, "RET-1 New title rewritten.md") {
		t.Fatalf("view after abandoning: exit %d, stdout %s, stderr %s", code, stdout, stderr)
	}
}

// issuesListing is the names the RET Issues folder of vault lists,
// spelled as the folder stores them.
func issuesListing(t *testing.T, vault string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(vault, "Projects", "RET", "Issues"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// A retitle that changes only the case of the filename renames the file
// on any file system. On a case-insensitive one, such as macOS's, the old
// and new names reach the same file, so the target is not taken: the
// file is renamed so its name changes case. Here a hard link stands in
// for the second name a case-insensitive file system gives the file, so
// the test means the same on a case-sensitive one. An interrupted
// case-only retitle resumes the same way.
func TestRetitleCaseOnly(t *testing.T) {
	const oldName, newName = "RET-1 Old title.md", "RET-1 Old Title.md"
	checkRenamed := func(t *testing.T, vault string) {
		t.Helper()
		names := issuesListing(t, vault)
		if !slices.Contains(names, newName) || slices.Contains(names, oldName) {
			t.Errorf("Issues lists %q, want %q in place of %q", names, newName, oldName)
		}
		b, err := os.ReadFile(filepath.Join(vault, "Projects", "RET", "Issues", newName))
		if err != nil || !strings.Contains(string(b), "\ntitle: Old Title\n") {
			t.Errorf("the renamed file does not carry the new title: %v\n%s", err, b)
		}
	}
	args := []string{"edit", "Projects/RET/Issues/" + oldName, "--title", "Old Title", "--json"}

	t.Run("case only", func(t *testing.T) {
		vault := retitleVault(t)
		if code, _, stderr := runFault(vault, nil, args...); code != 0 {
			t.Fatalf("case-only retitle: exit %d: %s", code, stderr)
		}
		checkRenamed(t, vault)
	})

	// sameFileVault is the retitle fixture with the new name linked to
	// the old one's file.
	sameFileVault := func(t *testing.T) string {
		t.Helper()
		vault := retitleVault(t)
		issues := filepath.Join(vault, "Projects", "RET", "Issues")
		if err := os.Link(filepath.Join(issues, oldName), filepath.Join(issues, newName)); err != nil {
			t.Skipf("cannot hard-link: %v", err)
		}
		return vault
	}
	t.Run("same file", func(t *testing.T) {
		vault := sameFileVault(t)
		if code, _, stderr := runFault(vault, nil, args...); code != 0 {
			t.Fatalf("case-only retitle: exit %d: %s", code, stderr)
		}
		checkRenamed(t, vault)
	})
	for n := 0; n <= 2; n++ {
		t.Run(fmt.Sprintf("same file, resumed from step %d", n), func(t *testing.T) {
			vault := sameFileVault(t)
			fired := false
			if code, _, stderr := runFault(vault, crashAt(n, &fired), args...); code != 1 || !fired {
				t.Fatalf("interrupted retitle: exit %d: %s", code, stderr)
			}
			code, stdout, stderr := runFault(vault, nil, "list", "--json")
			if code != 0 || !strings.Contains(stdout, `"code": "resumed_operation"`) {
				t.Fatalf("list after the crash: exit %d, stdout %s, stderr %s", code, stdout, stderr)
			}
			checkRenamed(t, vault)
		})
	}
}
