package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/talvor/otman/internal/item"
)

// edit changes the Kind, body and assignee with explicit delta flags. A
// Kind change moves the file to the new Kind's folder and keeps the body.
// A body edit replaces only the main body and keeps the comments section.
// The assignee can be set or cleared at any status, overriding any
// existing one. An edit that changes nothing reports changed:false and
// leaves the file alone. A matching --if-rev lets the edit through.
func TestEdit(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-edit",
		fixture: "items",
		files: withOTM(map[string]string{
			"body.md": "Body from a file.\r\nSecond line.\n\n\n",
		}),
		steps: []step{
			{args: []string{"edit", "OTM-1", "--kind", "spec"}, tty: true},
			{args: []string{"edit", "OTM-1", "--kind", "spec", "--json"}},
			{args: []string{"edit", "OTM-1", "--body", "A new body.\n\nWith two paragraphs.", "--json"}},
			{args: []string{"edit", "OTM-1", "--body", "A new body.\n\nWith two paragraphs."}, tty: true},
			{args: []string{"edit", "OTM-1", "--body-file", "body.md"}},
			{args: []string{"edit", "OTM-1", "--body-file", "-", "--json"}, stdin: "From stdin.\n"},
			{args: []string{"edit", "OTM-1", "--clear-body", "--json"}},
			{args: []string{"edit", "OTM-1", "--clear-body", "--json"}},
			{args: []string{"edit", "OTM-1", "--assignee", "@me", "--json"}, env: map[string]string{"OTM_ACTOR": "talvor"}},
			{args: []string{"edit", "OTM-1", "--assignee", "talvor"}},
			{args: []string{"edit", "OTM-1", "--clear-assignee", "--json"}},
			{args: []string{"edit", "OTM-2", "--assignee", "agent-c", "--json"}},
			{args: []string{"edit", "OTM-2", "--clear-assignee"}, tty: true},
			{args: []string{"edit", "WEB-1", "--kind", "issue", "--body", "Moved and rewritten.", "--assignee", "agent-a", "--if-rev", "500e0146a3ff", "--json"}},
		},
	})
}

// Contradictory flags, an empty edit and invalid values fail with exit 2
// before the Vault is touched; a stale --if-rev fails with exit 4
// stale_item. A body edit of an Item without a comments marker is refused
// with unsafe_write, and a Kind move onto an existing file with
// move_target_exists. Nothing is written by a failed edit, even one that
// fails after the Vault is opened.
func TestEditErrors(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-edit-errors",
		fixture: "items",
		files: withOTM(map[string]string{
			"vault/Projects/OTM/Issues/OTM-3 No marker.md":      "---\nid: OTM-3\ntitle: No marker\nkind: issue\nstatus: open\n---\nBody.\n\n## Comments\n",
			"vault/Projects/OTM/Specs/OTM-4 In the way.md/keep": "a folder in the way of the move\n",
			"vault/Projects/OTM/Issues/OTM-4 In the way.md":     relationItem("OTM-4", "In the way", "null", "[]"),
		}),
		steps: []step{
			{args: []string{"edit", "OTM-1"}, tty: true},
			{args: []string{"edit", "OTM-1", "--if-rev", "0123456789ab", "--json"}},
			{args: []string{"edit", "--kind", "spec"}},
			{args: []string{"edit", "OTM-1", "--body", "x", "--clear-body", "--json"}},
			{args: []string{"edit", "OTM-1", "--body", "x", "--body-file", "-"}},
			{args: []string{"edit", "OTM-1", "--body-file", "-", "--clear-body", "--kind", "spec"}},
			{args: []string{"edit", "OTM-1", "--assignee", "x", "--clear-assignee", "--json"}},
			{args: []string{"edit", "OTM-1", "--kind", "task"}},
			{args: []string{"edit", "OTM-1", "--kind", "spec", "--body", "a\n<!-- otman:comments -->\nb", "--json"}},
			{args: []string{"edit", "OTM-1", "--kind", "spec", "--body-file", "missing.md"}},
			{args: []string{"edit", "OTM-1", "--kind", "spec", "--assignee", "@me", "--json"}},
			{args: []string{"edit", "OTM-1", "--kind", "spec", "--assignee", ""}},
			{args: []string{"edit", "OTM-1", "--kind", "spec", "--if-rev", ""}},
			{args: []string{"edit", "OTM-1", "--kind", "spec", "--if-rev", "0123456789ab", "--json"}},
			{args: []string{"edit", "OTM-1", "--assignee", "agent-b", "--if-rev", "0123456789ab"}},
			{args: []string{"edit", "OTM-99", "--kind", "spec", "--json"}},
			{args: []string{"edit", "OTM-3", "--body", "New.", "--json"}},
			{args: []string{"edit", "OTM-3", "--clear-body", "--kind", "spec"}, tty: true},
			{args: []string{"edit", "OTM-4", "--kind", "spec", "--json"}},
		},
	})
}

