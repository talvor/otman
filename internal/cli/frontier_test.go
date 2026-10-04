package cli_test

import "testing"

// frontierItem is an open Item file of Project REL with extra
// frontmatter lines, such as relations or an assignee, given as raw YAML.
func frontierItem(id, title string, fields ...string) string {
	fm := "---\nid: " + id + "\ntitle: " + title + "\nkind: issue\nstatus: open\n"
	for _, f := range fields {
		fm += f + "\n"
	}
	return fm + "---\n<!-- otman:comments -->\n## Comments\n"
}

// frontierFiles add to the relations fixture: REL-9 is blocked only by
// the closed REL-5, REL-10 is claimed, REL-11 is blocked by the closed
// REL-5 and the open REL-2, and REL-12 is a grandchild of REL-1, under
// REL-2.
var frontierFiles = map[string]string{
	"vault/Projects/REL/Issues/REL-9 After the groundwork.md": frontierItem("REL-9", "After the groundwork",
		`blocked_by: ["[[REL-5 Old groundwork]]"]`, "labels: [docs]"),
	"vault/Projects/REL/Issues/REL-10 Already taken.md": frontierItem("REL-10", "Already taken",
		"assignee: agent-a"),
	"vault/Projects/REL/Issues/REL-11 Half unblocked.md": frontierItem("REL-11", "Half unblocked",
		`blocked_by: ["[[REL-5 Old groundwork]]", "[[REL-2 Wikilink scanner]]"]`),
	"vault/Projects/REL/Issues/REL-12 Scanner tests.md": frontierItem("REL-12", "Scanner tests",
		`parent: "[[REL-2 Wikilink scanner]]"`, "labels: [docs]"),
}

// frontier lists the open, unassigned Items with no open blockers. A
// closed blocker no longer blocks, so closing the last open blocker of an
// Item puts it on the frontier, and claiming an Item takes it off.
func TestFrontier(t *testing.T) {
	me := map[string]string{"OTM_ACTOR": "talvor"}
	runGolden(t, goldenCase{
		name:    "frontier",
		fixture: "relations",
		files:   withREL(frontierFiles),
		steps: []step{
			{args: []string{"frontier"}, tty: true},
			{args: []string{"frontier"}},
			{args: []string{"frontier", "--json"}},
			{args: []string{"close", "REL-2", "--json"}},
			{args: []string{"frontier"}, tty: true},
			{args: []string{"claim", "REL-3", "--json"}, env: me},
			{args: []string{"frontier", "--json"}},
		},
	})
}

// --parent keeps the ready direct children of REF, never grandchildren,
// and sets the Project scope as it does for list. --kind, --label and
// --search narrow the frontier, which pages like list.
func TestFrontierParentAndFilters(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "frontier-parent",
		fixture: "relations",
		files:   withREL(frontierFiles),
		steps: []step{
			{args: []string{"frontier", "--parent", "REL-1"}, tty: true},
			{args: []string{"frontier", "--parent", "REL-2", "--json"}},
			{args: []string{"frontier", "--parent", "REL-6", "--json"}},
			{args: []string{"frontier", "--parent", "REL-1"}, env: map[string]string{"OTM_PROJECT": "OTH"}, tty: true},
			{args: []string{"frontier", "--kind", "spec"}, tty: true},
			{args: []string{"frontier", "--label", "docs"}, tty: true},
			{args: []string{"frontier", "--search", "SCANNER"}, tty: true},
			{args: []string{"frontier", "--limit", "2", "--offset", "1"}, tty: true},
			{args: []string{"frontier", "--limit", "2", "--offset", "3", "--json"}},
		},
	})
}

