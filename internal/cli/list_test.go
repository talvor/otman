package cli_test

import "testing"

// list defaults to the open Items of the selected Project, sorted by
// number, then path, in every output format. An Item whose status is
// missing or not open/closed is left out with a warning, as is one whose
// frontmatter cannot be read; files under Templates/ are not Items.
func TestListDefault(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "list-default",
		fixture: "list",
		files:   withOTM(nil),
		steps: []step{
			{args: []string{"list"}, tty: true},
			{args: []string{"list"}},
			{args: []string{"list", "--json"}},
			{args: []string{"list", "--json"}, env: map[string]string{"OTM_PROJECT": "WEB"}},
		},
	})
}

// With no Project selected, list needs --all-projects, which spans every
// Project in key order and conflicts with an explicit --project. Implicit
// Project defaults (environment, repo pointer, config) give way to it, so
// even an unreadable pointer does not block it.
func TestListAllProjects(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "list-all-projects",
		fixture: "list",
		files:   withFiles(map[string]string{"repo/.otman.toml": "project = [\n"}),
		steps: []step{
			{args: []string{"list", "--json"}},
			{args: []string{"list"}, tty: true},
			{args: []string{"list", "--all-projects"}, tty: true},
			{args: []string{"list", "--all-projects", "--json"}},
			{args: []string{"list", "--all-projects"}, env: map[string]string{"OTM_PROJECT": "WEB"}},
			{args: []string{"list", "--all-projects", "--project", "OTM", "--json"}},
			{args: []string{"list", "--all-projects", "--project", "OTM"}, tty: true},
			{args: []string{"list", "--json"}, dir: "repo"},
			{args: []string{"list", "--all-projects", "--json"}, dir: "repo"},
			{args: []string{"list", "--project", "NOPE", "--json"}},
		},
	})
}

// --state picks open (the default), closed or all Items. An Item whose
// status is neither is listed only by --state all, with its status as
// found, and otherwise named in a warning unless another filter left it
// out. An unreadable Item is named in a warning in every state. --kind picks one Kind, read from the
// frontmatter or else the folder.
func TestListStateAndKind(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "list-state-kind",
		fixture: "list",
		files:   withOTM(nil),
		steps: []step{
			{args: []string{"list", "--state", "closed"}, tty: true},
			{args: []string{"list", "--state", "all"}, tty: true},
			{args: []string{"list", "--state", "all", "--kind", "issue", "--search", "status", "--json"}},
			{args: []string{"list", "--state", "open", "--json"}, env: map[string]string{"OTM_PROJECT": "WEB"}},
			{args: []string{"list", "--kind", "spec"}, tty: true},
			{args: []string{"list", "--kind", "prd", "--state", "all", "--json"}},
			{args: []string{"list", "--kind", "spec", "--state", "closed", "--all-projects"}, tty: true},
			{args: []string{"list", "--kind", "prd", "--state", "closed", "--json"}, env: map[string]string{"OTM_PROJECT": "AAA"}},
			{args: []string{"list", "--kind", "prd", "--state", "closed"}, env: map[string]string{"OTM_PROJECT": "AAA"}, tty: true},
		},
	})
}

// --assignee keeps the Items assigned to exactly NAME, or to the actor
// with @me; --unassigned keeps those with no assignee.
func TestListAssignee(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "list-assignee",
		fixture: "list",
		files:   withOTM(nil),
		steps: []step{
			{args: []string{"list", "--assignee", "talvor", "--state", "all"}, tty: true},
			{args: []string{"list", "--assignee", "talvor", "--all-projects", "--state", "all", "--json"}},
			{args: []string{"list", "--assignee", "@me"}, env: map[string]string{"OTM_ACTOR": "agent-b"}, tty: true},
			{args: []string{"list", "--assignee", "@me", "--actor", "Talvor"}, tty: true},
			{args: []string{"list", "--assignee", "nobody", "--json"}},
			{args: []string{"list", "--unassigned"}, tty: true},
			{args: []string{"list", "--unassigned", "--state", "closed", "--all-projects", "--json"}},
		},
	})
}

// --search keeps Items whose title or body contains TEXT, ignoring case.
// Comments are not searched.
func TestListSearch(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "list-search",
		fixture: "list",
		files:   withOTM(nil),
		steps: []step{
			{args: []string{"list", "--search", "sync"}, tty: true},
			{args: []string{"list", "--search", "DUPLICATE NUMBERS", "--json"}},
			{args: []string{"list", "--search", "needle"}, tty: true},
			{args: []string{"list", "--search", "FILE FORMAT", "--state", "all", "--kind", "spec"}, tty: true},
			{args: []string{"list", "--search", "screen", "--all-projects", "--unassigned"}, tty: true},
			{args: []string{"list", "--search", "nothing like this"}},
		},
	})
}

// list pages like every collection: 50 by default, --limit and --offset
// over the sorted, filtered Items, or --all. Paging past the end is an
// empty page, not an error.
func TestListPaging(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "list-paging",
		fixture: "list",
		files:   withOTM(nil),
		steps: []step{
			{args: []string{"list", "--limit", "2"}, tty: true},
			{args: []string{"list", "--limit", "2", "--offset", "2"}, tty: true},
			{args: []string{"list", "--limit", "2", "--offset", "4", "--json"}},
			{args: []string{"list", "--limit", "2", "--offset", "2"}},
			{args: []string{"list", "--offset", "5", "--json"}},
			{args: []string{"list", "--offset", "99"}, tty: true},
			{args: []string{"list", "--limit", "9223372036854775807", "--offset", "6", "--state", "all", "--all-projects", "--json"}},
			{args: []string{"list", "--all", "--all-projects", "--state", "all"}, tty: true},
			{args: []string{"list", "--limit", "1", "--all-projects", "--kind", "prd", "--json"}},
		},
	})
}

// Every invalid value or combination exits 2 before the Vault is read.
func TestListErrors(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "list-errors",
		fixture: "list",
		files:   withOTM(nil),
		steps: []step{
			{args: []string{"list", "--state", "done", "--json"}},
			{args: []string{"list", "--state", ""}, tty: true},
			{args: []string{"list", "--kind", "bug", "--json"}},
			{args: []string{"list", "--kind", ""}},
			{args: []string{"list", "--assignee", "talvor", "--unassigned", "--json"}},
			{args: []string{"list", "--assignee", "@me", "--unassigned"}, tty: true},
			{args: []string{"list", "--assignee", "@me", "--json"}},
			{args: []string{"list", "--assignee", " ", "--json"}},
			{args: []string{"list", "--search", "", "--json"}},
			{args: []string{"list", "--all-projects", "--project", "WEB", "--json"}},
			{args: []string{"list", "--all", "--limit", "5", "--json"}},
			{args: []string{"list", "--all", "--offset", "1"}, tty: true},
			{args: []string{"list", "--limit", "0", "--json"}},
			{args: []string{"list", "--limit", "-3"}},
			{args: []string{"list", "--offset", "-1", "--json"}},
			{args: []string{"list", "--limit", "many", "--json"}},
			{args: []string{"list", "OTM-1", "--json"}},
		},
	})
}
