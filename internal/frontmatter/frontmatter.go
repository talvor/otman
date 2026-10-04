// Package frontmatter rewrites an Item file's YAML frontmatter by byte
// splicing (ADR 0006). yaml.v3 Nodes are used only to read values and
// locate keys; a write re-serialises just the keys it changes into their
// own byte spans and leaves every other byte, and the body, untouched. It
// is pure: no file system.
package frontmatter

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Edit is one change to a top-level key: set it to Value, or remove it
// when Value is nil.
type Edit struct {
	Key   string
	Value *yaml.Node
}

// UnsafeError is a refusal to write: the frontmatter cannot be spliced
// without guessing at, or losing, content otman does not own.
type UnsafeError struct {
	Reason string
}

func (e *UnsafeError) Error() string { return "unsafe write: " + e.Reason }

func unsafe(format string, args ...any) error {
	return &UnsafeError{Reason: fmt.Sprintf(format, args...)}
}

// Split returns the YAML between a leading --- line and the next --- line,
// and what follows it, and false when the file has none.
func Split(b []byte) (fm, rest []byte, ok bool) {
	start, end, ok := fences(b)
	if !ok {
		return nil, b, false
	}
	return b[start:end], b[end+lineLen(b[end:]):], true
}

// fences locates the frontmatter: start is just after the opening ---
// line and end is at the start of the closing --- line.
func fences(b []byte) (start, end int, ok bool) {
	first := lineLen(b)
	if first == 0 || !isFence(b[:first]) {
		return 0, 0, false
	}
	for i := first; i < len(b); {
		n := lineLen(b[i:])
		if isFence(b[i : i+n]) {
			return first, i, true
		}
		i += n
	}
	return 0, 0, false
}

// lineLen is the length of b's first line, including its "\n".
func lineLen(b []byte) int {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return i + 1
	}
	return len(b)
}

func isFence(line []byte) bool {
	return string(bytes.TrimRight(line, "\r\n")) == "---"
}

// Splice applies edits to the frontmatter of file and returns the new
// file. A changed key's span (from its line to the line before the next
// top-level key, minus trailing blank and comment lines) is replaced by
// the key re-serialised, a removed key's span is deleted, and a new key
// goes just before the closing ---. A key already holding its new value,
// or a removal of an absent key, leaves the bytes alone, so edits that
// change nothing return file unchanged.
//
// The result is re-parsed and every other key checked to be semantically
// unchanged. A file that cannot be spliced safely (no frontmatter, invalid
// YAML, flow style, line breaks other than LF and CRLF) or a result that
// fails the check is refused with an *UnsafeError.
func Splice(file []byte, edits []Edit) ([]byte, error) {
	seen := map[string]bool{}
	for _, e := range edits {
		if seen[e.Key] {
			return nil, fmt.Errorf("frontmatter: key %q edited twice", e.Key)
		}
		seen[e.Key] = true
	}
	start, end, ok := fences(file)
	if !ok {
		return nil, unsafe("the file has no frontmatter between --- lines")
	}
	fm := file[start:end]
	before, err := parse(fm)
	if err != nil {
		return nil, err
	}

	keys := before.keys
	type replacement struct {
		from, to int
		text     []byte
	}
	var reps []replacement
	var added []byte
	want := map[string]*yaml.Node{} // the expected value of each edited key
	for _, e := range edits {
		i := before.index(e.Key)
		if e.Value == nil {
			if i >= 0 {
				reps = append(reps, replacement{keys[i].offset, before.spanEnd(fm, i), nil})
			}
			continue
		}
		var eol string
		if i >= 0 {
			eol = lineEnding(fm[keys[i].offset:])
		} else {
			eol = lineEnding(file)
		}
		text, value, err := render(e.Key, e.Value, eol)
		if err != nil {
			return nil, err
		}
		want[e.Key] = value
		switch {
		case i < 0:
			added = append(added, text...)
		case !equal(keys[i].value, value):
			reps = append(reps, replacement{keys[i].offset, before.spanEnd(fm, i), text})
		}
	}
	if len(reps) == 0 && len(added) == 0 {
		return file, nil
	}

	// Spans never overlap, so splicing in order of position copies each
	// untouched stretch of fm once.
	slices.SortFunc(reps, func(a, b replacement) int { return a.from - b.from })
	var out []byte
	at := 0
	for _, r := range reps {
		out = append(out, fm[at:r.from]...)
		out = append(out, r.text...)
		at = r.to
	}
	out = append(out, fm[at:]...)
	out = append(out, added...)

	if err := verify(before, out, edits, want); err != nil {
		return nil, err
	}
	result := make([]byte, 0, len(file)-len(fm)+len(out))
	result = append(result, file[:start]...)
	result = append(result, out...)
	return append(result, file[end:]...), nil
}

