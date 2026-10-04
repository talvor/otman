package cli_test

import "testing"

// Hand edits and Drift (ADR 0005): reads accept Drift and warn, and
// writes heal only lossless Drift in the Item they touch, refusing with
// unsafe_write rather than guess at or lose hand-edited content. Every
// case runs against the drift fixture, whose files are hand-edited YAML
// with comments, blank lines and unknown keys, and each golden pins the
// warnings and the exact bytes every write leaves.

// driftConfig selects the drift fixture's Vault and Project DRF, with an
// actor.
var driftConfig = map[string]string{
	"config/otman/config.toml": "vault = \"$WORK/vault\"\nproject = \"DRF\"\nactor = \"talvor\"\n",
}

// Unparseable and flow-style frontmatter, and frontmatter that repeats a
// key, warn malformed_frontmatter: view still shows the Item, derived
// from its filename and folder, but list and frontier leave it out. Every
// write refuses with unsafe_write and leaves the file as it was.
func TestDriftMalformedFrontmatter(t *testing.T) {
	var steps []step
	for _, ref := range []string{"DRF-9", "DRF-10", "DRF-11"} {
		steps = append(steps,
			step{args: []string{"view", ref, "--json"}},
			step{args: []string{"close", ref}},
			step{args: []string{"reopen", ref}},
			step{args: []string{"edit", ref, "--assignee", "alice"}},
			step{args: []string{"edit", ref, "--body", "New body."}},
			step{args: []string{"comment", ref, "--body", "A comment."}},
			step{args: []string{"claim", ref}},
			step{args: []string{"release", ref}},
			step{args: []string{"parent", "set", ref, "DRF-2"}},
			step{args: []string{"block", ref, "--by", "DRF-2"}},
		)
	}
	steps = append(steps,
		step{args: []string{"view", "DRF-9"}, tty: true},
		step{args: []string{"list", "--state", "all", "--json"}},
		step{args: []string{"frontier"}},
	)
	runGolden(t, goldenCase{name: "drift-malformed-frontmatter", fixture: "drift", files: driftConfig, steps: steps})
}

// An Item's identity is its filename prefix. A frontmatter id that
// differs warns id_mismatch on read, and any write resets it from the
// filename, keeping the rest of the hand-edited frontmatter byte for
// byte. A write that changes nothing heals nothing.
func TestDriftIDMismatch(t *testing.T) {
	runGolden(t, goldenCase{
		name: "drift-id-mismatch", fixture: "drift", files: driftConfig,
		steps: []step{
			{args: []string{"view", "DRF-1", "--json"}},
			{args: []string{"view", "DRF-1"}, tty: true},
			{args: []string{"reopen", "DRF-1", "--json"}},
			{args: []string{"edit", "DRF-1", "--assignee", "alice", "--json"}},
			{args: []string{"view", "DRF-1", "--json"}},
		},
	})
}

// A title that disagrees with the filename warns title_drift and a Kind
// that disagrees with the folder warns kind_drift; reads show the
// frontmatter value, and writes are allowed and keep it. An Item anywhere
// under its Project's folder but outside its Kind folder is still found,
// by reference and by list, and warns unexpected_folder. A Kind that is
// not one of issue, prd or spec, or a title that is not text, reads as
// absent, derived from the folder or the filename, and is kept on write.
func TestDriftTitleKindFolder(t *testing.T) {
	runGolden(t, goldenCase{
		name: "drift-title-kind-folder", fixture: "drift", files: driftConfig,
		steps: []step{
			{args: []string{"view", "DRF-2", "--json"}},
			{args: []string{"view", "DRF-3", "--json"}},
			{args: []string{"view", "DRF-4", "--json"}},
			{args: []string{"view", "DRF-16", "--json"}},
			{args: []string{"list", "--kind", "spec"}, tty: true},
			{args: []string{"list", "--search", "outside"}},
			{args: []string{"close", "DRF-2", "--json"}},
			{args: []string{"close", "DRF-3", "--json"}},
			{args: []string{"comment", "DRF-4", "--body", "Still found in Notes/."}},
			{args: []string{"close", "DRF-16", "--json"}},
		},
	})
}

// A missing id, title or kind is derived from the filename and folder
// with a missing_key warning, and any write heals it, adding the key just
// before the closing ---. A missing created or updated reads as null; a
// write sets updated but never invents created.
func TestDriftMissingKeys(t *testing.T) {
	runGolden(t, goldenCase{
		name: "drift-missing-keys", fixture: "drift", files: driftConfig,
		steps: []step{
			{args: []string{"view", "DRF-5", "--json"}},
			{args: []string{"view", "DRF-5"}, tty: true},
			{args: []string{"reopen", "DRF-5", "--json"}},
			{args: []string{"close", "DRF-5", "--json"}},
			{args: []string{"view", "DRF-5", "--json"}},
		},
	})
}

