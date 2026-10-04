// Package wikilink finds Obsidian wikilinks in Markdown and resolves them
// the way Obsidian does. It recognises [[target]], [[target|alias]], the
// [[target\|alias]] of a table, [[target#heading]], ![[embed]] and
// path-qualified targets, and skips links inside code: fenced and
// indented code blocks and inline code spans. It is pure: no file system.
package wikilink

import (
	"path"
	"strings"
)

// Link is one wikilink.
type Link struct {
	// Start and End are the byte offsets of the whole link in the text
	// scanned, the "!" of an embed included.
	Start, End int
	// Embed is true for ![[...]].
	Embed bool
	// Target is the note the link names, as written but trimmed: a name
	// or a path, with or without ".md". It is "" for a link to a heading
	// of the note it is in, such as [[#Heading]].
	Target string
	// Heading is the text after the first #, without it, such as "H" or
	// "^block"; HasHeading reports whether there is a #.
	Heading    string
	HasHeading bool
	// Alias is the text after the first |, or the \| a table escapes
	// it as; HasAlias reports whether there is one.
	Alias    string
	HasAlias bool
}

// Name is the note name the link resolves by: the target's last path
// segment, without ".md".
func (l Link) Name() string {
	if l.Target == "" {
		return ""
	}
	return trimMD(path.Base(l.Target))
}

// Parse reads s as exactly one wikilink, such as a relation value
// "[[OTM-1 Title]]". ok is false for anything else.
func Parse(s string) (Link, bool) {
	l, ok := parseAt(s, 0)
	if !ok || l.End != len(s) {
		return Link{}, false
	}
	return l, true
}

