// These tests are a deliberate, known exception to the rule that tests
// never reach into packages or the db schema (spec #14, Testing
// Decisions): they read snapshots below the CLI, through vault.Open and
// Vault.Snapshot, and arrange the db with raw SQL. Snapshots have no CLI
// reader until doctor exists; once it lands, these tests move to the CLI
// seam and read snapshots through it.

package cli_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/talvor/otman/internal/vault"
	_ "modernc.org/sqlite"
)

// snapshotOf opens dir, a Vault, and returns the snapshot of Item
// <key>-<n>, or nil when the db has none.
func snapshotOf(t *testing.T, dir, key string, n int) *vault.Snapshot {
	t.Helper()
	v, _, err := vault.Open(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	s, ok, err := v.Snapshot(key, n)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return nil
	}
	return &s
}

func checkSnapshot(t *testing.T, got *vault.Snapshot, filename, title, kind, folder string) {
	t.Helper()
	if got == nil {
		t.Fatalf("no snapshot, want %s", filename)
	}
	if got.Filename != filename || got.Title == nil || *got.Title != title ||
		got.Kind == nil || string(*got.Kind) != kind || got.Folder != folder {
		t.Errorf("snapshot %s, %v, %v, %s; want %s, %s, %s, %s",
			got.Filename, deref(got.Title), deref(got.Kind), got.Folder, filename, title, kind, folder)
	}
}

func deref[T any](s *T) any {
	if s == nil {
		return nil
	}
	return *s
}

var titleLine = regexp.MustCompile(`(?m)^title: .*$`)

// The db keeps a snapshot of the filename, title, Kind and folder otman
// last wrote for each Item, and every write updates it: create, every
// kind of edit, comment, close, reopen, claim, release, parent, block, the
// link rewrites of a retitle and a resumed journal. An Item otman has
// never written has none.
func TestSnapshots(t *testing.T) {
	dir := retitleVault(t)
	if s := snapshotOf(t, dir, "RET", 2); s != nil {
		t.Fatalf("RET-2 has a snapshot before otman wrote it: %+v", *s)
	}
	run := func(args ...string) {
		t.Helper()
		if code, _, stderr := runFault(dir, nil, append(args, "--actor", "agent-a")...); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, stderr)
		}
	}

	run("create", "--title", "Fresh one")
	checkSnapshot(t, snapshotOf(t, dir, "RET", 5), "RET-5 Fresh one.md", "Fresh one", "issue", "Projects/RET/Issues")
	run("edit", "RET-5", "--kind", "spec")
	checkSnapshot(t, snapshotOf(t, dir, "RET", 5), "RET-5 Fresh one.md", "Fresh one", "spec", "Projects/RET/Specs")

	// A hand edit of the title before each write is not absorbed: the write
	// changed no title, so the snapshot keeps the title otman last wrote.
	file := filepath.Join(dir, "Projects", "RET", "Specs", "RET-5 Fresh one.md")
	for _, args := range [][]string{
		{"comment", "RET-5", "--body", "A comment."},
		{"close", "RET-5"},
		{"reopen", "RET-5"},
		{"claim", "RET-5"},
		{"release", "RET-5"},
		{"edit", "RET-5", "--assignee", "someone"},
		{"edit", "RET-5", "--add-label", "bug"},
		{"edit", "RET-5", "--body", "A body."},
		{"parent", "set", "RET-5", "RET-3"},
		{"block", "RET-5", "--by", "RET-4"},
	} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		hand := "By hand before " + args[0]
		if err := os.WriteFile(file, titleLine.ReplaceAll(b, []byte("title: "+hand)), 0o644); err != nil {
			t.Fatal(err)
		}
		run(args...)
		checkSnapshot(t, snapshotOf(t, dir, "RET", 5), "RET-5 Fresh one.md", "Fresh one", "spec", "Projects/RET/Specs")
	}

	run(retitleArgs...)
	checkSnapshot(t, snapshotOf(t, dir, "RET", 1), "RET-1 New title rewritten.md", "New title: rewritten", "issue", "Projects/RET/Issues")
	checkSnapshot(t, snapshotOf(t, dir, "RET", 2), "RET-2 Every link form.md", "Every link form", "issue", "Projects/RET/Issues")

	if code, _, stderr := runFault(dir, crashAt(0, nil), "edit", "RET-3", "--title", "Code links"); code != 1 {
		t.Fatalf("interrupted retitle: exit %d: %s", code, stderr)
	}
	run("list")
	checkSnapshot(t, snapshotOf(t, dir, "RET", 3), "RET-3 Code links.md", "Code links", "issue", "Projects/RET/Issues")
}

// A crash can land between a journal step's file change and the snapshot
// it records. The resume that finds the step already applied records the
// snapshot then, so the snapshot still holds what otman wrote.
func TestSnapshotsResumeAppliedSteps(t *testing.T) {
	dir := retitleVault(t)
	if code, _, stderr := runFault(dir, crashAt(retitleSteps, nil), retitleArgs...); code != 1 {
		t.Fatalf("interrupted retitle: exit %d: %s", code, stderr)
	}
	// Every step was applied, but none of their snapshots reached the db.
	db, err := sql.Open("sqlite", filepath.Join(dir, ".otman", "otman.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM snapshots"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if code, stdout, stderr := runFault(dir, nil, "list", "--json"); code != 0 || !strings.Contains(stdout, "resumed_operation") {
		t.Fatalf("list after the crash: exit %d, stdout %s, stderr %s", code, stdout, stderr)
	}
	checkSnapshot(t, snapshotOf(t, dir, "RET", 1), "RET-1 New title rewritten.md", "New title: rewritten", "issue", "Projects/RET/Issues")
	checkSnapshot(t, snapshotOf(t, dir, "RET", 2), "RET-2 Every link form.md", "Every link form", "issue", "Projects/RET/Issues")
}

// A db from before snapshots (schema version 1) is migrated, not rebuilt:
// it keeps its high-water mark, so a deleted Item's number is still never
// reused, and snapshots are recorded from then on.
func TestSnapshotsMigrateOldDB(t *testing.T) {
	dir := retitleVault(t)
	run := func(args ...string) string {
		t.Helper()
		code, stdout, stderr := runFault(dir, nil, append(args, "--json")...)
		if code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, stderr)
		}
		return stdout
	}
	run("create", "--title", "Soon deleted")
	if err := os.Remove(filepath.Join(dir, "Projects", "RET", "Issues", "RET-5 Soon deleted.md")); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, ".otman", "otman.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE snapshots; PRAGMA user_version = 1;"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	out := run("create", "--title", "After the upgrade")
	if strings.Contains(out, "db_rebuilt") {
		t.Errorf("the old db was rebuilt rather than migrated:\n%s", out)
	}
	if !strings.Contains(out, `"id": "RET-6"`) {
		t.Errorf("the migrated db lost its high-water mark:\n%s", out)
	}
	checkSnapshot(t, snapshotOf(t, dir, "RET", 6), "RET-6 After the upgrade.md", "After the upgrade", "issue", "Projects/RET/Issues")
}
