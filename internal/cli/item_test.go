package cli_test

import (
	"testing"
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