// brokenBlockers are Items whose blocked_by links do not resolve. REL-30
// has a dangling blocker, REL-31 one that matches both REL-7 Twin files,
// REL-32 a malformed blocked_by value and REL-33 a blocker, REL-23, whose
// frontmatter cannot be read. REL-34 is claimed and REL-35 closed, so
// their dangling blockers are irrelevant, as is the dangling parent of
// REL-36.
var brokenBlockers = map[string]string{
	"vault/Projects/REL/Issues/REL-30 Dangling blocker.md": frontierItem("REL-30", "Dangling blocker",
		`blocked_by: ["[[REL-5 Old groundwork]]", "[[REL-99 Gone]]"]`),
	"vault/Projects/REL/Issues/REL-7 Twin.md": "---\nid: REL-7\ntitle: Twin\nkind: issue\nstatus: closed\n---\n",
	"vault/Projects/REL/Specs/REL-7 Twin.md":  "---\nid: REL-7\ntitle: Twin\nkind: spec\nstatus: closed\n---\n",
	"vault/Projects/REL/Issues/REL-31 Ambiguous blocker.md": frontierItem("REL-31", "Ambiguous blocker",
		`blocked_by: ["[[REL-7 Twin]]"]`),
	"vault/Projects/REL/Issues/REL-32 Malformed blockers.md": frontierItem("REL-32", "Malformed blockers",
		`blocked_by: "[[REL-5 Old groundwork]]"`),
	"vault/Projects/REL/Issues/REL-23 Unreadable.md": "---\nstatus: [unclosed\n---\n",
	"vault/Projects/REL/Issues/REL-33 Blocked by the unreadable.md": frontierItem("REL-33", "Blocked by the unreadable",
		`blocked_by: ["[[REL-23 Unreadable]]"]`),
	"vault/Projects/REL/Issues/REL-34 Claimed and dangling.md": frontierItem("REL-34", "Claimed and dangling",
		`blocked_by: ["[[REL-99 Gone]]"]`, "assignee: agent-a"),
	"vault/Projects/REL/Issues/REL-35 Closed and dangling.md": "---\nid: REL-35\ntitle: Closed and dangling\nkind: issue\n" +
		"status: closed\nblocked_by: [\"[[REL-99 Gone]]\"]\n---\n",
	"vault/Projects/REL/Issues/REL-36 Lost parent.md": frontierItem("REL-36", "Lost parent",
		`parent: "[[REL-98 Missing parent]]"`),
}

// frontier fails with exit 4, rather than return a partial answer, when
// a blocker link of an Item it would otherwise keep is missing, malformed
// or ambiguous, or names an Item it cannot read: a blocker it cannot see
// is never taken as closed. Broken links of Items it leaves out anyway,
// and broken parent links, do not matter. Each step removes the Item the
// one before failed on.
func TestFrontierBrokenLinks(t *testing.T) {
	issues := "vault/Projects/REL/Issues/"
	runGolden(t, goldenCase{
		name:    "frontier-broken-links",
		fixture: "relations",
		files:   withREL(brokenBlockers),
		steps: []step{
			{args: []string{"frontier", "--json"}},
			{args: []string{"frontier"}, tty: true},
			{args: []string{"frontier", "--parent", "REL-1", "--json"}},
			{args: []string{"frontier", "--kind", "spec", "--json"}},
			{args: []string{"frontier", "--json"}, rm: []string{issues + "REL-30 Dangling blocker.md"}},
			{args: []string{"frontier", "--json"}, rm: []string{issues + "REL-31 Ambiguous blocker.md"}},
			{args: []string{"frontier", "--json"}, rm: []string{issues + "REL-32 Malformed blockers.md"}},
			{args: []string{"frontier"}, rm: []string{issues + "REL-33 Blocked by the unreadable.md"}, tty: true},
		},
	})
}

// frontier's readiness rules are fixed: --state, --assignee, --unassigned
// and --blocked-by are not its flags, so each exits 2 before the Vault is
// read, as do the invalid values list rejects.
func TestFrontierRejectedFlags(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "frontier-rejected",
		fixture: "relations",
		files:   withREL(nil),
		steps: []step{
			{args: []string{"frontier", "--state", "open", "--json"}},
			{args: []string{"frontier", "--state", "all"}, tty: true},
			{args: []string{"frontier", "--assignee", "talvor", "--json"}},
			{args: []string{"frontier", "--assignee", "@me"}},
			{args: []string{"frontier", "--unassigned", "--json"}},
			{args: []string{"frontier", "--blocked-by", "REL-2", "--json"}},
			{args: []string{"frontier", "--without-label", "docs"}, tty: true},
			{args: []string{"frontier", "--parent", "", "--json"}},
			{args: []string{"frontier", "--kind", "bug", "--json"}},
			{args: []string{"frontier", "--all", "--limit", "5", "--json"}},
			{args: []string{"frontier", "REL-1", "--json"}},
		},
	})
}

// With no Project selected frontier needs --all-projects, which spans
// every Project and conflicts with --project and --parent.
func TestFrontierScope(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "frontier-scope",
		fixture: "relations",
		files:   map[string]string{"config/otman/config.toml": "vault = \"$WORK/vault\"\n"},
		steps: []step{
			{args: []string{"frontier", "--json"}},
			{args: []string{"frontier", "--all-projects"}, tty: true},
			{args: []string{"frontier", "--all-projects", "--project", "REL", "--json"}},
			{args: []string{"frontier", "--all-projects", "--parent", "REL-1", "--json"}},
			{args: []string{"frontier", "--parent", "REL-1"}, tty: true},
		},
	})
}