// Edits splice only the keys they change into hand-edited frontmatter and
// keep its line endings, including in a rewritten body. Frontmatter otman
// cannot splice safely is refused with unsafe_write.
func TestEditHandEdited(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-edit-hand-edited",
		fixture: "handedited",
		files:   map[string]string{"config/otman/config.toml": "vault = \"$WORK/vault\"\nproject = \"HND\"\n"},
		steps: []step{
			{args: []string{"edit", "HND-1", "--assignee", "agent-c", "--body", "Rewritten.", "--json"}},
			{args: []string{"edit", "HND-2", "--kind", "prd", "--json"}},
			{args: []string{"edit", "HND-3", "--body", "Line one.\nLine two.", "--assignee", "agent-c", "--json"}},
			{args: []string{"edit", "HND-6", "--clear-assignee", "--json"}},
			{args: []string{"edit", "HND-8", "--clear-body", "--json"}},
		},
	})
}

// --if-rev fails with stale_item when the file changed since it was read,
// including by a hand edit, and the edit writes nothing.
func TestEditIfRevAfterHandEdit(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	copyTree(t, filepath.Join("testdata", "vaults", "items"), vault)
	file := filepath.Join(vault, "Projects", "OTM", "Issues", "OTM-1 Handle sync collisions.md")
	rev := viewRev(t, vault, "OTM-1")

	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := item.Rev(original); got != rev {
		t.Fatalf("view reported rev %s, the file's is %s", rev, got)
	}
	handEdited := bytes.Replace(original, []byte("Detect duplicate"), []byte("Detect all duplicate"), 1)
	if err := os.WriteFile(file, handEdited, 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runVault(vault, 0, "edit", "OTM-1", "--project", "OTM",
		"--body", "Overwrite.", "--if-rev", rev, "--json")
	if code != 4 || stdout != "" || !strings.Contains(stderr, `"stale_item"`) {
		t.Fatalf("exit %d, stdout %q, stderr %s", code, stdout, stderr)
	}
	if after, _ := os.ReadFile(file); !bytes.Equal(after, handEdited) {
		t.Fatalf("a stale edit changed the file:\n%s", after)
	}

	code, _, stderr = runVault(vault, 0, "edit", "OTM-1", "--project", "OTM",
		"--body", "Overwrite.", "--if-rev", viewRev(t, vault, "OTM-1"), "--json")
	if code != 0 {
		t.Fatalf("an edit at the current rev failed: exit %d: %s", code, stderr)
	}
}

func viewRev(t *testing.T, vault, ref string) string {
	t.Helper()
	code, stdout, stderr := runVault(vault, 0, "view", ref, "--project", "OTM", "--json")
	if code != 0 {
		t.Fatalf("view: exit %d: %s", code, stderr)
	}
	var res struct {
		Data struct {
			Item struct {
				Rev string `json:"rev"`
			} `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatal(err)
	}
	return res.Data.Item.Rev
}

// The byte-level diff of an edit: frontmatter edits change only the
// touched keys and updated, and a body edit leaves the frontmatter as the
// splice made it and the comments section byte for byte.
func TestEditByteDiff(t *testing.T) {
	cases := []struct {
		name string
		args []string
		keys []string // the frontmatter keys the edit may change
		body bool     // whether the edit may change the body
	}{
		{"kind", []string{"--kind", "prd"}, []string{"kind"}, false},
		{"assignee", []string{"--assignee", "agent-z"}, []string{"assignee"}, false},
		{"clear assignee", []string{"--clear-assignee"}, []string{"assignee"}, false},
		{"body", []string{"--body", "Replaced."}, nil, true},
		{"clear body", []string{"--clear-body"}, nil, true},
		{"all", []string{"--kind", "spec", "--assignee", "x", "--body", "B"}, []string{"kind", "assignee"}, true},
	}
	for _, fx := range []struct{ fixture, rel string }{
		{"items", "Projects/OTM/Issues/OTM-1 Handle sync collisions.md"},
		{"handedited", "Projects/HND/Issues/HND-1 Comments and blank lines.md"},
		{"handedited", "Projects/HND/Issues/HND-3 Windows line endings.md"},
	} {
		for _, c := range cases {
			t.Run(fx.fixture+"/"+idOf(fx.rel)+"/"+c.name, func(t *testing.T) {
				vault := filepath.Join(t.TempDir(), "vault")
				copyTree(t, filepath.Join("testdata", "vaults", fx.fixture), vault)
				before, err := os.ReadFile(filepath.Join(vault, filepath.FromSlash(fx.rel)))
				if err != nil {
					t.Fatal(err)
				}
				args := append([]string{"edit", fx.rel}, c.args...)
				code, stdout, stderr := runVault(vault, 0, append(args, "--json")...)
				if code != 0 {
					t.Fatalf("exit %d: %s", code, stderr)
				}
				var res struct {
					Data struct {
						Item struct {
							Path string `json:"path"`
						} `json:"item"`
					} `json:"data"`
				}
				if err := json.Unmarshal([]byte(stdout), &res); err != nil {
					t.Fatal(err)
				}
				after, err := os.ReadFile(filepath.Join(vault, filepath.FromSlash(res.Data.Item.Path)))
				if err != nil {
					t.Fatal(err)
				}
				keys := append([]string{"updated"}, c.keys...)
				fmBefore, bodyBefore, commentsBefore := splitItem(t, before)
				fmAfter, bodyAfter, commentsAfter := splitItem(t, after)
				if !bytes.Equal(withoutKeys(fmBefore, keys...), withoutKeys(fmAfter, keys...)) {
					t.Errorf("the frontmatter changed beyond %v:\n%q\n%q", keys, fmBefore, fmAfter)
				}
				if !c.body && !bytes.Equal(bodyBefore, bodyAfter) {
					t.Errorf("the body changed:\n%q\n%q", bodyBefore, bodyAfter)
				}
				if !bytes.Equal(commentsBefore, commentsAfter) {
					t.Errorf("the comments section changed:\n%q\n%q", commentsBefore, commentsAfter)
				}
			})
		}
	}
}

func idOf(rel string) string { return strings.SplitN(filepath.Base(rel), " ", 2)[0] }

// splitItem splits an Item file into its frontmatter (with its fences),
// its body and its comments section, from the marker line on.
func splitItem(t *testing.T, b []byte) (fm, body, comments []byte) {
	t.Helper()
	fence := bytes.Index(b[3:], []byte("\n---")) + 3
	end := fence + 1 + bytes.IndexByte(b[fence+1:], '\n') + 1
	marker := bytes.Index(b, []byte(item.CommentsMarker))
	if fence < 3 || marker < end {
		t.Fatalf("not an Item with frontmatter and a marker:\n%q", b)
	}
	return b[:end], b[end:marker], b[marker:]
}
