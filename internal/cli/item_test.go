package cli_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/talvor/otman/internal/cli"
)

// otmConfig selects the test Vault and Project OTM, with no actor.
var otmConfig = map[string]string{
	"config/otman/config.toml": "vault = \"$WORK/vault\"\nproject = \"OTM\"\n",
}

// withOTM returns otmConfig plus extra files.
func withOTM(extra map[string]string) map[string]string { return withConfig(otmConfig, extra) }

// withConfig returns the files of config plus extra files.
func withConfig(config, extra map[string]string) map[string]string {
	files := maps.Clone(config)
	maps.Copy(files, extra)
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
			{args: []string{"create", "--title", "CRLF body", "--body-file", "-"}, stdin: "Windows line one\r\nline two\r\n\r\nOld Mac\rline\r\n"},
		},
	})
}

// The actor (--actor > OTM_ACTOR > config) is the Author and what @me
// means. There is no git or OS identity fallback: with no actor the Author
// is null and @me fails.
func TestCreateActor(t *testing.T) {
	noActor := map[string]string{"USER": "os-user", "LOGNAME": "os-user", "GIT_AUTHOR_NAME": "git-user"}
	runGolden(t, goldenCase{
		name:    "item-create-actor",
		fixture: "basic",
		files:   withOTM(nil),
		steps: []step{
			{args: []string{"create", "--title", "From the flag", "--assignee", "@me", "--actor", "flag-actor"}, env: map[string]string{"OTM_ACTOR": "env-actor"}},
			{args: []string{"create", "--title", "From the environment", "--assignee", "@me"}, env: map[string]string{"OTM_ACTOR": "env-actor"}},
			{args: []string{"create", "--title", "No actor", "--assignee", "alice"}, env: noActor},
			{args: []string{"create", "--title", "No actor for me", "--assignee", "@me"}, env: noActor},
			{args: []string{"config", "set", "actor", "config-actor"}},
			{args: []string{"create", "--title", "From config", "--assignee", "@me"}},
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
			{args: []string{"create", "--title", "Bad \xff title", "--json"}},
			{args: []string{"create", "--title", "Bad assignee", "--assignee", "bad\xffname", "--json"}},
			{args: []string{"create", "--title", "Bad actor", "--actor", "bad\xffactor", "--json"}},
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
// unique full filename (with its .md) or an exact Vault-relative path. A qualified ID overrides the implicit Project, but not an explicit
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

// relationItem is an Item file whose frontmatter carries relations, given
// as raw YAML.
func relationItem(id, title, parent, blockedBy string) string {
	return "---\nid: " + id + "\ntitle: " + title + "\nkind: issue\nstatus: open\n" +
		"parent: " + parent + "\nblocked_by: " + blockedBy + "\n---\n<!-- otman:comments -->\n## Comments\n"
}

// view reports the relations on disk. parent and blocked_by are quoted
// full-filename wikilinks resolved by basename within the Item's Project;
// a link that matches no Item, or more than one, stays unresolved with its
// raw text, and a malformed value is absent. children and blocks are the
// Items of the same Project whose links resolve to this one. Broken links
// elsewhere never make view fail.
func TestViewRelations(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-view-relations",
		fixture: "items",
		files: withOTM(map[string]string{
			"vault/Projects/OTM/Specs/OTM-10 Relations hub.md": relationItem("OTM-10", "Relations hub",
				`"[[OTM-1 Handle sync collisions]]"`,
				"\n  - \"[[OTM-2 Item file format frontmatter and body]]\"\n  - \"[[OTM-99 Gone]]\"\n"+
					"  - \"[[OTM-3 Twin]]\"\n  - \"[[WEB-1 Landing page]]\"\n  - \"OTM-2\"\n  - 7"),
			"vault/Projects/OTM/Issues/OTM-3 Twin.md": relationItem("OTM-3", "Twin", "null", "[]"),
			"vault/Projects/OTM/PRDs/OTM-3 Twin.md":   relationItem("OTM-3", "Twin", "null", "[]"),
			"vault/Projects/OTM/Issues/OTM-11 Child of the hub.md": relationItem("OTM-11", "Child of the hub",
				`"[[OTM-10 Relations hub|the hub]]"`, `["[[Projects/OTM/Specs/OTM-10 Relations hub]]"]`),
			"vault/Projects/OTM/Issues/OTM-12 Lowercase link.md": relationItem("OTM-12", "Lowercase link",
				"null", `["[[otm-10 relations hub]]"]`),
			"vault/Projects/OTM/Issues/OTM-13 Malformed relations.md": relationItem("OTM-13", "Malformed relations",
				"42", `"[[OTM-10 Relations hub]]"`),
			"vault/Projects/OTM/Issues/OTM-14 Unquoted link.md": relationItem("OTM-14", "Unquoted link",
				"[[OTM-10 Relations hub]]", "[]"),
			"vault/Projects/OTM/Issues/OTM-15 Unreadable.md": "---\nparent: [unclosed\n---\n",
			"vault/Projects/WEB/PRDs/WEB-2 Elsewhere.md": relationItem("WEB-2", "Elsewhere",
				`"[[OTM-10 Relations hub]]"`, `["[[OTM-10 Relations hub]]"]`),
		}),
		steps: []step{
			{args: []string{"view", "OTM-10", "--json"}},
			{args: []string{"view", "OTM-10"}, tty: true},
			{args: []string{"view", "OTM-10"}},
			{args: []string{"view", "OTM-11", "--json"}},
			{args: []string{"view", "OTM-1", "--json"}},
			{args: []string{"view", "OTM-13", "--json"}},
			{args: []string{"view", "OTM-14", "--json"}},
			{args: []string{"view", "OTM-15", "--json"}},
		},
	})
}

// longText is n numbered lines of 99 Unicode characters each, so a
// 2,000-character cut falls inside line 21 and counts runes, not bytes.
func longText(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%02d %s\n", i, strings.Repeat("ü", 95))
	}
	return b.String()
}

// Human and AXI view cut the body and each comment to 2,000 characters,
// leave comments out unless --comments, mark what was hidden and show the
// command that reads everything. --full lifts the limit; JSON is always
// complete.
func TestViewFormats(t *testing.T) {
	long := "---\nid: OTM-1\ntitle: A long spec\naliases:\n  - A long spec\nkind: spec\nstatus: open\nauthor: talvor\n" +
		"parent: null\nblocked_by: []\nlabels:\n  - needs-triage\n  - wayfinder:map\nassignee: null\n" +
		"created: 2026-01-01T10:00:00Z\nupdated: 2026-01-01T11:00:00Z\n---\n" +
		longText(21) + "\n<!-- otman:comments -->\n## Comments\n\n" +
		"### 2026-01-01T10:30:00Z · talvor\nShort comment.\n\n" +
		"### 2026-01-01T11:00:00Z · agent-b\n" + longText(21)
	runGolden(t, goldenCase{
		name:    "item-view-formats",
		fixture: "basic",
		files:   withOTM(map[string]string{"vault/Projects/OTM/Specs/OTM-1 A long spec.md": long}),
		steps: []step{
			{args: []string{"view", "OTM-1"}, tty: true},
			{args: []string{"view", "OTM-1", "--comments"}, tty: true},
			{args: []string{"view", "OTM-1", "--comments", "--full"}, tty: true},
			{args: []string{"view", "OTM-1"}},
			{args: []string{"view", "OTM-1", "--full"}},
			{args: []string{"view", "OTM-1", "--json"}},
			{args: []string{"create", "--title", "Short", "--body", "Just this."}, tty: true},
			{args: []string{"view", "OTM-2"}, tty: true},
			{args: []string{"view", "OTM-2", "--comments"}},
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
