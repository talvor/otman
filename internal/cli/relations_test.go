package cli_test

import "testing"

// relConfig selects the relations fixture Vault and Project REL, with no
// actor. In the fixture REL-2, REL-3 and REL-4 are children of the spec
// REL-1, REL-4 is blocked by REL-3 and REL-3 by REL-2; REL-5 is closed and
// REL-6 has no relations. OTH-1 is in another Project.
var relConfig = map[string]string{
	"config/otman/config.toml": "vault = \"$WORK/vault\"\nproject = \"REL\"\n",
}

// withREL returns relConfig plus extra files.
func withREL(extra map[string]string) map[string]string {
	files := map[string]string{}
	for p, c := range relConfig {
		files[p] = c
	}
	for p, c := range extra {
		files[p] = c
	}
	return files
}

// parent set stores the parent as a quoted full-filename wikilink,
// replacing any parent the Item has; parent clear sets it to null. Setting
// the parent it has, or clearing a missing one, is a no-op with
// changed:false. A bare target number resolves within the child's
// Project, whatever Project is selected, and a closed parent is valid.
// The parent's view lists its direct children.
func TestParent(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "relation-parent",
		fixture: "relations",
		files:   withREL(nil),
		steps: []step{
			{args: []string{"parent", "set", "REL-6", "REL-1"}, tty: true},
			{args: []string{"parent", "set", "REL-6", "Projects/REL/Specs/REL-1 Relations spec.md", "--json"}},
			{args: []string{"parent", "set", "REL-6", "5"}, env: map[string]string{"OTM_PROJECT": "OTH"}},
			{args: []string{"view", "REL-5", "--json"}},
			{args: []string{"parent", "set", "6", "REL-2 Wikilink scanner.md"}, tty: true},
			{args: []string{"view", "REL-1", "--json"}},
			{args: []string{"parent", "clear", "REL-6"}, tty: true},
			{args: []string{"parent", "clear", "REL-6", "--json"}},
			{args: []string{"parent", "clear", "REL-1"}},
			{args: []string{"parent"}, tty: true},
		},
	})
}

// block appends a quoted full-filename wikilink to blocked_by and unblock
// removes it, keeping the other entries. Adding an existing edge or
// removing an absent one is a no-op with changed:false. Blockers may be
// closed, an Item may have any number of them, and the blocking graph is
// independent of the parent graph. The blocker's view lists what it
// blocks.
func TestBlock(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "relation-block",
		fixture: "relations",
		files:   withREL(nil),
		steps: []step{
			{args: []string{"block", "REL-6", "--by", "REL-4"}, tty: true},
			{args: []string{"block", "REL-6", "--by", "4", "--json"}},
			{args: []string{"block", "REL-6", "--by", "5"}, env: map[string]string{"OTM_PROJECT": "OTH"}},
			{args: []string{"block", "REL-6", "--by", "REL-2", "--json"}},
			{args: []string{"view", "REL-4", "--json"}},
			{args: []string{"block", "REL-1", "--by", "REL-2"}, tty: true},
			{args: []string{"unblock", "REL-6", "--by", "REL-4"}, tty: true},
			{args: []string{"unblock", "REL-6", "--by", "REL-4", "--json"}},
			{args: []string{"unblock", "REL-6", "--by", "REL-5"}},
			{args: []string{"unblock", "REL-6", "--by", "REL-2"}, tty: true},
			{args: []string{"view", "REL-6", "--json"}},
		},
	})
}

// Rule violations exit 4 with stable codes and write nothing: self-edges,
// cycles in either graph, targets in another Project and missing targets.
// A missing primary Item is not found (exit 3), and usage errors exit 2.
func TestRelationErrors(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "relation-errors",
		fixture: "relations",
		files:   withREL(nil),
		steps: []step{
			{args: []string{"parent", "set", "REL-2", "REL-2", "--json"}},
			{args: []string{"block", "REL-2", "--by", "2"}, tty: true},
			{args: []string{"parent", "set", "REL-1", "REL-2", "--json"}},
			{args: []string{"parent", "set", "REL-1", "REL-4"}, tty: true},
			{args: []string{"block", "REL-2", "--by", "REL-4", "--json"}},
			{args: []string{"block", "REL-3", "--by", "REL-4"}, tty: true},
			{args: []string{"parent", "set", "REL-6", "OTH-1", "--json"}},
			{args: []string{"block", "REL-6", "--by", "Projects/OTH/Issues/OTH-1 Elsewhere.md"}, tty: true},
			{args: []string{"unblock", "REL-6", "--by", "OTH-1"}},
			{args: []string{"parent", "set", "REL-6", "REL-99", "--json"}},
			{args: []string{"block", "REL-6", "--by", "99"}, tty: true},
			{args: []string{"unblock", "REL-6", "--by", "REL-6 Missing.md", "--json"}},
			{args: []string{"block", "REL-6", "--by", "Handle sync"}},
			{args: []string{"parent", "set", "REL-99", "REL-1", "--json"}},
			{args: []string{"parent", "set", "OTH-1", "REL-1", "--project", "REL", "--json"}},
			{args: []string{"block", "REL-6", "--json"}},
			{args: []string{"block", "REL-6", "--by", ""}, tty: true},
			{args: []string{"parent", "set", "REL-6"}, tty: true},
			{args: []string{"parent", "clear"}, tty: true},
		},
	})
}

