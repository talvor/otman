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

// A title edited by hand and a file renamed by hand are a choice, since both
// changed since otman last wrote the Item. A Kind changed by hand moves the
// Item, and the next run still finds the title a choice, rather than
// renaming the file back to the title the snapshot holds.
func TestDoctorHandRenameStaysChoice(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-hand-rename-stays-choice", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{args: []string{"create", "--title", "Snap A", "--kind", "issue"}},
			{rm: []string{"vault/Projects/DOC/Issues/DOC-17 Snap A.md"},
				write: map[string]string{
					"vault/Projects/DOC/Issues/DOC-17 Snap C.md": "---\nid: DOC-17\ntitle: Snap B\nkind: spec\nstatus: open\nlabels: []\nassignee: null\ncreated: 2026-01-02T03:04:05Z\nupdated: 2026-01-02T03:04:05Z\n---\n\n<!-- otman:comments -->\n## Comments\n",
				}, args: []string{"doctor", "DOC-17", "--fix", "--json"}},
			{args: []string{"doctor", "DOC-17", "--fix", "--json"}},
		},
	})
}

// Once --fix settles title or Kind Drift, both sides agree, so the snapshot
// holds them both, and the next hand edit to one side is auto. The title
// edited by hand renames the file, and then the file renamed by hand alone
// retitles the Item. The file renamed by hand retitles the Item, and then the
// title edited by hand alone renames the file. The Kind edited by hand moves
// the file, and then the file moved by hand alone changes the Kind.
func TestDoctorSettledDriftRebaselines(t *testing.T) {
	kindText := func(id, title, kind string) string {
		return "---\nid: " + id + "\ntitle: " + title + "\nkind: " + kind + "\nstatus: open\nassignee: null\n" +
			"created: 2026-01-02T03:04:05Z\nupdated: 2026-01-02T03:04:05Z\n---\n\n<!-- otman:comments -->\n## Comments\n"
	}
	runGolden(t, goldenCase{
		name: "doctor-settled-drift-rebaselines", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{args: []string{"create", "--title", "Alpha", "--kind", "issue"}},
			{write: map[string]string{
				"vault/Projects/DOC/Issues/DOC-17 Alpha.md": doctorItemText("DOC-17", "Alpha renamed", ""),
			}, args: []string{"doctor", "DOC-17", "--fix", "--json"}},
			{rm: []string{"vault/Projects/DOC/Issues/DOC-17 Alpha renamed.md"},
				write: map[string]string{
					"vault/Projects/DOC/Issues/DOC-17 Delta.md": doctorItemText("DOC-17", "Alpha renamed", ""),
				}, args: []string{"doctor", "DOC-17", "--fix", "--json"}},
			{args: []string{"create", "--title", "Beta", "--kind", "issue"}},
			{rm: []string{"vault/Projects/DOC/Issues/DOC-18 Beta.md"},
				write: map[string]string{
					"vault/Projects/DOC/Issues/DOC-18 Delta.md": doctorItemText("DOC-18", "Beta", ""),
				}, args: []string{"doctor", "DOC-18", "--fix", "--json"}},
			{write: map[string]string{
				"vault/Projects/DOC/Issues/DOC-18 Delta.md": doctorItemText("DOC-18", "Epsilon", ""),
			}, args: []string{"doctor", "DOC-18", "--fix", "--json"}},
			{args: []string{"create", "--title", "Gamma", "--kind", "issue"}},
			{write: map[string]string{
				"vault/Projects/DOC/Issues/DOC-19 Gamma.md": kindText("DOC-19", "Gamma", "spec"),
			}, args: []string{"doctor", "DOC-19", "--fix", "--json"}},
			{rm: []string{"vault/Projects/DOC/Specs/DOC-19 Gamma.md"},
				write: map[string]string{
					"vault/Projects/DOC/PRDs/DOC-19 Gamma.md": kindText("DOC-19", "Gamma", "spec"),
				}, args: []string{"doctor", "DOC-19", "--fix", "--json"}},
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

// A hand edit survives an unrelated write. The title is edited by hand, then
// close writes the Item, which must keep the title snapshot it had. After the
// file is also renamed by hand, both sides changed since otman last wrote
// the Item, so the title disagreement is a choice, not a rename to the
// filename.
func TestDoctorHandEditSurvivesWrite(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-hand-edit-survives-write", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{args: []string{"create", "--title", "Hand edit", "--kind", "issue"}},
			{write: map[string]string{
				"vault/Projects/DOC/Issues/DOC-17 Hand edit.md": doctorItemText("DOC-17", "Retyped by hand", ""),
			}, args: []string{"close", "DOC-17"}},
			{rm: []string{"vault/Projects/DOC/Issues/DOC-17 Hand edit.md"},
				write: map[string]string{
					"vault/Projects/DOC/Issues/DOC-17 Renamed by hand.md": doctorItemText("DOC-17", "Retyped by hand", ""),
				}, args: []string{"doctor", "DOC-17", "--fix", "--json"}},
		},
	})
}

