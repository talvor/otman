package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/talvor/otman/internal/item"
)

// noMarker is an Item file with no comments marker line, whose
// body/comments boundary otman will not guess at when it writes.
const noMarker = "---\nid: OTM-3\ntitle: No marker\nkind: issue\nstatus: open\n---\nBody.\n\n## Comments\n"

// actor is the environment that configures an actor.
var actor = map[string]string{"OTM_ACTOR": "talvor"}

// comment appends a "### <timestamp> · <author>" heading and the text at
// the end of the comments section, sets updated and keeps everything else
// byte for byte. It is not retry-idempotent. A body with its own
// "## Comments" heading keeps it: the marker line is the boundary.
// view --comments and JSON view show the new comments.
func TestComment(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-comment",
		fixture: "items",
		files: withOTM(map[string]string{
			"comment.md": "\n\nFrom a file.\r\nWith a CRLF line.\n\n\n",
		}),
		steps: []step{
			{args: []string{"comment", "OTM-1", "--body", "Checked on a second device."}, env: actor, tty: true},
			{args: []string{"comment", "OTM-1", "--body", "Checked on a second device.", "--json"}, env: actor},
			{args: []string{"comment", "OTM-1", "--body-file", "-", "--actor", "agent-b"}, stdin: "## Comments\n\nA comment with its own heading.\n"},
			{args: []string{"comment", "1", "--body-file", "comment.md", "--json"}, env: actor},
			{args: []string{"view", "OTM-1", "--comments"}, tty: true},
			{args: []string{"view", "OTM-1", "--json"}},
			{args: []string{"create", "--title", "Own heading", "--body", "Intro.\n\n## Comments\n\nNot a real comment."}, env: actor},
			{args: []string{"comment", "OTM-3", "--body", "The first real comment."}, env: actor},
			{args: []string{"view", "OTM-3", "--json"}},
		},
	})
}

// comment keeps a hand-edited file's frontmatter and line endings, and
// writes the comment in the file's own line endings.
func TestCommentHandEdited(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-comment-hand-edited",
		fixture: "handedited",
		files:   map[string]string{"config/otman/config.toml": "vault = \"$WORK/vault\"\nproject = \"HND\"\nactor = \"talvor\"\n"},
		steps: []step{
			{args: []string{"comment", "HND-1", "--body", "Hand-edited frontmatter stays as it was."}},
			{args: []string{"comment", "HND-3", "--body", "Two lines\nin CRLF.", "--json"}},
			{args: []string{"comment", "HND-6", "--body", "Flow style is refused.", "--json"}},
		},
	})
}

// comment needs a REF, an actor and non-empty text, and fails with exit 2
// before the Vault is touched otherwise. A comment can't carry the
// comments marker. An Item without a marker is refused with unsafe_write.
// Nothing is written by a failed comment.
func TestCommentErrors(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-comment-errors",
		fixture: "items",
		files:   withOTM(map[string]string{"vault/Projects/OTM/Issues/OTM-3 No marker.md": noMarker}),
		steps: []step{
			{args: []string{"comment", "--body", "x"}, env: actor},
			{args: []string{"comment", "OTM-1"}, env: actor, tty: true},
			{args: []string{"comment", "OTM-1", "--body", "No actor.", "--json"}},
			{args: []string{"comment", "OTM-1", "--body", "No actor."}, tty: true},
			{args: []string{"comment", "OTM-1", "--body", "", "--json"}, env: actor},
			{args: []string{"comment", "OTM-1", "--body", " \n\t\n"}, env: actor},
			{args: []string{"comment", "OTM-1", "--body-file", "-", "--json"}, env: actor, stdin: "\r\n\r\n"},
			{args: []string{"comment", "OTM-1", "--body", "x", "--body-file", "-"}, env: actor},
			{args: []string{"comment", "OTM-1", "--body", "a\n<!-- otman:comments -->\nb", "--json"}, env: actor},
			{args: []string{"comment", "OTM-1", "--body-file", "missing.md"}, env: actor},
			{args: []string{"comment", "OTM-1", "--body", "x", "--json"}, env: map[string]string{"OTM_ACTOR": "two\nlines"}},
			{args: []string{"comment", "OTM-99", "--body", "x", "--json"}, env: actor},
			{args: []string{"comment", "OTM-3", "--body", "x", "--json"}, env: actor},
		},
	})
}

