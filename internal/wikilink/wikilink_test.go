package wikilink_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/talvor/otman/internal/wikilink"
)

var update = flag.Bool("update", false, "rewrite golden files")

// linkJSON is how a golden file shows one scanned link.
type linkJSON struct {
	Raw     string  `json:"raw"`
	Embed   bool    `json:"embed"`
	Target  string  `json:"target"`
	Heading *string `json:"heading"`
	Alias   *string `json:"alias"`
	Name    string  `json:"name"`
}

func toJSON(text string, l wikilink.Link) linkJSON {
	j := linkJSON{Raw: text[l.Start:l.End], Embed: l.Embed, Target: l.Target, Name: l.Name()}
	if l.HasHeading {
		j.Heading = &l.Heading
	}
	if l.HasAlias {
		j.Alias = &l.Alias
	}
	return j
}

// Each testdata/scan/<name>.md fixture is scanned and its links compared
// with <name>.golden: every link form, links in code, and text that only
// looks like a link.
func TestScanFixtures(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join("testdata", "scan", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no fixtures")
	}
	for _, fx := range fixtures {
		t.Run(filepath.Base(fx), func(t *testing.T) {
			b, err := os.ReadFile(fx)
			if err != nil {
				t.Fatal(err)
			}
			text := string(b)
			links := []linkJSON{}
			for _, l := range wikilink.Scan(text) {
				links = append(links, toJSON(text, l))
			}
			var got bytes.Buffer
			enc := json.NewEncoder(&got)
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			if err := enc.Encode(links); err != nil {
				t.Fatal(err)
			}
			golden := strings.TrimSuffix(fx, ".md") + ".golden"
			if *update {
				if err := os.WriteFile(golden, got.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("missing golden %s (run go test -update): %v", golden, err)
			}
			if got.String() != string(want) {
				t.Errorf("golden mismatch for %s\n--- want\n%s\n--- got\n%s", golden, want, got.String())
			}
		})
	}
}

// Parse accepts exactly one whole link, as a relation value holds.
func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in     string
		ok     bool
		target string
	}{
		{"[[OTM-1 Title]]", true, "OTM-1 Title"},
		{"[[OTM-1 Title|alias]]", true, "OTM-1 Title"},
		{"[[OTM-1 Title#Heading]]", true, "OTM-1 Title"},
		{"![[OTM-1 Title]]", true, "OTM-1 Title"},
		{"[[Projects/OTM/Issues/OTM-1 Title.md]]", true, "Projects/OTM/Issues/OTM-1 Title.md"},
		{"[[#Heading]]", true, ""},
		{"OTM-1", false, ""},
		{"[[OTM-1]] trailing", false, ""},
		{" [[OTM-1]]", false, ""},
		{"[[OTM-1]][[OTM-2]]", false, ""},
		{"[[]]", false, ""},
		{"[[ ]]", false, ""},
		{"[[a\nb]]", false, ""},
		{"[OTM-1]", false, ""},
	} {
		l, ok := wikilink.Parse(tc.in)
		if ok != tc.ok || l.Target != tc.target {
			t.Errorf("Parse(%q) = %q, %v; want %q, %v", tc.in, l.Target, ok, tc.target, tc.ok)
		}
	}
}

// Links resolve by basename the way Obsidian does: ignoring case and
// ".md", anywhere in the index for a bare name, and by path suffix for a
// path-qualified target. Several matches are ambiguous; none, dangling.
func TestResolve(t *testing.T) {
	ix := wikilink.NewIndex([]string{
		"Projects/OTM/Issues/OTM-1 Handle sync.md",
		"Projects/OTM/Specs/OTM-2 Format.md",
		"Projects/OTM/Issues/OTM-3 Twin.md",
		"Projects/OTM/PRDs/OTM-3 Twin.md",
	})
	for _, tc := range []struct {
		target string
		want   []string
	}{
		{"OTM-1 Handle sync", []string{"Projects/OTM/Issues/OTM-1 Handle sync.md"}},
		{"otm-1 handle SYNC", []string{"Projects/OTM/Issues/OTM-1 Handle sync.md"}},
		{"OTM-1 Handle sync.md", []string{"Projects/OTM/Issues/OTM-1 Handle sync.md"}},
		{"OTM-1 Handle sync.MD", []string{"Projects/OTM/Issues/OTM-1 Handle sync.md"}},
		{"Specs/OTM-2 Format", []string{"Projects/OTM/Specs/OTM-2 Format.md"}},
		{"Projects/OTM/Specs/OTM-2 Format.md", []string{"Projects/OTM/Specs/OTM-2 Format.md"}},
		{"/Projects/OTM/Specs/OTM-2 Format", []string{"Projects/OTM/Specs/OTM-2 Format.md"}},
		{"Issues/OTM-2 Format", nil},
		{"cs/OTM-2 Format", nil},
		{"OTM-3 Twin", []string{"Projects/OTM/Issues/OTM-3 Twin.md", "Projects/OTM/PRDs/OTM-3 Twin.md"}},
		{"PRDs/OTM-3 Twin", []string{"Projects/OTM/PRDs/OTM-3 Twin.md"}},
		{"OTM-1", nil},
		{"OTM-99 Gone", nil},
		{"", nil},
	} {
		if got := ix.Resolve(tc.target); !slices.Equal(got, tc.want) {
			t.Errorf("Resolve(%q) = %q, want %q", tc.target, got, tc.want)
		}
	}
}
