package cli_test

import "testing"

// list defaults to the open Items of the selected Project, sorted by
// number, then path, in every output format. Items whose status is
// missing or not open/closed, or whose frontmatter cannot be read, are
// left out; files under Templates/ are not Items.
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
