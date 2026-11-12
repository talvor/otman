package cli_test

import "testing"

// A Doc is a note: created, listed, shown and edited like any Item, but its
// file has no status, parent, blocked_by or assignee, and every command that
// would set one fails with invalid_for_doc.
func TestDocKind(t *testing.T) {
	me := map[string]string{"OTM_ACTOR": "talvor"}
	runGolden(t, goldenCase{
		name:    "item-doc",
		fixture: "items",
		files:   withOTM(nil),
		steps: []step{
			{args: []string{"create", "--title", "Meeting notes", "--kind", "doc", "--body", "Agenda.", "--label", "notes", "--json"}, env: me},
			{args: []string{"create", "--title", "Empty doc", "--kind", "doc"}, env: me, tty: true},
			{args: []string{"view", "OTM-3"}, tty: true},
			{args: []string{"list"}},
			{args: []string{"list", "--kind", "doc", "--state", "closed"}},
			{args: []string{"frontier", "--json"}},
			{args: []string{"edit", "OTM-3", "--title", "Meeting minutes", "--body", "Done.", "--add-label", "minutes"}},
			{args: []string{"comment", "OTM-3", "--body", "Noted."}, env: me},
			{args: []string{"create", "--title", "Bad", "--kind", "doc", "--assignee", "x"}},
			{args: []string{"create", "--title", "Bad", "--kind", "doc", "--parent", "OTM-1"}},
			{args: []string{"create", "--title", "Bad", "--kind", "doc", "--blocked-by", "OTM-1"}},
			{args: []string{"edit", "OTM-3", "--assignee", "x"}},
			{args: []string{"edit", "OTM-3", "--clear-assignee"}},
			{args: []string{"edit", "OTM-3", "--kind", "issue"}},
			{args: []string{"edit", "OTM-1", "--kind", "doc"}},
			{args: []string{"close", "OTM-3"}},
			{args: []string{"reopen", "OTM-3"}},
			{args: []string{"claim", "OTM-3"}, env: me},
			{args: []string{"release", "OTM-3"}, env: me},
			{args: []string{"parent", "set", "OTM-3", "OTM-1"}},
			{args: []string{"parent", "set", "OTM-1", "OTM-3"}},
			{args: []string{"block", "OTM-1", "--by", "OTM-3"}},
			{args: []string{"unblock", "OTM-3", "--by", "OTM-1"}},
			{args: []string{"list", "--kind", "doc", "--unassigned"}},
			{args: []string{"doctor"}},
		},
	})
}
