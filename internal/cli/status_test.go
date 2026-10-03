package cli_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// close and reopen set status and updated and report changed. A matching
// state is a no-op with changed:false that leaves the file alone. The
// assignee is kept and children are untouched.
func TestCloseReopen(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-close-reopen",
		fixture: "items",
		files: withOTM(map[string]string{
			"vault/Projects/OTM/Issues/OTM-3 Child of the first.md": relationItem("OTM-3", "Child of the first",
				`"[[OTM-1 Handle sync collisions]]"`, "[]"),
		}),
		steps: []step{
			{args: []string{"close", "OTM-1"}, tty: true},
			{args: []string{"close", "OTM-1", "--json"}},
			{args: []string{"close", "1"}, tty: true},
			{args: []string{"reopen", "OTM-1"}},
			{args: []string{"reopen", "OTM-1", "--json"}},
			{args: []string{"reopen", "OTM-2 Item file format frontmatter and body.md", "--json"}},
			{args: []string{"close", "Projects/WEB/PRDs/WEB-1 Landing page.md"}, tty: true},
			{args: []string{"reopen", "WEB-1"}, tty: true},
		},
	})
}

// close and reopen take exactly one REF and fail like view when it
// matches nothing.
func TestCloseReopenErrors(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-close-reopen-errors",
		fixture: "items",
		files:   withOTM(nil),
		steps: []step{
			{args: []string{"close"}, tty: true},
			{args: []string{"reopen", "OTM-1", "OTM-2", "--json"}},
			{args: []string{"close", "OTM-99", "--json"}},
			{args: []string{"reopen", "Handle sync collisions"}},
			{args: []string{"close", "WEB-1", "--project", "OTM", "--json"}},
		},
	})
}

// Writes splice only status and updated into hand-edited frontmatter,
// keeping comments, blank lines, quoting, unknown keys and line endings,
// and repair a missing or invalid status. Frontmatter otman cannot splice
// safely is refused with unsafe_write and left as it was, even when the
// status already matches.
func TestCloseHandEdited(t *testing.T) {
	runGolden(t, goldenCase{
		name:    "item-close-hand-edited",
		fixture: "handedited",
		files:   map[string]string{"config/otman/config.toml": "vault = \"$WORK/vault\"\nproject = \"HND\"\n"},
		steps: []step{
			{args: []string{"close", "HND-1", "--json"}},
			{args: []string{"close", "HND-2", "--json"}},
			{args: []string{"reopen", "HND-3", "--json"}},
			{args: []string{"close", "HND-4", "--json"}},
			{args: []string{"reopen", "HND-5", "--json"}},
			{args: []string{"close", "HND-6", "--json"}},
			{args: []string{"close", "HND-6"}, tty: true},
			{args: []string{"reopen", "HND-6", "--json"}},
			{args: []string{"close", "HND-7"}},
			{args: []string{"reopen", "HND-8", "--json"}},
			{args: []string{"close", "HND-9", "--json"}},
		},
	})
}

// itemName is an Item filename, <KEY>-<n> and an optional title.
var itemName = regexp.MustCompile(`^[A-Z][A-Z0-9]*-[1-9][0-9]* ?.*\.md$`)

// Byte-identical round trip: for every Item of every fixture Vault, a
// close or reopen that changes nothing reproduces the file's exact bytes,
// and one that changes the status alters only the status and updated
// spans. Files otman refuses to rewrite stay as they were.
func TestRoundTrip(t *testing.T) {
	fixtures, err := os.ReadDir(filepath.Join("testdata", "vaults"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fx := range fixtures {
		src := filepath.Join("testdata", "vaults", fx.Name())
		err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !itemName.MatchString(d.Name()) {
				return err
			}
			rel, _ := filepath.Rel(src, p)
			t.Run(fx.Name()+"/"+d.Name(), func(t *testing.T) { roundTrip(t, src, filepath.ToSlash(rel)) })
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func roundTrip(t *testing.T, fixture, rel string) {
	vault := filepath.Join(t.TempDir(), "vault")
	copyTree(t, fixture, vault)
	file := filepath.Join(vault, filepath.FromSlash(rel))
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	other := map[string]string{"closed": "open", "open": "closed"}
	verb := map[string]string{"closed": "close", "open": "reopen"}

	// set runs close or reopen to status and checks the bytes it leaves.
	set := func(status string) (refused bool) {
		before, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := runVault(vault, 0, verb[status], rel, "--json")
		after, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if code == 4 && strings.Contains(stderr, `"unsafe_write"`) {
			if !bytes.Equal(before, after) {
				t.Fatalf("a refused write changed the file:\n%q\n%q", before, after)
			}
			return true
		}
		if code != 0 {
			t.Fatalf("%s: exit %d: %s", verb[status], code, stderr)
		}
		var res struct {
			Data struct {
				Changed bool `json:"changed"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatal(err)
		}
		if !res.Data.Changed {
			if !bytes.Equal(before, after) {
				t.Fatalf("an unchanged %s rewrote the file:\n%q\n%q", verb[status], before, after)
			}
			return false
		}
		if want := withoutKeys(before); !bytes.Equal(withoutKeys(after), want) {
			t.Fatalf("%s changed more than status and updated:\n%q\n%q", verb[status], before, after)
		}
		eol := "\n"
		if bytes.HasPrefix(before, []byte("---\r\n")) {
			eol = "\r\n"
		}
		for _, line := range []string{"status: " + status + eol, "updated: 2026-01-02T03:04:05Z" + eol} {
			if bytes.Count(after, []byte(line)) != 1 {
				t.Fatalf("%s did not write %q:\n%q", verb[status], line, after)
			}
		}
		return false
	}

	status := "open"
	if m := regexp.MustCompile(`(?m)^status: "?(open|closed)"?\s`).FindSubmatch(original); m != nil {
		status = string(m[1])
	}
	if set(status) { // a no-op when the status is valid
		return
	}
	set(other[status])
	set(status)
	set(status)
}

// withoutKeys drops the status and updated keys from a file's frontmatter:
// each key's line and the indented lines continuing its value.
func withoutKeys(b []byte) []byte {
	var out []byte
	inFrontmatter, dropping := false, false
	for i, line := range bytes.SplitAfter(b, []byte("\n")) {
		trimmed := strings.TrimRight(string(line), "\r\n")
		switch {
		case trimmed == "---":
			inFrontmatter = i == 0
			dropping = false
		case !inFrontmatter:
		case strings.HasPrefix(trimmed, "status:") || strings.HasPrefix(trimmed, "updated:"):
			dropping = true
			continue
		case dropping && (strings.HasPrefix(trimmed, " ") || strings.HasPrefix(trimmed, "\t")) &&
			!strings.HasPrefix(strings.TrimSpace(trimmed), "#"):
			continue
		default:
			dropping = false
		}
		out = append(out, line...)
	}
	return out
}
