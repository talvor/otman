package cli_test

import "testing"

// doctor reports the Drift of Items (ADR 0005) and, with --fix, repairs
// what is deterministic. Every case runs against the doctor fixture, whose
// Project DOC has one Item per finding, and each golden pins the findings,
// the repairs and the exact bytes they leave. A fresh db has no snapshots,
// so title and Kind disagreements are choices until a snapshot exists.

// doctorConfig selects the doctor fixture's Vault and Project DOC, with an
// actor.
var doctorConfig = map[string]string{
	"config/otman/config.toml": "vault = \"$WORK/vault\"\nproject = \"DOC\"\nactor = \"talvor\"\n",
}

// Without --fix nothing changes. Every finding is listed with its fix:
// auto when --fix repairs it deterministically, choice when it needs
// --prefer, and none when a person must act. A clean Item is not listed,
// and the run exits 4 while any finding remains.
func TestDoctorReport(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-report", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{args: []string{"doctor"}, tty: true},
			{args: []string{"doctor", "--json"}},
		},
	})
}

// --fix repairs the findings that need no choice: a file outside its Kind
// folder moves into it, id is reset, missing keys are healed from the
// filename and folder, labels are slugified and merged, a dangling link is
// repointed when its prefix names exactly one Item, and a restorable
// marker is put back. The title and Kind disagreements are choices, which
// stay, so the run still exits 4.
func TestDoctorFixAuto(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-fix-auto", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{args: []string{"doctor", "--fix"}, tty: true},
			{args: []string{"doctor", "--json"}},
		},
	})
}

// --prefer frontmatter settles every choice in favour of the frontmatter:
// the file is renamed to its title and moved to its Kind folder, with its
// links rewritten through the journal.
func TestDoctorFixPreferFrontmatter(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-fix-prefer-frontmatter", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{args: []string{"doctor", "--fix", "--prefer", "frontmatter"}, tty: true},
			{args: []string{"doctor", "--json"}},
		},
	})
}

// --prefer file settles every choice in favour of the file: the title is
// taken from the filename and the Kind from the folder.
func TestDoctorFixPreferFile(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-fix-prefer-file", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{args: []string{"doctor", "--fix", "--prefer", "file"}, tty: true},
			{args: []string{"doctor"}},
		},
	})
}

// A REF narrows the run to one Item, whose findings are the only ones
// reported or repaired.
func TestDoctorRef(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-ref", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{args: []string{"doctor", "DOC-6"}, tty: true},
			{args: []string{"doctor", "DOC-2", "--fix", "--json"}},
			{args: []string{"doctor", "DOC-16"}},
		},
	})
}

// --all-projects checks every Project; without it doctor checks only the
// selected one.
func TestDoctorAllProjects(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-all-projects", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{args: []string{"doctor", "--all-projects"}, tty: true},
			{args: []string{"doctor", "--all-projects", "--fix", "--prefer", "file", "--json"}},
		},
	})
}

// The db of a rebuilt vault has no snapshots, so every title and Kind
// disagreement is a choice, even one a snapshot would settle. Removing the
// db is what a rebuild looks like to doctor.
func TestDoctorRebuiltDB(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-rebuilt-db", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{args: []string{"create", "--title", "Fresh", "--kind", "issue"}},
			{rm: []string{"vault/.otman/otman.db"}, args: []string{"doctor", "--json"}},
		},
	})
}

