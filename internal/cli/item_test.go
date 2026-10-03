package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/talvor/otman/internal/cli"
)

// otmConfig selects the test Vault and Project OTM, with no actor.
var otmConfig = map[string]string{
	"config/otman/config.toml": "vault = \"$WORK/vault\"\nproject = \"OTM\"\n",
}

// withOTM returns otmConfig plus extra files.
func withOTM(extra map[string]string) map[string]string {
	files := map[string]string{}
	for p, c := range otmConfig {
		files[p] = c
	}
	for p, c := range extra {
		files[p] = c
	}
	return files
}

// create makes one open Item in the selected Project, numbered after the
// highest number in use, filed in its Kind's folder, in every output
// format. The actor, when set, is the Author.
func TestCreate(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-create",
		fixture: "basic",
		files:   withOTM(map[string]string{"notes/body.md": "From a file.\n\n## Comments\n\nA body may have its own Comments heading.\n"}),
		steps: []step{
			{args: []string{"create", "--title", "Handle sync collisions"}, tty: true},
			{args: []string{"create", "--title", "Item file format", "--kind", "spec", "--body", "Line one\n\nLine two", "--actor", "talvor", "--json"}},
			{args: []string{"create", "--title", "Why: a tracker?", "--kind", "prd", "--body-file", "-", "--assignee", "@me", "--actor", "agent-b"}, stdin: "From stdin.\n"},
			{args: []string{"create", "--title", "Body from a file", "--body-file", "notes/body.md", "--assignee", "someone", "--json"}},
		},
	})
}

// A create that fails validation writes nothing: no file, and no number
// taken.
func TestCreateErrors(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-create-errors",
		fixture: "basic",
		files:   withOTM(nil),
		steps: []step{
			{args: []string{"create"}, tty: true},
			{args: []string{"create", "--title", "   ", "--json"}},
			{args: []string{"create", "--title", "two\nlines"}},
			{args: []string{"create", "--title", "Bug", "--kind", "bug", "--json"}},
			{args: []string{"create", "--title", "Both", "--body", "x", "--body-file", "-"}},
			{args: []string{"create", "--title", "Marker", "--body", "Text\n<!-- otman:comments -->\nmore", "--json"}},
			{args: []string{"create", "--title", "Marker", "--body-file", "-"}, stdin: "Text\r\n<!-- otman:comments -->\r\n", tty: true},
			{args: []string{"create", "--title", "Missing", "--body-file", "nope.md", "--json"}},
			{args: []string{"create", "--title", "Mine", "--assignee", "@me", "--json"}},
			{args: []string{"create", "--title", "Nobody", "--assignee", " "}},
			{args: []string{"create", "--title", "Elsewhere", "--project", "NOPE", "--json"}},
			{args: []string{"create", "--title", "Marker inline is fine", "--body", "Say <!-- otman:comments --> inline", "--json"}},
		},
	})
}

// Without a selected Project, create fails rather than guess one.
func TestCreateWithoutProject(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-create-unselected",
		fixture: "basic",
		files:   vaultConfig,
		steps:   []step{{args: []string{"create", "--title", "Orphan", "--json"}}},
	})
}

// The filename is a sanitised projection of the title, cut to 60
// characters at a word boundary; title and aliases keep the exact text.
func TestCreateFilenames(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-create-filenames",
		fixture: "basic",
		files:   withOTM(nil),
		steps: []step{
			{args: []string{"create", "--title", `What? "Quoted" #tags | pipes ^ref: a/b\c *star* <angle> %%hidden%% [[link]]`, "--json"}},
			{args: []string{"create", "--title", "  lots   of\tspace  here  ", "--json"}},
			{args: []string{"create", "--title", "Allocate item numbers under the Vault lock so that two agents never collide", "--json"}},
			{args: []string{"create", "--title", "Exactly sixty characters long, which needs no cutting at all", "--json"}},
			{args: []string{"create", "--title", "Supercalifragilisticexpialidocious-and-then-some-more-words-joined", "--json"}},
			{args: []string{"create", "--title", "Ünïcödé títlé wïth áccénts thát rüns pást thé sïxty chäräctér lïmït", "--json"}},
			{args: []string{"create", "--title", "[#[nested]#]", "--json"}},
			{args: []string{"create", "--title", `??? ::: ***`, "--json"}},
		},
	})
}

// Numbers come after the highest Item number on disk, even in a fresh db,
// and are never reused once a file is deleted.
func TestCreateNumbering(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-numbering",
		fixture: "basic",
		files: withOTM(map[string]string{
			"vault/Projects/OTM/Issues/OTM-7 Seven.md":      "---\nid: OTM-7\n---\n",
			"vault/Projects/OTM/Elsewhere/OTM-3 Three.md":   "---\nid: OTM-3\n---\n",
			"vault/Projects/OTM/Templates/OTM-50 Sample.md": "not an Item\n",
			"vault/Projects/OTM/Issues/OTM-0 Zero.md":       "not an Item\n",
			"vault/Projects/OTM/Issues/WEB-90 Other.md":     "another Project's prefix\n",
			"vault/Projects/OTM/Issues/OTM-12x.md":          "not an Item\n",
		}),
		steps: []step{
			{args: []string{"create", "--title", "Eight"}},
			{args: []string{"create", "--title", "Nine"}},
			{rm: []string{"vault/Projects/OTM/Issues/OTM-9 Nine.md"}, args: []string{"create", "--title", "Ten, not nine again"}},
			{rm: []string{"vault/Projects/OTM/Issues/OTM-10 Ten, not nine again.md", "vault/Projects/OTM/Issues/OTM-8 Eight.md"}, args: []string{"create", "--title", "Eleven"}},
		},
	})
}

