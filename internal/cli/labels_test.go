package cli_test

import (
	"strings"
	"testing"
)

// labelItem is an Item file of Kind issue with status and labels, given as
// the YAML of the labels value; "" leaves the key out.
func labelItem(id, title, status, labels string) string {
	fm := "---\nid: " + id + "\ntitle: " + title + "\nkind: issue\nstatus: " + status + "\n"
	if labels != "" {
		fm += "labels: " + labels + "\n"
	}
	return fm + "assignee: null\n---\n<!-- otman:comments -->\n## Comments\n"
}

// labelVault is a Vault whose Projects use a few Labels, one Item with
// Labels differing only by case, and Items with no Labels.
func labelVault(extra map[string]string) map[string]string {
	files := map[string]string{
		"vault/Projects/OTM/Issues/OTM-1 Triage me.md":        labelItem("OTM-1", "Triage me", "open", "[bug, needs-triage]"),
		"vault/Projects/OTM/Issues/OTM-2 Map the way.md":      labelItem("OTM-2", "Map the way", "open", "\n  - bug\n  - wayfinder:map"),
		"vault/Projects/OTM/Issues/OTM-3 Fixed already.md":    labelItem("OTM-3", "Fixed already", "closed", "[bug]"),
		"vault/Projects/OTM/Issues/OTM-4 Empty labels.md":     labelItem("OTM-4", "Empty labels", "open", "[]"),
		"vault/Projects/OTM/Issues/OTM-5 Hand edited case.md": labelItem("OTM-5", "Hand edited case", "open", "\n  - Bug\n  - BUG\n  - Enhancement"),
		"vault/Projects/OTM/Issues/OTM-6 No labels key.md":    labelItem("OTM-6", "No labels key", "open", ""),
		"vault/Projects/WEB/WEB.md":                           webNote,
		"vault/Projects/WEB/Issues/WEB-1 Redesign.md":         labelItem("WEB-1", "Redesign", "open", "[design]"),
		"vault/Projects/AAA/AAA.md":                           "---\nname: unlabelled\nkind: project\n---\n",
	}
	for p, c := range extra {
		files[p] = c
	}
	return files
}

// create --label and edit --add-label/--remove-label store lowercase
// Labels without repeats in the flat labels list. Repeating an add or a
// remove, in any case, is a no-op with changed:false. A Label no other
// Item in the Project carries warns new_label, listing the closest Labels
// in use: those a few edits away, or containing or contained in it.
func TestLabelCreateEdit(t *testing.T) {
	long := strings.Repeat("a", 64)
	runGolden(t, goldenCase{
		name:    "label-create-edit",
		fixture: "basic",
		files:   withOTM(labelVault(nil)),
		steps: []step{
			{args: []string{"create", "--title", "Known labels", "--label", "Bug", "--label", "needs-triage", "--label", "BUG", "--json"}},
			{args: []string{"create", "--title", "A typo", "--label", "bgu"}, tty: true},
			{args: []string{"create", "--title", "Map typo", "--label", "wayfinder:mpa", "--label", "triage", "--json"}},
			{args: []string{"create", "--title", "Longest label", "--label", long, "--label", "x", "--no-template"}},
			{args: []string{"edit", "OTM-1", "--add-label", "enhancement", "--json"}},
			{args: []string{"edit", "OTM-1", "--add-label", "enhancement", "--json"}},
			{args: []string{"edit", "OTM-1", "--add-label", "ENHANCEMENT"}, tty: true},
			{args: []string{"edit", "OTM-1", "--remove-label", "Bug", "--json"}},
			{args: []string{"edit", "OTM-1", "--remove-label", "bug", "--remove-label", "nothing-here", "--json"}},
			{args: []string{"edit", "OTM-1", "--add-label", "docs", "--add-label", "Docs", "--remove-label", "needs-triage"}, tty: true},
			{args: []string{"edit", "OTM-4", "--add-label", "bug", "--add-label", "wayfinder:map", "--json"}},
			{args: []string{"edit", "OTM-6", "--add-label", "needs-triage", "--json"}},
			{args: []string{"edit", "WEB-1", "--add-label", "bug", "--json"}},
		},
	})
}

// A Label must match [a-z0-9][a-z0-9._:/-]* once lowercased and be at
// most 64 characters, on create, edit or a filter; otherwise the command
// fails with exit 2 invalid_label before the Vault is touched. Adding and
// removing the same Label conflicts, as do --label and --unlabeled.
func TestLabelErrors(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "label-errors",
		fixture: "basic",
		files:   withOTM(labelVault(nil)),
		steps: []step{
			{args: []string{"create", "--title", "X", "--label", "has space", "--json"}},
			{args: []string{"create", "--title", "X", "--label", ""}, tty: true},
			{args: []string{"create", "--title", "X", "--label", "ok", "--label", "-dash"}},
			{args: []string{"create", "--title", "X", "--label", strings.Repeat("a", 65), "--json"}},
			{args: []string{"create", "--title", "X", "--label", "Ünicode"}},
			{args: []string{"edit", "OTM-1", "--add-label", "a b", "--json"}},
			{args: []string{"edit", "OTM-1", "--remove-label", "_x"}, tty: true},
			{args: []string{"edit", "OTM-1", "--add-label", "bug", "--remove-label", "BUG", "--json"}},
			{args: []string{"list", "--label", "Bad Label", "--json"}},
			{args: []string{"list", "--without-label", "why?"}},
			{args: []string{"list", "--label", "bug", "--unlabeled", "--json"}},
			{args: []string{"label", "list", "--limit", "0", "--json"}},
			{args: []string{"label", "list", "--all-projects", "--project", "OTM", "--json"}},
			{args: []string{"label", "list", "--project", "NOPE", "--json"}},
			{args: []string{"label", "list", "extra"}},
		},
	})
}