// --fix repairs only what it reports. A valid mixed-case label stays as
// written, a label with a space is slugged, and the id is reset.
func TestDoctorFixLeavesUnreportedFields(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-fix-leaves-unreported-fields", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{args: []string{"create", "--title", "Mixed labels", "--kind", "issue"}},
			{write: map[string]string{
				"vault/Projects/DOC/Issues/DOC-17 Mixed labels.md": doctorItemText("DOC-99", "Mixed labels", "labels:\n  - Bug\n  - Needs Review\n"),
			}, args: []string{"doctor", "DOC-17", "--fix", "--json"}},
		},
	})
}

// A dangling link is never repointed at the Item holding it, nor at an Item
// whose parent chain leads back to it, since either would make a self-edge or
// a parent cycle. Those links stay as findings.
func TestDoctorRepointRefusesCycles(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-repoint-refuses-cycles", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{write: map[string]string{
				"vault/Projects/DOC/Issues/DOC-31 New.md": doctorItemText("DOC-31", "New", "parent: \"[[DOC-31 Old name]]\"\n"),
				"vault/Projects/DOC/Issues/DOC-32 X.md":   doctorItemText("DOC-32", "X", "parent: \"[[DOC-33 Gone]]\"\n"),
				"vault/Projects/DOC/Issues/DOC-33 Y.md":   doctorItemText("DOC-33", "Y", "parent: \"[[DOC-32 X]]\"\n"),
			}, args: []string{"doctor", "--fix", "--json"}},
		},
	})
}

// A dangling blocked_by link is repointed when the Item it names does not
// make a blocking cycle: DOC-41 is blocked by DOC-42, whose parent is DOC-41,
// which is allowed. It is refused when the Item would block its own blocker,
// as DOC-44 would by DOC-45, which DOC-44 already blocks.
func TestDoctorRepointBlockedBy(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-repoint-blocked-by", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{write: map[string]string{
				"vault/Projects/DOC/Issues/DOC-41 Blocked.md":       doctorItemText("DOC-41", "Blocked", "blocked_by: [\"[[DOC-42 Gone]]\"]\n"),
				"vault/Projects/DOC/Issues/DOC-42 Holder parent.md": doctorItemText("DOC-42", "Holder parent", "parent: \"[[DOC-41 Blocked]]\"\n"),
				"vault/Projects/DOC/Issues/DOC-44 Waits.md":         doctorItemText("DOC-44", "Waits", "blocked_by: [\"[[DOC-45 Gone]]\"]\n"),
				"vault/Projects/DOC/Issues/DOC-45 Cycle.md":         doctorItemText("DOC-45", "Cycle", "blocked_by: [\"[[DOC-44 Waits]]\"]\n"),
			}, args: []string{"doctor", "--fix", "--json"}},
		},
	})
}

// A repair the vault refuses stays a finding, and the run goes on. The
// repair of DOC-33 that came first is still reported as fixed, and the
// rename of DOC-34 is blocked by the flow-style link in DOC-35, which the
// link rewrite cannot splice.
func TestDoctorBlockedRepairStaysFinding(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-blocked-repair-stays-finding", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{write: map[string]string{
				"vault/Projects/DOC/Issues/DOC-33 Fixed first.md": doctorItemText("DOC-99", "Fixed first", ""),
				"vault/Projects/DOC/Issues/DOC-34 Old name.md":    doctorItemText("DOC-34", "New name", ""),
				"vault/Projects/DOC/Issues/DOC-35 Linked.md": "---\n{id: DOC-35, title: Linked, kind: issue, status: open, labels: [], " +
					"parent: \"[[DOC-34 Old name]]\", created: 2026-01-02T03:04:05Z, updated: 2026-01-02T03:04:05Z}\n---\n\n<!-- otman:comments -->\n## Comments\n",
			}, args: []string{"doctor", "--fix", "--prefer", "frontmatter", "--json"}},
		},
	})
}

// A title that differs only in case from another Item's filename on the
// same number is a target that is taken, once the duplicate rules leave
// it there: an Item whose frontmatter cannot be read is never renumbered.
// --fix leaves the Item as a finding instead of aborting the run.
func TestDoctorDuplicateCaseTarget(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor-duplicate-case-target", fixture: "doctor", files: doctorConfig,
		steps: []step{
			{write: map[string]string{
				"vault/Projects/DOC/Issues/DOC-37 Foo.md": doctorItemText("DOC-37", "foo", ""),
				"vault/Projects/DOC/Issues/DOC-37 foo.md": "---\n{id: DOC-37, title: Other}\n---\n",
			}, args: []string{"doctor", "--fix", "--prefer", "frontmatter", "--json"}},
		},
	})
}

// doctorItemText is the text of an Item file with identity id and title,
// and extra frontmatter lines, such as labels or a parent, after the rest.
func doctorItemText(id, title, extra string) string {
	return "---\nid: " + id + "\ntitle: " + title + "\nkind: issue\nstatus: open\nassignee: null\n" +
		extra + "created: 2026-01-02T03:04:05Z\nupdated: 2026-01-02T03:04:05Z\n---\n\n<!-- otman:comments -->\n## Comments\n"
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
