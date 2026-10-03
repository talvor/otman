package frontmatter_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/talvor/otman/internal/frontmatter"
	"go.yaml.in/yaml/v3"
)

// seedFiles are the Item files of every CLI fixture Vault, plus shapes of
// hand-edited YAML the fixtures do not hold.
func seedFiles(f *testing.F) [][]byte {
	f.Helper()
	var files [][]byte
	err := filepath.WalkDir("../cli/testdata/vaults", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		b, err := os.ReadFile(p)
		files = append(files, b)
		return err
	})
	if err != nil {
		f.Fatal(err)
	}
	return append(files,
		[]byte("---\n---\nbody\n"),
		[]byte("---\n# only a comment\n---\n"),
		[]byte("---\na: &x 1\nb: &x 2\nc: *x\nstatus: open\n---\n"),
		[]byte("---\nbody: |\n  text\n  # not a comment\nstatus: open\n---\n"),
		[]byte("---\nkeep: |+\n  text\n\nstatus: open\n---\n"),
		[]byte("---\nstatus: open\n...\n---\n"),
		[]byte("---\nstatus: open\rnext: 1\n---\n"),
		[]byte("---\n? status\n: open\n---\n"),
		[]byte("---\n  status: open\n  title: x\n---\n"),
		[]byte("---\r\nstatus: open\nupdated: 2026-01-01T11:00:00Z\r\n---\r\n"),
	)
}

var keyName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// FuzzSplice drives the splicer's interface (test seam 2): file bytes and
// one key edit go in, and new bytes or an *UnsafeError come out. A write
// keeps every other key semantically unchanged, leaves the bytes outside
// the edited key alone, sets the edited key and is always valid YAML.
func FuzzSplice(f *testing.F) {
	for _, file := range seedFiles(f) {
		for _, key := range []string{"status", "updated", "title", "labels", "reviewer", "added"} {
			f.Add(file, key, "closed", uint8(0))
			f.Add(file, key, "a,b", uint8(1))
			f.Add(file, key, "", uint8(2))
			f.Add(file, key, "{x: [1, 2]}", uint8(3))
			f.Add(file, key, "2026-01-02T03:04:05Z", uint8(4))
		}
		// An edit to the value a key already holds changes nothing.
		f.Add(file, "status", "open", uint8(0))
		f.Add(file, "updated", "2026-01-01T11:00:00Z", uint8(4))
	}
	f.Fuzz(func(t *testing.T, file []byte, key, value string, op uint8) {
		if !keyName.MatchString(key) {
			t.Skip("otman only writes its own, plain key names")
		}
		v, ok := editValue(value, op)
		if !ok {
			t.Skip()
		}
		edit := frontmatter.Edit{Key: key, Value: v}
		out, err := frontmatter.Splice(file, []frontmatter.Edit{edit})
		var unsafe *frontmatter.UnsafeError
		if errors.As(err, &unsafe) {
			return // refused, never mangled
		}
		if err != nil {
			if op%5 == 3 {
				t.Skip("an arbitrary YAML value need not encode as one key")
			}
			t.Fatalf("Splice: %v", err)
		}
		checkSplice(t, file, out, edit)
		again, err := frontmatter.Splice(out, []frontmatter.Edit{edit})
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("repeating the edit changed the file (%v):\n%q\n%q", err, out, again)
		}
	})
}

// editValue is the edit an op asks for: a string, a list of strings, a
// removal, any YAML value, or a timestamp.
func editValue(value string, op uint8) (*yaml.Node, bool) {
	switch op % 5 {
	case 0:
		n := &yaml.Node{}
		return n, n.Encode(value) == nil
	case 1:
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, s := range strings.Split(value, ",") {
			c := &yaml.Node{}
			if c.Encode(s) != nil {
				return nil, false
			}
			n.Content = append(n.Content, c)
		}
		return n, true
	case 2:
		return nil, true
	case 3:
		var doc yaml.Node
		if yaml.Unmarshal([]byte(value), &doc) != nil || len(doc.Content) != 1 {
			return nil, false
		}
		return doc.Content[0], true
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: value}, utf8.ValidString(value)
	}
}

