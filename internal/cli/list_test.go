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
