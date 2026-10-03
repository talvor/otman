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
	"slices"
	"strings"
	"testing"
	"time"

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

var keyName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)

// FuzzSplice drives the splicer's interface (test seam 2): file bytes and
// two key edits go in, as close and reopen send them, and new bytes or an
// *UnsafeError come out. A write keeps every other key semantically
// unchanged, leaves every byte outside the edited keys' spans alone, sets
// the edited keys and is always valid YAML.
func FuzzSplice(f *testing.F) {
	for _, file := range seedFiles(f) {
		for _, key := range []string{"status", "updated", "title", "labels", "reviewer", "added"} {
			f.Add(file, key, "closed", uint8(0), "updated", "2026-01-02T03:04:05Z", uint8(4))
			f.Add(file, key, "a,b", uint8(1), "status", "open", uint8(0))
			f.Add(file, key, "", uint8(2), "labels", "", uint8(2))
			f.Add(file, key, "{x: [1, 2]}", uint8(3), "added", "x", uint8(0))
		}
		// Edits to the values keys already hold change nothing.
		f.Add(file, "status", "open", uint8(0), "updated", "2026-01-01T11:00:00Z", uint8(4))
	}
	f.Fuzz(func(t *testing.T, file []byte, key1, value1 string, op1 uint8, key2, value2 string, op2 uint8) {
		if !keyName.MatchString(key1) || !keyName.MatchString(key2) || key1 == key2 {
			t.Skip("otman edits its own, plain key names, each once")
		}
		v1, ok1 := editValue(value1, op1)
		v2, ok2 := editValue(value2, op2)
		if !ok1 || !ok2 {
			t.Skip()
		}
		edits := []frontmatter.Edit{{Key: key1, Value: v1}, {Key: key2, Value: v2}}
		out, err := frontmatter.Splice(file, edits)
		var unsafe *frontmatter.UnsafeError
		if errors.As(err, &unsafe) {
			return // refused, never mangled
		}
		if err != nil {
			if op1%5 == 3 || op2%5 == 3 {
				t.Skip("an arbitrary YAML value need not encode as one key")
			}
			t.Fatalf("Splice: %v", err)
		}
		checkSplice(t, file, out, edits)
		again, err := frontmatter.Splice(out, edits)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("repeating the edits changed the file (%v):\n%q\n%q", err, out, again)
		}
	})
}

// editValue is the edit an op asks for: a string, a list of strings, a
// removal, any YAML value, or an RFC3339 timestamp.
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
		_, err := time.Parse(time.RFC3339, value)
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: value}, err == nil
	}
}

// topKey is one top-level key as the test reads it.
type topKey struct {
	name       string
	value      string // the decoded value, printed
	start, end int    // its span in the frontmatter
}

func checkSplice(t *testing.T, file, out []byte, edits []frontmatter.Edit) {
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
	edited := map[string]*yaml.Node{}
	for _, e := range edits {
		edited[e.Key] = e.Value
	}

	var want []topKey
	for _, k := range before {
		v, ok := edited[k.name]
		switch {
		case !ok:
			want = append(want, k)
		case v != nil:
			want = append(want, topKey{name: k.name, value: decoded(t, v)})
		}
	}
	for _, e := range edits {
		if e.Value != nil && !slices.ContainsFunc(before, func(k topKey) bool { return k.name == e.Key }) {
			want = append(want, topKey{name: e.Key, value: decoded(t, e.Value)})
		}
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

	// Every byte outside the edited keys' spans is kept: comments, blank
	// lines and every other key's lines.
	if a, b := withoutSpans(fm, before, edited), withoutSpans(outFM, after, edited); !bytes.Equal(a, b) {
		t.Fatalf("bytes outside the edited keys changed\n%q\n%q", fm, outFM)
	}
	unchanged := true
	for _, k := range before {
		if v, ok := edited[k.name]; ok && (v == nil || k.value != decoded(t, v)) {
			unchanged = false
		}
	}
	for _, e := range edits {
		if e.Value != nil && !slices.ContainsFunc(before, func(k topKey) bool { return k.name == e.Key }) {
			unchanged = false
		}
	}
	if unchanged && !bytes.Equal(file, out) {
		t.Fatalf("edits to the values keys hold changed the file\n%q\n%q", file, out)
	}
}

// withoutSpans is fm with the spans of the edited keys cut out.
func withoutSpans(fm []byte, keys []topKey, edited map[string]*yaml.Node) []byte {
	var out []byte
	at := 0
	for _, k := range keys {
		if _, ok := edited[k.name]; ok {
			out = append(out, fm[at:k.start]...)
			at = k.end
		}
	}
	return append(out, fm[at:]...)
}

// topKeys parses frontmatter that must be valid YAML: an empty document
// or a mapping. A key's span runs from its line to the line before the
// next key, less trailing blank and comment lines.
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
		keys = append(keys, topKey{
			name:  m.Content[i].Value,
			value: decoded(t, m.Content[i+1]),
			start: starts[m.Content[i].Line-1],
		})
	}
	for i := range keys {
		end := len(fm)
		if i+1 < len(keys) {
			end = keys[i+1].start
		}
		for {
			s := bytes.LastIndexByte(fm[:end-1], '\n') + 1
			line := strings.TrimSpace(string(fm[s:end]))
			if s <= keys[i].start || (line != "" && !strings.HasPrefix(line, "#")) {
				break
			}
			end = s
		}
		keys[i].end = end
	}
	return keys
}

func lineEnd(b []byte, from int) int {
	if i := bytes.IndexByte(b[from:], '\n'); i >= 0 {
		return from + i + 1
	}
	return len(b)
}

// decoded is a value as Go sees it, printed so that NaN equals NaN and
// with every scalar's type, so that 0.0 and 0 differ.
func decoded(t *testing.T, n *yaml.Node) string {
	t.Helper()
	var v any
	if err := n.Decode(&v); err != nil {
		return "error: " + err.Error()
	}
	return typed(v)
}

func typed(v any) string {
	var parts []string
	switch x := v.(type) {
	case []any:
		for _, c := range x {
			parts = append(parts, typed(c))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		for k, c := range x {
			parts = append(parts, fmt.Sprintf("%q: %s", k, typed(c)))
		}
	case map[any]any:
		for k, c := range x {
			parts = append(parts, typed(k)+": "+typed(c))
		}
	default:
		return fmt.Sprintf("%T(%#v)", v, v)
	}
	slices.Sort(parts)
	return "{" + strings.Join(parts, ", ") + "}"
}

func names(keys []topKey) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k.name
	}
	return out
}