// topKey is one top-level key as the test reads it.
type topKey struct {
	name  string
	value string // the decoded value, printed
	start int    // where its line starts in the frontmatter
	end   int    // where its line ends
}

func checkSplice(t *testing.T, file, out []byte, edit frontmatter.Edit) {
	t.Helper()
	fm, rest, ok := frontmatter.Split(file)
	if !ok {
		t.Fatalf("spliced a file without frontmatter: %q", file)
	}
	outFM, outRest, ok := frontmatter.Split(out)
	if !ok {
		t.Fatalf("the result lost its frontmatter: %q", out)
	}
	if !bytes.Equal(rest, outRest) || !bytes.Equal(file[:lineEnd(file, 0)], out[:lineEnd(out, 0)]) {
		t.Fatalf("bytes outside the frontmatter changed:\n%q\n%q", file, out)
	}
	before := topKeys(t, fm)
	after := topKeys(t, outFM)

	var want []topKey
	editedBefore := false
	for _, k := range before {
		if k.name != edit.Key {
			want = append(want, k)
			continue
		}
		editedBefore = true
		if edit.Value != nil {
			want = append(want, topKey{name: k.name, value: decoded(t, edit.Value)})
		}
	}
	if !editedBefore && edit.Value != nil {
		want = append(want, topKey{name: edit.Key, value: decoded(t, edit.Value)})
	}
	if len(after) != len(want) {
		t.Fatalf("keys %v, want %v\n%q\n%q", names(after), names(want), fm, outFM)
	}
	for i := range want {
		if after[i].name != want[i].name || after[i].value != want[i].value {
			t.Fatalf("key %d is %q=%s, want %q=%s\n%q\n%q", i, after[i].name, after[i].value,
				want[i].name, want[i].value, fm, outFM)
		}
	}

	// The bytes that differ lie within the edited key's lines (or at the
	// closing fence for a new key): no other key's line is touched.
	// Both ends are taken back to whole lines, so a shared first letter
	// of the next key does not pull the boundary into its line.
	p := 0
	for p < len(fm) && p < len(outFM) && fm[p] == outFM[p] {
		p++
	}
	p = bytes.LastIndexByte(fm[:p], '\n') + 1
	s := 0
	for s < len(fm)-p && s < len(outFM)-p && fm[len(fm)-1-s] == outFM[len(outFM)-1-s] {
		s++
	}
	for s > 0 && fm[len(fm)-s-1] != '\n' {
		s--
	}
	for _, k := range before {
		if k.name != edit.Key && k.end > p && k.start < len(fm)-s {
			t.Fatalf("the line of key %q changed\n%q\n%q", k.name, fm, outFM)
		}
	}
	for i, k := range before {
		if k.name == edit.Key && edit.Value != nil && k.value == want[i].value && !bytes.Equal(file, out) {
			t.Fatalf("setting %q to the value it holds changed the file\n%q\n%q", k.name, file, out)
		}
	}
}

// topKeys parses frontmatter that must be valid YAML: an empty document
// or a mapping.
func topKeys(t *testing.T, fm []byte) []topKey {
	t.Helper()
	dec := yaml.NewDecoder(bytes.NewReader(fm))
	var doc yaml.Node
	if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		t.Fatalf("invalid YAML: %v\n%q", err, fm)
	}
	if len(doc.Content) == 0 {
		return nil
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		t.Fatalf("frontmatter is not a mapping: %q", fm)
	}
	starts := []int{0}
	for i, c := range fm {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	var keys []topKey
	for i := 0; i+1 < len(m.Content); i += 2 {
		start := starts[m.Content[i].Line-1]
		keys = append(keys, topKey{
			name:  m.Content[i].Value,
			value: decoded(t, m.Content[i+1]),
			start: start,
			end:   lineEnd(fm, start),
		})
	}
	return keys
}

func lineEnd(b []byte, from int) int {
	if i := bytes.IndexByte(b[from:], '\n'); i >= 0 {
		return from + i + 1
	}
	return len(b)
}

// decoded is a value as Go sees it, printed so that NaN equals NaN.
func decoded(t *testing.T, n *yaml.Node) string {
	t.Helper()
	var v any
	if err := n.Decode(&v); err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("%#v", v)
}

func names(keys []topKey) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k.name
	}
	return out
}