// view takes a qualified ID, a bare number in the selected Project, a
// unique full filename (with or without .md) or an exact Vault-relative
// path. A qualified ID overrides the implicit Project, but not an explicit
// conflicting --project. Nothing matches by title.
func TestViewRefs(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-view-refs",
		fixture: "items",
		files: withOTM(map[string]string{
			"vault/Projects/OTM/Issues/OTM-3 First copy.md":  "---\nid: OTM-3\ntitle: First copy\nkind: issue\nstatus: open\n---\n<!-- otman:comments -->\n## Comments\n",
			"vault/Projects/OTM/Issues/OTM-3 Second copy.md": "---\nid: OTM-3\ntitle: Second copy\nkind: issue\nstatus: open\n---\n<!-- otman:comments -->\n## Comments\n",
			"vault/Projects/OTM/Templates/OTM-9 Sample.md":   "not an Item\n",
			"vault/outside.md": "not an Item\n",
		}),
		steps: []step{
			{args: []string{"view", "OTM-1", "--json"}},
			{args: []string{"view", "2", "--json"}},
			{args: []string{"view", "OTM-2 Item file format frontmatter and body.md", "--json"}},
			{args: []string{"view", "OTM-2 Item file format frontmatter and body"}},
			{args: []string{"view", "Projects/OTM/Specs/OTM-2 Item file format frontmatter and body.md"}, tty: true},
			{args: []string{"view", "WEB-1", "--json"}},
			{args: []string{"view", "WEB-1", "--project", "WEB"}},
			{args: []string{"view", "WEB-1", "--project", "OTM", "--json"}},
			{args: []string{"view", "WEB-1"}, env: map[string]string{"OTM_PROJECT": "lower"}},
			{args: []string{"view", "1", "--project", "WEB", "--json"}},
			{args: []string{"view", "Projects/WEB/PRDs/WEB-1 Landing page.md", "--project", "OTM"}},
			{args: []string{"view", "OTM-3", "--json"}},
			{args: []string{"view", "Projects/OTM/Issues/OTM-3 Second copy.md", "--json"}},
			{args: []string{"view", "99", "--json"}},
			{args: []string{"view", "OTM-99"}, tty: true},
			{args: []string{"view", "NOPE-1"}},
			{args: []string{"view", "Handle sync collisions", "--json"}},
			{args: []string{"view", "OTM-1 Handle sync"}},
			{args: []string{"view", "Projects/OTM/Templates/OTM-9 Sample.md"}},
			{args: []string{"view", "Projects/OTM/Issues/../Issues/OTM-1 Handle sync collisions.md"}},
			{args: []string{"view", "outside.md"}},
			{args: []string{"view"}},
		},
	})
}

// A bare number needs a selected Project.
func TestViewWithoutProject(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-view-unselected",
		fixture: "items",
		files:   vaultConfig,
		steps: []step{
			{args: []string{"view", "1", "--json"}},
			{args: []string{"view", "OTM-1", "--format", "axi"}},
		},
	})
}

// Concurrent creates in one Project on one device are serialised by the
// lock, so every Item gets its own number.
func TestCreateConcurrent(t *testing.T) {
	work := t.TempDir()
	vault := filepath.Join(work, "vault")
	writeFile(t, filepath.Join(vault, "Projects", "OTM", "OTM.md"), "---\nname: otman\nkind: project\n---\n")
	const n = 8
	codes := make([]int, n)
	stdouts := make([]string, n)
	stderrs := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], stdouts[i], stderrs[i] = runVault(vault, 0,
				"create", "--title", fmt.Sprintf("Item %d", i), "--project", "OTM", "--format", "json")
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for i := range n {
		if codes[i] != cli.ExitOK {
			t.Fatalf("run %d: exit %d: %s", i, codes[i], stderrs[i])
		}
		var res struct {
			Data struct {
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(stdouts[i]), &res); err != nil {
			t.Fatalf("run %d: %v: %s", i, err, stdouts[i])
		}
		if seen[res.Data.Item.ID] {
			t.Errorf("two creates got %s", res.Data.Item.ID)
		}
		seen[res.Data.Item.ID] = true
	}
	for i := 1; i <= n; i++ {
		if id := fmt.Sprintf("OTM-%d", i); !seen[id] {
			t.Errorf("no create got %s; got %v", id, seen)
		}
	}
	entries, err := os.ReadDir(filepath.Join(vault, "Projects", "OTM", "Issues"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != n {
		t.Errorf("%d files in Issues/, want %d", len(entries), n)
	}
}