// brokenFiles are Items whose relations do not resolve: REL-20 has a
// dangling parent, a dangling blocker and a malformed one; REL-21 is
// blocked by a link that matches both REL-7 Twin files; REL-22 has a
// malformed blocked_by value; REL-23 cannot be read; REL-24 is a child
// of REL-23; REL-25 has a null blocked_by entry.
var brokenFiles = map[string]string{
	"vault/Projects/REL/Issues/REL-20 Dangling.md": relationItem("REL-20", "Dangling",
		`"[[REL-98 Missing parent]]"`, "\n  - \"[[REL-99 Gone]]\"\n  - REL-2\n  - \"[[REL-5 Old groundwork]]\""),
	"vault/Projects/REL/Issues/REL-7 Twin.md": relationItem("REL-7", "Twin", "null", "[]"),
	"vault/Projects/REL/Specs/REL-7 Twin.md":  relationItem("REL-7", "Twin", "null", "[]"),
	"vault/Projects/REL/Issues/REL-21 Ambiguous.md": relationItem("REL-21", "Ambiguous",
		"null", `["[[REL-7 Twin]]"]`),
	"vault/Projects/REL/Issues/REL-22 Malformed.md": relationItem("REL-22", "Malformed",
		"null", `"[[REL-2 Wikilink scanner]]"`),
	"vault/Projects/REL/Issues/REL-23 Unreadable.md": "---\nparent: [unclosed\n---\n",
	"vault/Projects/REL/Issues/REL-24 Child of the unreadable.md": relationItem("REL-24", "Child of the unreadable",
		`"[[REL-23 Unreadable]]"`, "[]"),
	"vault/Projects/REL/Issues/REL-25 Null entry.md": relationItem("REL-25", "Null entry",
		"null", "\n  - ~"),
}

// Broken links never block view or scalar edits, which warn about them,
// but graph operations fail with exit 4 on a malformed, missing or
// ambiguous link they depend on. A link unrelated to the operation, even
// in the Item it changes, is kept and warned about.
func TestRelationBrokenLinks(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "relation-broken-links",
		fixture: "relations",
		files:   withREL(brokenFiles),
		steps: []step{
			{args: []string{"view", "REL-20", "--json"}},
			{args: []string{"close", "REL-20"}, tty: true},
			{args: []string{"parent", "set", "REL-6", "REL-20", "--json"}},
			{args: []string{"block", "REL-6", "--by", "REL-21", "--json"}},
			{args: []string{"block", "REL-6", "--by", "REL-22"}, tty: true},
			{args: []string{"parent", "set", "REL-6", "REL-24", "--json"}},
			{args: []string{"unblock", "REL-21", "--by", "Projects/REL/Specs/REL-7 Twin.md", "--json"}},
			{args: []string{"block", "REL-6", "--by", "REL-7", "--json"}},
			{args: []string{"list", "--blocked-by", "Projects/REL/Issues/REL-7 Twin.md", "--json"}},
			{args: []string{"list", "--blocked-by", "Projects/REL/Issues/REL-7 Twin.md", "--search", "ambiguous", "--json"}},
			{args: []string{"block", "REL-20", "--by", "REL-6", "--json"}},
			{args: []string{"unblock", "REL-20", "--by", "REL-5"}, tty: true},
			{args: []string{"parent", "set", "REL-20", "REL-1"}},
			{args: []string{"parent", "clear", "REL-22", "--json"}},
			{args: []string{"block", "REL-22", "--by", "REL-6", "--json"}},
			{args: []string{"unblock", "REL-22", "--by", "REL-2"}, tty: true},
			{args: []string{"view", "REL-25"}, tty: true},
			{args: []string{"parent", "set", "REL-23", "REL-1", "--json"}},
		},
	})
}