// list --label keeps Items with every named Label, --without-label drops
// Items with any, and --unlabeled keeps Items with none, all ignoring
// case. A filter Label that no Item in scope carries, whatever its
// status, warns unknown_label with the closest Labels in use.
func TestListLabels(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "list-labels",
		fixture: "basic",
		files:   withOTM(labelVault(nil)),
		steps: []step{
			{args: []string{"list", "--label", "bug"}, tty: true},
			{args: []string{"list", "--label", "BUG", "--label", "needs-triage", "--json"}},
			{args: []string{"list", "--label", "bug", "--state", "closed"}, tty: true},
			{args: []string{"list", "--without-label", "bug", "--state", "all"}, tty: true},
			{args: []string{"list", "--without-label", "needs-triage", "--without-label", "wayfinder:map", "--json"}},
			{args: []string{"list", "--unlabeled", "--state", "all", "--json"}},
			{args: []string{"list", "--unlabeled", "--without-label", "bug", "--all-projects"}, tty: true},
			{args: []string{"list", "--label", "bgu", "--json"}},
			{args: []string{"list", "--label", "bug", "--without-label", "nope"}, tty: true},
			{args: []string{"list", "--label", "design"}},
			{args: []string{"list", "--label", "design", "--all-projects", "--json"}},
		},
	})
}

// label list counts each Label in use, lowercased, in the selected Project
// or every Project: how many open Items carry it and how many in all,
// sorted by name and paged like every collection.
func TestLabelList(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "label-list",
		fixture: "basic",
		files: labelVault(map[string]string{
			"config/otman/config.toml":                  "vault = \"$WORK/vault\"\n",
			"vault/Projects/OTM/Issues/OTM-7 Broken.md": "---\nlabels: [bug\n---\n",
		}),
		steps: []step{
			{args: []string{"label", "list", "--project", "OTM"}, tty: true},
			{args: []string{"label", "list", "--project", "OTM", "--json"}},
			{args: []string{"label", "list", "--project", "OTM"}},
			{args: []string{"label", "list", "--all-projects"}, tty: true},
			{args: []string{"label", "list", "--project", "OTM", "--limit", "2", "--offset", "1", "--json"}},
			{args: []string{"label", "list", "--project", "OTM", "--limit", "2"}, tty: true},
			{args: []string{"label", "list", "--project", "AAA", "--json"}},
			{args: []string{"label", "list", "--project", "AAA"}, tty: true},
			{args: []string{"label", "list", "--json"}},
		},
	})
}

// Hand-edited Labels differing only by case read as written and match in
// lowercase. The next otman write to that Item, whatever it changes,
// lowercases and dedupes them, keeping any still invalid; a write that
// changes nothing heals nothing. A labels value that is not a list, or a
// list with null entries, is left alone until a Label edit replaces it;
// Label edits see the Labels a read shows.
func TestLabelHealing(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "label-heal",
		fixture: "basic",
		files: withOTM(labelVault(map[string]string{
			"vault/Projects/OTM/Issues/OTM-7 Still invalid.md": labelItem("OTM-7", "Still invalid", "open", "\n  - Needs-Triage\n  - needs-triage\n  - Not Valid!"),
			"vault/Projects/OTM/Issues/OTM-8 Not a list.md":    labelItem("OTM-8", "Not a list", "open", "Bug"),
			"vault/Projects/OTM/Issues/OTM-9 Null entry.md":    labelItem("OTM-9", "Null entry", "open", "[Bug, ~]"),
		})),
		steps: []step{
			{args: []string{"view", "OTM-5", "--json"}},
			{args: []string{"edit", "OTM-5", "--add-label", "bug", "--remove-label", "wontfix", "--json"}},
			{args: []string{"close", "OTM-5", "--json"}},
			{args: []string{"comment", "OTM-7", "--body", "Healed by a comment.", "--json"}, env: map[string]string{"OTM_ACTOR": "talvor"}},
			{args: []string{"close", "OTM-8", "--json"}},
			{args: []string{"edit", "OTM-8", "--add-label", "bug", "--json"}},
			{args: []string{"close", "OTM-9", "--json"}},
			{args: []string{"edit", "OTM-9", "--add-label", "bug", "--json"}},
			{args: []string{"edit", "OTM-9", "--remove-label", "bug", "--json"}},
		},
	})
}