// close --comment appends the comment and closes the Item in one write. A
// close of a closed Item with a comment still appends it and reports
// changed; without one it is a no-op. A comment needs an actor and
// non-empty text, but a close without one needs neither. A failed close
// writes nothing, so the status never changes without its comment.
func TestCloseComment(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-close-comment",
		fixture: "items",
		files: withOTM(map[string]string{
			"vault/Projects/OTM/Issues/OTM-3 No marker.md": noMarker,
			"reason.md": "Superseded by WEB-1.\n",
		}),
		steps: []step{
			{args: []string{"close", "OTM-1", "--comment", "Fixed in the scanner."}, env: actor, tty: true},
			{args: []string{"close", "OTM-1", "--comment-file", "-", "--json"}, env: actor, stdin: "Retried: the reason is not lost.\n"},
			{args: []string{"close", "OTM-1", "--comment", "Once more, as a human."}, env: actor, tty: true},
			{args: []string{"close", "OTM-1", "--json"}},
			{args: []string{"view", "OTM-1", "--comments"}, tty: true},
			{args: []string{"close", "OTM-2", "--comment-file", "reason.md", "--json"}, env: actor},
			{args: []string{"close", "WEB-1", "--comment", "No actor.", "--json"}},
			{args: []string{"close", "WEB-1", "--comment", "  ", "--json"}, env: actor},
			{args: []string{"close", "WEB-1", "--comment", "x", "--comment-file", "-"}, env: actor},
			{args: []string{"close", "WEB-1", "--comment", "<!-- otman:comments -->"}, env: actor},
			{args: []string{"close", "WEB-1", "--comment-file", "missing.md", "--json"}, env: actor},
			{args: []string{"reopen", "WEB-1", "--comment", "x"}, env: actor},
			{args: []string{"close", "OTM-3", "--comment", "x", "--json"}, env: actor},
		},
	})
}

// For every Item of every fixture Vault, comment either appends the
// comment, in the file's line endings, and changes nothing else but
// updated, or refuses with unsafe_write and leaves the file as it was.
func TestCommentKeepsFile(t *testing.T) {
	eachFixtureItem(t, func(t *testing.T, fixture, rel string) {
		vault := filepath.Join(t.TempDir(), "vault")
		copyTree(t, fixture, vault)
		file := filepath.Join(vault, filepath.FromSlash(rel))
		before, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		code, _, stderr := runVault(vault, 0, "comment", rel, "--body", "Appended.\nTwo lines.", "--actor", "talvor", "--json")
		after, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if code == 4 && strings.Contains(stderr, `"unsafe_write"`) {
			if !bytes.Equal(before, after) {
				t.Fatalf("a refused comment changed the file:\n%q\n%q", before, after)
			}
			return
		}
		if code != 0 {
			t.Fatalf("comment: exit %d: %s", code, stderr)
		}
		eol := "\n"
		if bytes.HasPrefix(before, []byte("---\r\n")) {
			eol = "\r\n"
		}
		added := "### 2026-01-02T03:04:05Z · talvor" + eol + "Appended." + eol + "Two lines." + eol
		kept, appended, ok := bytes.Cut(withoutKeys(after, "updated"), withoutKeys(before, "updated"))
		if !ok || len(kept) != 0 || !bytes.HasSuffix(appended, []byte(eol+added)) ||
			len(bytes.Trim(bytes.TrimSuffix(appended, []byte(added)), "\r\n")) != 0 {
			t.Fatalf("comment changed more than updated and the appended comment:\n%q\n%q", before, after)
		}
		if bytes.Count(after, []byte("updated: 2026-01-02T03:04:05Z"+eol)) != 1 {
			t.Fatalf("comment did not set updated:\n%q", after)
		}
		comments := item.Parse(after).Comments
		if last := comments[len(comments)-1]; last != (item.Comment{Author: "talvor", Created: "2026-01-02T03:04:05Z", Body: "Appended.\nTwo lines."}) {
			t.Fatalf("the last comment reads back as %+v", last)
		}
	})
}