// WrittenAs reports whether the top-level key name of file's frontmatter
// is written byte for byte as Splice would write it holding value: no
// comments, quoting or layout of its own. Splicing another value over such
// a key loses nothing but the value it replaces.
func WrittenAs(file []byte, name string, value *yaml.Node) bool {
	start, end, ok := fences(file)
	if !ok {
		return false
	}
	fm := file[start:end]
	p, err := parse(fm)
	i := p.index(name)
	if err != nil || i < 0 {
		return false
	}
	span := fm[p.keys[i].offset:p.spanEnd(fm, i)]
	text, _, err := render(name, value, lineEnding(span))
	return err == nil && bytes.Equal(span, text)
}

// Check reports why the frontmatter fm, the YAML between the --- lines,
// could not be spliced, as an *UnsafeError, or nil when it could. A read
// that accepts only what Check accepts never shows an Item that every
// write would refuse.
func Check(fm []byte) error {
	_, err := parse(fm)
	return err
}

// key is one top-level key of the frontmatter as parsed.
type key struct {
	name   string
	value  *yaml.Node
	offset int // where the key's line starts in the frontmatter
}

type parsed struct{ keys []key }

func (p parsed) index(name string) int {
	for i, k := range p.keys {
		if k.name == name {
			return i
		}
	}
	return -1
}

// spanEnd is where the span of key i in frontmatter fm ends: at the line
// before the next top-level key, less trailing blank and comment lines.
// The span starts at the key's own line.
func (p parsed) spanEnd(fm []byte, i int) int {
	next := len(fm)
	if i+1 < len(p.keys) {
		next = p.keys[i+1].offset
	}
	return trimTrailing(fm, p.keys[i].offset, next)
}

// parse reads frontmatter fm as a flat block mapping whose keys start
// their own lines in the first column. An empty frontmatter has no keys.
func parse(fm []byte) (parsed, error) {
	// yaml also breaks lines at a lone CR, NEL, LS and PS; locating keys
	// by line would then disagree with the parser.
	if bytes.Contains(bytes.ReplaceAll(fm, []byte("\r\n"), nil), []byte("\r")) ||
		bytes.Contains(fm, []byte("\u0085")) || bytes.Contains(fm, []byte("\u2028")) || bytes.Contains(fm, []byte("\u2029")) {
		return parsed{}, unsafe("the frontmatter has a line break other than LF or CRLF")
	}
	starts := []int{0}
	for i, c := range fm {
		if c == '\n' && i+1 < len(fm) {
			starts = append(starts, i+1)
		}
	}
	for _, s := range starts {
		line := fm[s:]
		if bytes.HasPrefix(line, []byte("...")) && (len(line) == 3 || strings.ContainsRune(" \t\r\n", rune(line[3]))) {
			return parsed{}, unsafe("the frontmatter has a YAML document end marker")
		}
	}

	dec := yaml.NewDecoder(bytes.NewReader(fm))
	var doc yaml.Node
	if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
		return parsed{}, nil
	} else if err != nil {
		return parsed{}, unsafe("the frontmatter is not valid YAML: %v", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return parsed{}, unsafe("the frontmatter is more than one YAML document")
	}
	if len(doc.Content) == 0 {
		return parsed{}, nil // only comments
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return parsed{}, unsafe("the frontmatter is not a mapping of keys to values")
	}
	m := doc.Content[0]
	if m.Style&yaml.FlowStyle != 0 {
		return parsed{}, unsafe("the frontmatter is flow-style YAML")
	}
	var p parsed
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Column != 1 || k.Line < 1 || k.Line > len(starts) ||
			(len(p.keys) > 0 && starts[k.Line-1] <= p.keys[len(p.keys)-1].offset) {
			return parsed{}, unsafe("the frontmatter has a key that does not start its own line")
		}
		if p.index(k.Value) >= 0 {
			return parsed{}, unsafe("the frontmatter repeats the key %q", k.Value)
		}
		p.keys = append(p.keys, key{name: k.Value, value: v, offset: starts[k.Line-1]})
	}
	return p, nil
}

// trimTrailing moves a span's end, initially next, back over trailing
// blank and comment lines, never past the key's own line.
func trimTrailing(fm []byte, from, next int) int {
	end := next
	first := from + lineLen(fm[from:])
	for end > first {
		s := bytes.LastIndexByte(fm[:end-1], '\n') + 1
		if s < first {
			break
		}
		line := bytes.TrimLeft(fm[s:end], " \t")
		if len(bytes.TrimRight(line, "\r\n")) > 0 && line[0] != '#' {
			break
		}
		end = s
	}
	return end
}