// With a snapshot, a disagreement is settled when only one side changed
// since otman last wrote the Item, and a choice when both changed. A
// hand edit between steps stands in for an edit made outside otman.
func TestDoctorSnapshots(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-snapshots", fixture: "doctor", files: doctorConfig,
		steps: []step{
			// The frontmatter title changes alone: the title wins.
			{args: []string{"create", "--title", "Snapshot one", "--kind", "issue"}},
			{write: map[string]string{
				"vault/Projects/DOC/Issues/DOC-17 Snapshot one.md": "---\nid: DOC-17\ntitle: Retyped title\nkind: issue\nstatus: open\nlabels: []\nassignee: null\ncreated: 2026-01-02T03:04:05Z\nupdated: 2026-01-02T03:04:05Z\n---\n\n<!-- otman:comments -->\n## Comments\n",
			}, args: []string{"doctor", "DOC-17", "--json"}},
			{args: []string{"doctor", "DOC-17", "--fix", "--json"}},
			// The filename changes alone: the file wins, and the title follows it.
			{args: []string{"create", "--title", "Snapshot two", "--kind", "issue"}},
			{rm: []string{"vault/Projects/DOC/Issues/DOC-18 Snapshot two.md"},
				write: map[string]string{
					"vault/Projects/DOC/Issues/DOC-18 Renamed by hand.md": "---\nid: DOC-18\ntitle: Snapshot two\nkind: issue\nstatus: open\nlabels: []\nassignee: null\ncreated: 2026-01-02T03:04:05Z\nupdated: 2026-01-02T03:04:05Z\n---\n\n<!-- otman:comments -->\n## Comments\n",
				}, args: []string{"doctor", "DOC-18", "--fix", "--json"}},
			// Both the title and the filename change: a choice, which --prefer settles.
			{args: []string{"create", "--title", "Snapshot three", "--kind", "issue"}},
			{rm: []string{"vault/Projects/DOC/Issues/DOC-19 Snapshot three.md"},
				write: map[string]string{
					"vault/Projects/DOC/Issues/DOC-19 Both sides.md": "---\nid: DOC-19\ntitle: Both edited\nkind: issue\nstatus: open\nlabels: []\nassignee: null\ncreated: 2026-01-02T03:04:05Z\nupdated: 2026-01-02T03:04:05Z\n---\n\n<!-- otman:comments -->\n## Comments\n",
				}, args: []string{"doctor", "DOC-19", "--fix", "--json"}},
			{args: []string{"doctor", "DOC-19", "--fix", "--prefer", "file", "--json"}},
			// The frontmatter Kind changes alone: the file moves to its Kind folder.
			{args: []string{"create", "--title", "Snapshot four", "--kind", "issue"}},
			{write: map[string]string{
				"vault/Projects/DOC/Issues/DOC-20 Snapshot four.md": "---\nid: DOC-20\ntitle: Snapshot four\nkind: prd\nstatus: open\nlabels: []\nassignee: null\ncreated: 2026-01-02T03:04:05Z\nupdated: 2026-01-02T03:04:05Z\n---\n\n<!-- otman:comments -->\n## Comments\n",
			}, args: []string{"doctor", "DOC-20", "--fix", "--json"}},
			// The folder changes alone: the folder wins, and the Kind follows it.
			{args: []string{"create", "--title", "Snapshot five", "--kind", "issue"}},
			{rm: []string{"vault/Projects/DOC/Issues/DOC-21 Snapshot five.md"},
				write: map[string]string{
					"vault/Projects/DOC/Specs/DOC-21 Snapshot five.md": "---\nid: DOC-21\ntitle: Snapshot five\nkind: issue\nstatus: open\nlabels: []\nassignee: null\ncreated: 2026-01-02T03:04:05Z\nupdated: 2026-01-02T03:04:05Z\n---\n\n<!-- otman:comments -->\n## Comments\n",
				}, args: []string{"doctor", "DOC-21", "--fix", "--json"}},
			// The filename loses its title alone: the file wins, but it has no
			// title to give, so the finding is none, even with --prefer.
			{args: []string{"create", "--title", "Snapshot six", "--kind", "issue"}},
			{rm: []string{"vault/Projects/DOC/Issues/DOC-22 Snapshot six.md"},
				write: map[string]string{
					"vault/Projects/DOC/Issues/DOC-22.md": "---\nid: DOC-22\ntitle: Snapshot six\nkind: issue\nstatus: open\nlabels: []\nassignee: null\ncreated: 2026-01-02T03:04:05Z\nupdated: 2026-01-02T03:04:05Z\n---\n\n<!-- otman:comments -->\n## Comments\n",
				}, args: []string{"doctor", "DOC-22", "--fix", "--prefer", "file", "--json"}},
			// The title and the Kind both change, and the file moves to another
			// folder. The title is settled by the frontmatter, which rewrites the
			// file, but the Kind is a choice and is left open. Its side of the
			// snapshot is kept, so once the file is back in its folder, the Kind
			// alone has changed since otman last wrote it.
			{args: []string{"create", "--title", "Keep kind", "--kind", "issue"}},
			{rm: []string{"vault/Projects/DOC/Issues/DOC-23 Keep kind.md"},
				write: map[string]string{
					"vault/Projects/DOC/Specs/DOC-23 Keep kind.md": "---\nid: DOC-23\ntitle: Keep kind retyped\nkind: prd\nstatus: open\nlabels: []\nassignee: null\ncreated: 2026-01-02T03:04:05Z\nupdated: 2026-01-02T03:04:05Z\n---\n\n<!-- otman:comments -->\n## Comments\n",
				}, args: []string{"doctor", "DOC-23", "--fix", "--json"}},
			{rm: []string{"vault/Projects/DOC/Specs/DOC-23 Keep kind retyped.md"},
				write: map[string]string{
					"vault/Projects/DOC/Issues/DOC-23 Keep kind retyped.md": "---\nid: DOC-23\ntitle: Keep kind retyped\nkind: prd\nstatus: open\nlabels: []\nassignee: null\ncreated: 2026-01-02T03:04:05Z\nupdated: 2026-01-02T03:04:05Z\n---\n\n<!-- otman:comments -->\n## Comments\n",
				}, args: []string{"doctor", "DOC-23", "--fix", "--json"}},
		},
	})
}

// A journal that cannot be finished stops every command, so doctor reports
// it as its one finding, exits 4 and leaves the journal in place.
func TestDoctorUnfinishedJournal(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-unfinished-journal", fixture: "doctor",
		files: withConfig(doctorConfig, map[string]string{
			"vault/.otman/journal/retitle-DOC-1.json": "{ not a journal\n",
		}),
		steps: []step{
			{args: []string{"doctor"}},
			{args: []string{"doctor", "--fix", "--json"}},
		},
	})
}

// Bad flags and scopes fail before anything is read: --prefer takes
// frontmatter or file, a REF and --all-projects exclude each other, and
// with no Project selected doctor says so.
func TestDoctorErrors(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-errors", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{args: []string{"doctor", "--prefer", "sideways"}},
			{args: []string{"doctor", "--prefer", ""}},
			{args: []string{"doctor", "DOC-1", "--all-projects"}},
			{args: []string{"doctor", "--all-projects", "--project", "DOC"}},
			{args: []string{"doctor", "NOPE-1"}},
		},
	})
	runGolden(t, goldenCase{
		name: "doctor-no-project", fixture: "doctor",
		files: map[string]string{"config/otman/config.toml": "vault = \"$WORK/vault\"\n"},
		steps: []step{
			{args: []string{"doctor"}},
		},
	})
}