// create --parent and --blocked-by validate every target before anything
// is written, so a bad relation takes no number. Repeated blockers are
// stored once, and bare numbers resolve in the selected Project.
func TestCreateRelations(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "relation-create",
		fixture: "relations",
		files:   withREL(nil),
		steps: []step{
			{args: []string{"create", "--title", "Relation goldens", "--parent", "REL-1", "--blocked-by", "REL-3", "--blocked-by", "4", "--blocked-by", "REL-3 Parent commands.md", "--json"}},
			{args: []string{"create", "--title", "Blocked by a closed Item", "--blocked-by", "5", "--parent", "5"}, tty: true},
			{args: []string{"create", "--title", "Elsewhere", "--parent", "OTH-1", "--json"}},
			{args: []string{"create", "--title", "Missing", "--blocked-by", "REL-3", "--blocked-by", "REL-99"}, tty: true},
			{args: []string{"create", "--title", "Empty", "--parent", "", "--json"}},
			{args: []string{"create", "--title", "Empty", "--blocked-by", ""}},
			{args: []string{"create", "--title", "Plain"}, tty: true},
			{args: []string{"view", "REL-3", "--json"}},
		},
	})
}

// list --parent keeps direct children and --blocked-by the Items a REF
// blocks. A qualified selector sets the Project scope, even with no
// Project selected or a different implicit one, but an explicit
// conflicting --project fails.
func TestListRelations(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "list-relations",
		fixture: "relations",
		files: withREL(map[string]string{
			"vault/Projects/REL/Issues/REL-8 Closed child.md": "---\nid: REL-8\ntitle: Closed child\nkind: issue\nstatus: closed\n" +
				"parent: \"[[REL-1 Relations spec|the spec]]\"\nblocked_by: [\"[[Issues/REL-2 Wikilink scanner]]\"]\n---\n<!-- otman:comments -->\n## Comments\n",
		}),
		steps: []step{
			{args: []string{"list", "--parent", "REL-1"}, tty: true},
			{args: []string{"list", "--parent", "REL-1", "--state", "all", "--json"}},
			{args: []string{"list", "--blocked-by", "REL-2", "--state", "all"}, tty: true},
			{args: []string{"list", "--parent", "1", "--blocked-by", "REL-3"}},
			{args: []string{"list", "--blocked-by", "REL-5", "--json"}},
			{args: []string{"list", "--parent", "REL-1"}, env: map[string]string{"OTM_PROJECT": "OTH"}, tty: true},
			{args: []string{"list", "--parent", "REL-1", "--project", "OTH", "--json"}},
			{args: []string{"list", "--parent", "REL-1", "--blocked-by", "OTH-1", "--json"}},
			{args: []string{"list", "--all-projects", "--blocked-by", "REL-2", "--json"}},
			{args: []string{"list", "--parent", "REL-99", "--json"}},
			{args: []string{"list", "--parent", "", "--json"}},
		},
	})
}

// With no Project configured, a qualified selector still scopes list,
// but a bare number needs a Project.
func TestListRelationsWithoutProject(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "list-relations-unselected",
		fixture: "relations",
		files:   map[string]string{"config/otman/config.toml": "vault = \"$WORK/vault\"\n"},
		steps: []step{
			{args: []string{"list", "--parent", "REL-1", "--json"}},
			{args: []string{"list", "--blocked-by", "3"}, tty: true},
		},
	})
}

// Relation writes splice only the changed relation key and updated into
// hand-edited frontmatter, keeping comments, quoting and line endings.
func TestRelationsHandEdited(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "relation-hand-edited",
		fixture: "handedited",
		files:   map[string]string{"config/otman/config.toml": "vault = \"$WORK/vault\"\nproject = \"HND\"\n"},
		steps: []step{
			{args: []string{"parent", "set", "HND-1", "HND-2", "--json"}},
			{args: []string{"block", "HND-3", "--by", "HND-10", "--json"}},
			{args: []string{"block", "HND-2", "--by", "HND-1", "--json"}},
			{args: []string{"parent", "clear", "HND-10", "--json"}},
			{args: []string{"block", "HND-6", "--by", "HND-1", "--json"}},
		},
	})
}