// A missing or invalid status warns invalid_status on read and leaves the
// Item out of list and frontier, which still warn about it; --state all
// shows it. close and reopen repair it, splicing only status and updated.
func TestDriftStatus(t *testing.T) {
	runGolden(t, goldenCase{
		name: "drift-status", fixture: "drift", files: driftConfig,
		steps: []step{
			{args: []string{"view", "DRF-6", "--json"}},
			{args: []string{"view", "DRF-7"}, tty: true},
			{args: []string{"list", "--search", "status"}, tty: true},
			{args: []string{"frontier", "--search", "status", "--json"}},
			{args: []string{"list", "--state", "all", "--search", "status"}, tty: true},
			{args: []string{"close", "DRF-6", "--json"}},
			{args: []string{"reopen", "DRF-7", "--json"}},
			{args: []string{"list", "--state", "all", "--search", "status"}, tty: true},
		},
	})
}

// A value of the wrong shape (labels that are not a list, an assignee
// that is a list) reads as absent with a malformed_value warning. Writes
// that do not set that key keep it byte for byte; only a command that
// sets it (claim for the assignee, a Label edit for labels) replaces it.
func TestDriftMalformedValues(t *testing.T) {
	runGolden(t, goldenCase{
		name: "drift-malformed-values-kept", fixture: "drift", files: driftConfig,
		steps: []step{
			{args: []string{"view", "DRF-8", "--json"}},
			{args: []string{"list", "--unassigned", "--unlabeled", "--search", "values"}, tty: true},
			{args: []string{"comment", "DRF-8", "--body", "Both values are kept.", "--json"}},
			{args: []string{"close", "DRF-8", "--json"}},
			{args: []string{"release", "DRF-8", "--json"}},
		},
	})
	runGolden(t, goldenCase{
		name: "drift-malformed-values-replaced", fixture: "drift", files: driftConfig,
		steps: []step{
			{args: []string{"claim", "DRF-8", "--json"}},
			{args: []string{"edit", "DRF-8", "--add-label", "bug", "--json"}},
		},
	})
}

// Without a comments marker line, reads fall back to the last
// "## Comments" heading and warn missing_comments_marker. comment and
// close --comment append and restore the marker, just before that heading
// or, with no heading either, in a new comments section at the end; a body
// edit refuses with unsafe_write. Duplicate markers warn; body edits and
// comments refuse with unsafe_write, while writes that touch only the
// frontmatter go ahead.
func TestDriftCommentsMarker(t *testing.T) {
	runGolden(t, goldenCase{
		name: "drift-comments-marker", fixture: "drift", files: driftConfig,
		steps: []step{
			{args: []string{"view", "DRF-12", "--json"}},
			{args: []string{"view", "DRF-12", "--comments"}, tty: true},
			{args: []string{"edit", "DRF-12", "--body", "Rewritten.", "--json"}},
			{args: []string{"edit", "DRF-12", "--clear-body"}, tty: true},
			{args: []string{"comment", "DRF-12", "--body", "The marker is back.", "--json"}},
			{args: []string{"view", "DRF-12", "--json"}},
			{args: []string{"view", "DRF-13", "--json"}},
			{args: []string{"close", "DRF-13", "--comment", "Closed with a new comments section."}, tty: true},
			{args: []string{"view", "DRF-13", "--comments"}, tty: true},
			{args: []string{"view", "DRF-14", "--json"}},
			{args: []string{"comment", "DRF-14", "--body", "Refused.", "--json"}},
			{args: []string{"close", "DRF-14", "--comment", "Refused too."}, tty: true},
			{args: []string{"edit", "DRF-14", "--body", "Refused as well.", "--json"}},
			{args: []string{"edit", "DRF-14", "--assignee", "alice", "--json"}},
		},
	})
}

// Labels still invalid once lowercased read as written with an
// invalid_label warning naming the path, by view, list and label list.
// Writes keep them, lowercasing and deduping case-only differences.
func TestDriftInvalidLabels(t *testing.T) {
	runGolden(t, goldenCase{
		name: "drift-invalid-labels", fixture: "drift", files: driftConfig,
		steps: []step{
			{args: []string{"view", "DRF-15", "--json"}},
			{args: []string{"list", "--label", "bug"}, tty: true},
			{args: []string{"label", "list"}, tty: true},
			{args: []string{"close", "DRF-15", "--json"}},
		},
	})
}

// Every kind of write keeps the YAML otman does not own (unknown keys,
// key order, comments and blank lines) and heals the lossless Drift of
// the Item it touches: a relation edit, a claim and a Kind change that
// moves the file.
func TestDriftEveryWrite(t *testing.T) {
	runGolden(t, goldenCase{
		name: "drift-every-write", fixture: "drift", files: driftConfig,
		steps: []step{
			{args: []string{"parent", "set", "DRF-3", "DRF-2", "--json"}},
			{args: []string{"block", "DRF-5", "--by", "DRF-2", "--json"}},
			{args: []string{"claim", "DRF-1", "--json"}},
			{args: []string{"edit", "DRF-1", "--kind", "prd", "--json"}},
		},
	})
}