// parseAt reads the wikilink starting at text[i], which is "[[" or "![[".
func parseAt(text string, i int) (Link, bool) {
	l := Link{Start: i}
	if strings.HasPrefix(text[i:], "!") {
		l.Embed = true
		i++
	}
	if !strings.HasPrefix(text[i:], "[[") {
		return Link{}, false
	}
	open := i + 2
	end := strings.Index(text[open:], "]]")
	if end < 0 {
		return Link{}, false
	}
	inner := text[open : open+end]
	if inner == "" || strings.ContainsAny(inner, "\r\n") || strings.Contains(inner, "[[") {
		return Link{}, false
	}
	l.End = open + end + 2
	ref := inner
	if before, alias, ok := strings.Cut(inner, "|"); ok {
		// In a table Obsidian writes the separator as \|, so a pipe
		// does not end the cell; the backslash belongs to it, not to the
		// target or heading.
		ref, l.Alias, l.HasAlias = strings.TrimSuffix(before, `\`), alias, true
	}
	if target, heading, ok := strings.Cut(ref, "#"); ok {
		ref, l.Heading, l.HasHeading = target, heading, true
	}
	l.Target = strings.TrimSpace(ref)
	if l.Target == "" && !l.HasHeading {
		return Link{}, false
	}
	return l, true
}

// Scan returns the wikilinks of a Markdown text in order, leaving out
// those inside fenced and indented code blocks and inline code spans.
func Scan(text string) []Link {
	var links []Link
	for _, r := range proseRanges(text) {
		for i := r.from; i < r.to; {
			j := strings.Index(text[i:r.to], "[[")
			if j < 0 {
				break
			}
			at := i + j
			start := at
			if at > r.from && text[at-1] == '!' {
				start = at - 1
			}
			l, ok := parseAt(text[:r.to], start)
			if !ok {
				i = at + 1
				continue
			}
			links = append(links, l)
			i = l.End
		}
	}
	return links
}

// Retarget is the text of link l, found in text, with its target replaced
// by target. The embed marker, the heading, the alias and any spacing
// around the target are kept byte for byte.
func Retarget(text string, l Link, target string) string {
	s := text[l.Start:l.End]
	open := 2
	if l.Embed {
		open = 3
	}
	// The target is the trimmed text before any # or |; only spacing can
	// come before it.
	at := open + strings.Index(s[open:], l.Target)
	return s[:at] + target + s[at+len(l.Target):]
}

// Rewrite returns text with the target of every link Scan finds replaced
// by what retarget returns for it; a link retarget declines, by returning
// false, is left alone, as are links inside code.
func Rewrite(text string, retarget func(Link) (string, bool)) string {
	var b strings.Builder
	last := 0
	for _, l := range Scan(text) {
		target, ok := retarget(l)
		if !ok {
			continue
		}
		b.WriteString(text[last:l.Start])
		b.WriteString(Retarget(text, l, target))
		last = l.End
	}
	if last == 0 {
		return text
	}
	b.WriteString(text[last:])
	return b.String()
}

// span is the byte range [from, to) of a text.
type span struct{ from, to int }

// proseRanges are the stretches of text outside code blocks and inline
// code spans, in order. A link must lie inside one of them.
func proseRanges(text string) []span {
	var prose []span
	for _, b := range textBlocks(text) {
		prose = append(prose, outsideCodeSpans(text, b)...)
	}
	return prose
}

// textBlocks are the stretches of text outside fenced and indented code
// blocks, split at blank lines, since an inline code span never crosses a
// blank line.
//
// An indented code block is recognised conservatively: a line indented
// four or more columns that follows a blank line, or starts the text,
// outside a list, and the lines after it up to the next non-blank line
// indented less. An indented line straight after a paragraph line
// continues the paragraph, and one in a list may be the list item's
// content, so both stay text.
func textBlocks(text string) []span {
	var blocks []span
	var fence string   // the open fence's run of ` or ~; "" outside a fence
	indented := false  // inside an indented code block
	afterBlank := true // the previous line was blank, or there was none
	inList := false    // a list may still be open
	start := 0         // where the current block began
	offset := 0
	flush := func(end int) {
		if end > start {
			blocks = append(blocks, span{start, end})
		}
	}
	for line := range strings.Lines(text) {
		next := offset + len(line)
		content := strings.TrimRight(line, "\r\n")
		blank := strings.TrimSpace(content) == ""
		if indented && (blank || indent(content) >= 4) {
			start, offset = next, next
			continue
		}
		indented = false
		switch {
		case fence != "":
			if closesFence(content, fence) {
				fence = ""
			}
			start = next
		case openingFence(content) != "":
			flush(offset)
			fence = openingFence(content)
			start = next
		case blank:
			flush(offset)
			start = next
		case indent(content) >= 4 && afterBlank && !inList:
			flush(offset)
			indented = true
			start = next
		case listItem(content):
			inList = true
		case afterBlank && indent(content) < 4:
			inList = false // a paragraph after a blank line ends any list
		}
		afterBlank = blank
		offset = next
	}
	if fence == "" && !indented {
		flush(len(text))
	}
	return blocks
}

// indent is the width of the leading spaces and tabs of line, a tab
// advancing to the next multiple of four columns.
func indent(line string) int {
	n := 0
	for _, c := range line {
		switch c {
		case ' ':
			n++
		case '\t':
			n += 4 - n%4
		default:
			return n
		}
	}
	return n
}

// listItem reports whether line starts a list item: a bullet (-, + or *)
// or an ordered marker (up to nine digits and . or )) followed by a space,
// a tab or the end of the line.
func listItem(line string) bool {
	s := strings.TrimLeft(line, " \t")
	marker := 0
	switch {
	case s == "":
		return false
	case s[0] == '-' || s[0] == '+' || s[0] == '*':
		marker = 1
	default:
		for marker < len(s) && marker < 9 && s[marker] >= '0' && s[marker] <= '9' {
			marker++
		}
		if marker == 0 || marker == len(s) || (s[marker] != '.' && s[marker] != ')') {
			return false
		}
		marker++
	}
	return marker == len(s) || s[marker] == ' ' || s[marker] == '\t'
}

// openingFence is the run of three or more backticks or tildes that opens
// a fenced code block on line, indented at most three spaces, or "".
func openingFence(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || trimmed == "" {
		return ""
	}
	c := trimmed[0]
	if c != '`' && c != '~' {
		return ""
	}
	n := len(trimmed) - len(strings.TrimLeft(trimmed, string(c)))
	if n < 3 {
		return ""
	}
	// A backtick fence's info string cannot hold a backtick.
	if c == '`' && strings.Contains(trimmed[n:], "`") {
		return ""
	}
	return trimmed[:n]
}

// closesFence reports whether line closes a block opened by fence: a run
// of the same character at least as long, indented at most three spaces,
// with nothing after it but spaces.
func closesFence(line, fence string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return false
	}
	run := strings.TrimLeft(trimmed, fence[:1])
	return len(trimmed)-len(run) >= len(fence) && strings.TrimSpace(run) == ""
}

// outsideCodeSpans splits block, a stretch of text, at its inline code
// spans: a run of backticks up to the next run of exactly as many. A run
// with no match is plain text.
func outsideCodeSpans(text string, block span) []span {
	var out []span
	from := block.from
	for i := block.from; i < block.to; {
		if text[i] != '`' {
			i++
			continue
		}
		n := backticks(text, i, block.to)
		closing := -1
		for j := i + n; j < block.to; {
			if text[j] != '`' {
				j++
				continue
			}
			m := backticks(text, j, block.to)
			if m == n {
				closing = j
				break
			}
			j += m
		}
		if closing < 0 {
			i += n
			continue
		}
		if i > from {
			out = append(out, span{from, i})
		}
		i = closing + n
		from = i
	}
	if block.to > from {
		out = append(out, span{from, block.to})
	}
	return out
}

// backticks is the length of the run of backticks at text[i], up to end.
func backticks(text string, i, end int) int {
	n := 0
	for i+n < end && text[i+n] == '`' {
		n++
	}
	return n
}

// Index resolves link targets against a set of note paths.
type Index struct {
	byName map[string][]string // lowercased name -> paths
}

// NewIndex indexes paths, Vault-relative and slash-separated, such as
// "Projects/OTM/Issues/OTM-1 Title.md".
func NewIndex(paths []string) *Index {
	ix := &Index{byName: map[string][]string{}}
	for _, p := range paths {
		name := strings.ToLower(trimMD(path.Base(p)))
		ix.byName[name] = append(ix.byName[name], p)
	}
	return ix
}

// Resolve returns the indexed paths that target names, the way Obsidian
// resolves a link, ignoring case and a ".md" extension: a bare name
// matches every note with that name, wherever it is, and a path-qualified
// target matches the notes whose paths end with it. More than one path is
// an ambiguous link and none a dangling one.
func (ix *Index) Resolve(target string) []string {
	target = strings.TrimPrefix(strings.TrimSpace(target), "/")
	if target == "" {
		return nil
	}
	want := strings.ToLower(trimMD(target))
	candidates := ix.byName[strings.ToLower(trimMD(path.Base(target)))]
	if !strings.Contains(want, "/") {
		return candidates
	}
	var out []string
	for _, p := range candidates {
		have := strings.ToLower(trimMD(p))
		if have == want || strings.HasSuffix(have, "/"+want) {
			out = append(out, p)
		}
	}
	return out
}

func trimMD(s string) string {
	if len(s) > 3 && strings.EqualFold(s[len(s)-3:], ".md") {
		return s[:len(s)-3]
	}
	return s
}
