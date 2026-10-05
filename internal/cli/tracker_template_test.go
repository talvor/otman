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

// tracker-template --write saves the template as the repo's
// docs/agents/issue-tracker.md at the git root, from any subdirectory,
// creating docs/agents and replacing an existing file. It reports the path
// rather than echoing the template.
func TestTrackerTemplateWrite(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "tracker-template-write",
		fixture: "basic",
		files: withFiles(map[string]string{
			"vault/Projects/WEB/WEB.md": webNote,
			"repo/.git/HEAD":            "ref: refs/heads/main\n",
			"repo/sub/.keep":            "",
		}),
		steps: []step{
			{args: []string{"project", "link", "WEB"}, dir: "repo"},
			{args: []string{"tracker-template", "--write"}, dir: "repo/sub", tty: true},
			{args: []string{"tracker-template", "--write"}, dir: "repo/sub",
				write: map[string]string{"repo/docs/agents/issue-tracker.md": "stale\n"}},
			{args: []string{"tracker-template", "--write", "--json"}, dir: "repo"},
		},
	})
}

// Outside git, tracker-template --write saves the template under the
// working directory.
func TestTrackerTemplateWriteOutsideGit(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "tracker-template-write-no-git",
		fixture: "basic",
		files:   withFiles(map[string]string{"vault/Projects/WEB/WEB.md": webNote, "dir/.keep": ""}),
		steps: []step{
			{args: []string{"tracker-template", "--write", "--project", "WEB"}, dir: "dir"},
		},
	})
}

// With no Project selected, tracker-template --write fails with exit 2 and
// writes nothing.
func TestTrackerTemplateWriteWithoutProject(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "tracker-template-write-unselected",
		fixture: "basic",
		files:   vaultConfig,
		steps: []step{
			{args: []string{"tracker-template", "--write"}},
		},
	})
}
