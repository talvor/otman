package cli_test

import "testing"

// templateFiles are Project (OTM) and Vault-wide Templates for every Kind.
// WEB has no Project Templates of its own.
var templateFiles = withOTM(map[string]string{
	"vault/Projects/WEB/WEB.md":                "---\nname: web\nkind: project\n---\n",
	"vault/Projects/OTM/Templates/issue.md":    "## Project issue\n\nFrom OTM's Templates.\n",
	"vault/Projects/OTM/Templates/prd.md":      "## Project PRD\n",
	"vault/Projects/OTM/Templates/spec.md":     "## Project spec\n",
	"vault/Templates/otman/issue.md":           "## Vault issue\n\n{{title}} and <% tp.date.now() %> are copied as they are.\n",
	"vault/Templates/otman/prd.md":             "## Vault PRD\n",
	"vault/Templates/otman/spec.md":            "## Vault spec\n",
	"vault/Projects/OTM/Templates/OTM-40 x.md": "named like an Item, but a Template\n",
})

// A create with no body takes its Kind's Template from the Project, else
// the Vault, else the built-in, copied verbatim.
func TestCreateTemplatePrecedence(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-create-template-precedence",
		fixture: "basic",
		files:   templateFiles,
		steps: []step{
			{args: []string{"create", "--title", "Project issue"}},
			{args: []string{"create", "--title", "Project PRD", "--kind", "prd"}},
			{args: []string{"create", "--title", "Project spec", "--kind", "spec"}},
			{args: []string{"create", "--title", "WEB uses the Vault's", "--project", "WEB"}},
			{
				rm:   []string{"vault/Projects/OTM/Templates/issue.md", "vault/Projects/OTM/Templates/prd.md", "vault/Projects/OTM/Templates/spec.md"},
				args: []string{"create", "--title", "Vault issue"},
			},
			{args: []string{"create", "--title", "Vault PRD", "--kind", "prd"}},
			{args: []string{"create", "--title", "Vault spec", "--kind", "spec", "--json"}},
			{
				rm:   []string{"vault/Templates/otman/issue.md", "vault/Templates/otman/prd.md", "vault/Templates/otman/spec.md"},
				args: []string{"create", "--title", "Built-in issue"},
			},
			{args: []string{"create", "--title", "Built-in PRD", "--kind", "prd"}},
			{args: []string{"create", "--title", "Built-in spec", "--kind", "spec", "--json"}},
			{args: []string{"view", "Projects/OTM/Templates/OTM-40 x.md"}},
		},
	})
}

// An explicit body is stored as given, never merged with a Template;
// --no-template gives an empty body, and so does an empty Template.
func TestCreateTemplateOptOut(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-create-template-opt-out",
		fixture: "basic",
		files: withOTM(map[string]string{
			"vault/Projects/OTM/Templates/issue.md": "## Project issue\n",
			"vault/Projects/OTM/Templates/prd.md":   "",
			"notes/body.md":                         "From a file.\n",
		}),
		steps: []step{
			{args: []string{"create", "--title", "Explicit body", "--body", "Just this."}},
			{args: []string{"create", "--title", "Explicit empty body", "--body", ""}},
			{args: []string{"create", "--title", "Body from a file", "--body-file", "notes/body.md"}},
			{args: []string{"create", "--title", "Body from stdin", "--body-file", "-"}, stdin: "From stdin.\n"},
			{args: []string{"create", "--title", "No template", "--no-template"}},
			{args: []string{"create", "--title", "No built-in either", "--kind", "spec", "--no-template", "--json"}},
			{args: []string{"create", "--title", "Empty template", "--kind", "prd", "--json"}},
			{args: []string{"create", "--title", "Both", "--no-template", "--body", "x", "--json"}},
			{args: []string{"create", "--title", "Both", "--no-template", "--body-file", "-"}, tty: true},
		},
	})
}

// The first Template found that holds the comments marker line fails with
// invalid_template, naming the file, and nothing is written. So does one
// that is not UTF-8.
func TestCreateInvalidTemplate(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-create-invalid-template",
		fixture: "basic",
		files: withOTM(map[string]string{
			"vault/Projects/OTM/Templates/issue.md": "## Summary\n\n<!-- otman:comments -->\n",
			"vault/Templates/otman/prd.md":          "## Problem\r\n<!-- otman:comments -->\r\n",
			"vault/Templates/otman/issue.md":        "## Vault issue, shadowed\n",
			"vault/Templates/otman/spec.md":         "<!-- otman:comments --> inline is fine\n",
			"vault/Projects/WEB/WEB.md":             "---\nname: web\nkind: project\n---\n",
			"vault/Projects/WEB/Templates/issue.md": "## Summary \xff\n",
		}),
		steps: []step{
			{args: []string{"create", "--title", "Project marker", "--json"}},
			{args: []string{"create", "--title", "Vault marker", "--kind", "prd"}, tty: true},
			{args: []string{"create", "--title", "Explicit body skips it", "--body", "Fine."}},
			{args: []string{"create", "--title", "No template skips it", "--kind", "prd", "--no-template"}},
			{args: []string{"create", "--title", "Inline marker", "--kind", "spec"}},
			{args: []string{"create", "--title", "Not UTF-8", "--project", "WEB", "--json"}},
		},
	})
}
