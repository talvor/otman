package cli_test

import "testing"

// tracker-template prints the Tracker template as it is, piped or on a
// terminal, with the linked Project's key filled in. JSON wraps it in the
// envelope.
func TestTrackerTemplate(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "tracker-template",
		fixture: "basic",
		files: withFiles(map[string]string{
			"vault/Projects/WEB/WEB.md": webNote,
			"repo/.git/HEAD":            "ref: refs/heads/main\n",
			"repo/sub/.keep":            "",
		}),
		steps: []step{
			{args: []string{"project", "link", "WEB"}, dir: "repo"},
			{args: []string{"tracker-template"}, dir: "repo/sub"},
			{args: []string{"tracker-template", "--json"}, dir: "repo/sub"},
		},
	})
}

// With no Project selected, tracker-template fails with exit 2 rather than
// print a template for some other Project.
func TestTrackerTemplateWithoutProject(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "tracker-template-unselected",
		fixture: "basic",
		files:   vaultConfig,
		steps: []step{
			{args: []string{"tracker-template"}, tty: true},
			{args: []string{"tracker-template", "--json"}},
			{args: []string{"tracker-template", "--project", "web"}},
		},
	})
}