// lineEnding is the line break of b's first line: CRLF or LF.
func lineEnding(b []byte) string {
	if n := lineLen(b); n >= 2 && b[n-2] == '\r' && b[n-1] == '\n' {
		return "\r\n"
	}
	return "\n"
}

// render serialises one key in block style, indented by two spaces, and
// returns its text with eol line breaks and the value as it parses back,
// which must be the value given.
func render(name string, value *yaml.Node, eol string) ([]byte, *yaml.Node, error) {
	m := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, value,
	}}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return nil, nil, fmt.Errorf("frontmatter: encoding %q: %w", name, err)
	}
	if err := enc.Close(); err != nil {
		return nil, nil, fmt.Errorf("frontmatter: encoding %q: %w", name, err)
	}
	text := b.Bytes()
	p, err := parse(text)
	if err != nil || len(p.keys) != 1 || p.keys[0].name != name {
		return nil, nil, fmt.Errorf("frontmatter: %q does not encode as one key", name)
	}
	if !equal(p.keys[0].value, value) {
		return nil, nil, fmt.Errorf("frontmatter: the value of %q does not survive encoding", name)
	}
	// A value ending in blank lines (a kept block scalar) would leave them
	// outside its span, where a later write could not find them.
	if trimTrailing(text, 0, len(text)) != len(text) {
		return nil, nil, unsafe("the value of %q ends in blank lines", name)
	}
	if eol != "\n" {
		text = bytes.ReplaceAll(text, []byte("\n"), []byte(eol))
	}
	return text, p.keys[0].value, nil
}

// verify re-parses the spliced frontmatter and checks it holds exactly
// the original keys, in order, less removed ones, with edited keys set to
// want and every other key semantically unchanged, then the new keys.
func verify(before parsed, fm []byte, edits []Edit, want map[string]*yaml.Node) error {
	after, err := parse(fm)
	var refused *UnsafeError
	if errors.As(err, &refused) {
		return unsafe("splicing would corrupt the frontmatter: %s", refused.Reason)
	} else if err != nil {
		return err
	}
	var expected []key
	for _, k := range before.keys {
		if v, edited := want[k.name]; edited {
			expected = append(expected, key{name: k.name, value: v})
		} else if !removed(edits, k.name) {
			expected = append(expected, k)
		}
	}
	for _, e := range edits {
		if e.Value != nil && before.index(e.Key) < 0 {
			expected = append(expected, key{name: e.Key, value: want[e.Key]})
		}
	}
	if len(after.keys) != len(expected) {
		return unsafe("splicing would change other keys of the frontmatter")
	}
	for i, k := range expected {
		if after.keys[i].name != k.name || !equal(after.keys[i].value, k.value) {
			return unsafe("splicing would change the key %q", after.keys[i].name)
		}
	}
	return nil
}

func removed(edits []Edit, name string) bool {
	for _, e := range edits {
		if e.Key == name && e.Value == nil {
			return true
		}
	}
	return false
}

// equalBudget bounds how many nodes one comparison visits, so aliases
// that expand exponentially are refused rather than walked.
const equalBudget = 1 << 16

// equal reports whether two parsed values are semantically the same:
// the same kinds, resolved tags and scalar values (00 is 0), whatever
// their style, comments or position. Aliases compare as the nodes they
// refer to.
func equal(a, b *yaml.Node) bool {
	budget := equalBudget
	return equalNodes(a, b, &budget)
}

func equalNodes(a, b *yaml.Node, budget *int) bool {
	if *budget--; *budget < 0 {
		return false
	}
	for a.Kind == yaml.AliasNode && a.Alias != nil {
		a = a.Alias
	}
	for b.Kind == yaml.AliasNode && b.Alias != nil {
		b = b.Alias
	}
	if a.Kind != b.Kind || a.ShortTag() != b.ShortTag() || len(a.Content) != len(b.Content) {
		return false
	}
	if a.Kind == yaml.ScalarNode && a.Value != b.Value && !sameScalar(a, b) {
		return false
	}
	for i := range a.Content {
		if !equalNodes(a.Content[i], b.Content[i], budget) {
			return false
		}
	}
	return true
}

// sameScalar reports whether two scalars of one tag written differently
// decode to the same value, such as 00 and 0. Printing the values makes
// NaN equal to NaN.
func sameScalar(a, b *yaml.Node) bool {
	var va, vb any
	if a.Decode(&va) != nil || b.Decode(&vb) != nil {
		return false
	}
	return fmt.Sprintf("%#v", va) == fmt.Sprintf("%#v", vb)
}
