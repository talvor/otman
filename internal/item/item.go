// Package item is the Item file format (ADR 0002, ADR 0004): Kinds and
// their folders, the filename projection of a title, rendering a new Item
// file and reading one back leniently. It is pure: no file system.
package item

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/talvor/otman/internal/frontmatter"
	"go.yaml.in/yaml/v3"
)

// CommentsMarker is the reserved line that separates an Item's body from
// its comments section (ADR 0004).
const CommentsMarker = "<!-- otman:comments -->"

// CommentsHeading follows the marker.
const CommentsHeading = "## Comments"

// Kind is what sort of Item it is.
type Kind string

const (
	Issue Kind = "issue"
	PRD   Kind = "prd"
	Spec  Kind = "spec"
)

// Kinds are the Kinds, in display order.
var Kinds = []Kind{Issue, PRD, Spec}

var folders = map[Kind]string{Issue: "Issues", PRD: "PRDs", Spec: "Specs"}

// ParseKind validates a --kind value.
func ParseKind(s string) (Kind, bool) {
	k := Kind(s)
	_, ok := folders[k]
	return k, ok
}

// Folder is the folder under Projects/<KEY>/ that holds Items of Kind k.
func (k Kind) Folder() string { return folders[k] }

// KindOfFolder returns the Kind whose folder is name.
func KindOfFolder(name string) (Kind, bool) {
	for k, f := range folders {
		if f == name {
			return k, true
		}
	}
	return "", false
}

// The statuses an Item can have.
const (
	// Open is the status of an Item whose work is not done; every new Item
	// is open.
	Open = "open"
	// Closed is the status of an Item whose work is done or abandoned.
	Closed = "closed"
)

// TitleLimit is the most characters of a title the filename carries.
const TitleLimit = 60

// filenameStrip are the sequences a filename drops: those Obsidian cannot
// link to and those file systems reject.
var filenameStrip = strings.NewReplacer(
	"#", "", "|", "", "^", "", ":", "", "%%", "", "[[", "", "]]", "",
	"/", "", `\`, "", "?", "", "*", "", "<", "", ">", "", `"`, "",
)

// FilenameTitle is the sanitised projection of title that the filename
// carries: forbidden sequences removed, whitespace collapsed, and cut to
// TitleLimit characters at a word boundary.
func FilenameTitle(title string) string {
	s := title
	// Removing one sequence can join others ("[#[" becomes "[["), so
	// strip until nothing changes.
	for {
		next := filenameStrip.Replace(s)
		if next == s {
			break
		}
		s = next
	}
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= TitleLimit {
		return s
	}
	if r[TitleLimit] == ' ' {
		return string(r[:TitleLimit])
	}
	cut := strings.LastIndexByte(string(r[:TitleLimit]), ' ')
	if cut <= 0 {
		// One word longer than the limit: cut inside it.
		return string(r[:TitleLimit])
	}
	return string(r[:TitleLimit])[:cut]
}

// Filename is the file name of Item <key>-<n> with title.
func Filename(key string, n int, title string) string {
	name := key + "-" + strconv.Itoa(n)
	if t := FilenameTitle(title); t != "" {
		name += " " + t
	}
	return name + ".md"
}

// KeyPattern is a Project key, the prefix of every Item ID: an uppercase
// letter followed by up to 15 uppercase letters or digits.
const KeyPattern = `[A-Z][A-Z0-9]{0,15}`

// idPattern is an Item ID, <KEY>-<n>, capturing the key and the number.
const idPattern = `(` + KeyPattern + `)-([1-9][0-9]*)`

var (
	idRegexp        = regexp.MustCompile(`^` + idPattern + `$`)
	filenamePattern = regexp.MustCompile(`^` + idPattern + `(?: (.*))?\.md$`)
)

// ParseID reads a qualified Item ID such as OTM-12. ok is false for
// anything else.
func ParseID(s string) (key string, n int, ok bool) {
	m := idRegexp.FindStringSubmatch(s)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}

// ParseFilename reads the identity an Item filename carries: its Project
// key, its number and the title part. ok is false for any other name.
func ParseFilename(name string) (key string, n int, title string, ok bool) {
	m := filenamePattern.FindStringSubmatch(name)
	if m == nil {
		return "", 0, "", false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, "", false
	}
	return m[1], n, m[3], true
}

// ContainsMarker reports whether text has the reserved comments marker as
// one of its lines, splitting on every line break Render normalises.
func ContainsMarker(text string) bool {
	for line := range strings.Lines(normalizeLineEndings(text)) {
		if isLine(line, CommentsMarker) {
			return true
		}
	}
	return false
}

func isLine(line, want string) bool {
	return strings.TrimRight(line, "\r\n") == want
}

// Rev is the first 12 hex characters of SHA-256 over a file's bytes.
func Rev(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

// Fields are the owned frontmatter values of a new Item.
type Fields struct {
	ID       string
	Title    string
	Kind     Kind
	Status   string
	Author   *string
	Assignee *string
	Labels   []string
	Created  time.Time
	Updated  time.Time
}

// Render writes a new Item file: flat frontmatter with every owned key,
// then the body, then the comments marker and heading. New files use LF.
// Trailing line breaks of the body are not kept; a non-empty body is
// separated from the marker by one blank line.
func Render(f Fields, body string) ([]byte, error) {
	labels := f.Labels
	if labels == nil {
		labels = []string{}
	}
	fm := &yaml.Node{Kind: yaml.MappingNode}
	add := func(k string, v *yaml.Node) {
		fm.Content = append(fm.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, v)
	}
	add("id", str(f.ID))
	add("title", str(f.Title))
	add("aliases", list([]string{f.Title}))
	add("kind", str(string(f.Kind)))
	add("status", str(f.Status))
	add("author", optional(f.Author))
	add("parent", null())
	add("blocked_by", list(nil))
	add("labels", list(labels))
	add("assignee", optional(f.Assignee))
	add("created", timestamp(f.Created))
	add("updated", timestamp(f.Updated))

	var b bytes.Buffer
	b.WriteString("---\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	b.WriteString("---\n")
	body = normalizeLineEndings(body)
	if body = strings.TrimRight(body, "\n"); body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	b.WriteString(CommentsMarker + "\n" + CommentsHeading + "\n")
	return b.Bytes(), nil
}

// normalizeLineEndings turns CRLF and lone CR line breaks into LF.
func normalizeLineEndings(s string) string {
	return strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s)
}

func str(s string) *yaml.Node {
	n := &yaml.Node{}
	_ = n.Encode(s) // encoding a string cannot fail
	return n
}

func optional(s *string) *yaml.Node {
	if s == nil {
		return null()
	}
	return str(*s)
}

func null() *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"} }

func list(items []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	if len(items) == 0 {
		n.Style = yaml.FlowStyle
	}
	for _, s := range items {
		n.Content = append(n.Content, str(s))
	}
	return n
}

// timestamp is a plain UTC RFC3339 scalar, unquoted as Obsidian expects.
func timestamp(t time.Time) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: t.UTC().Format(time.RFC3339)}
}

// Comment is one entry of the comments section.
type Comment struct {
	Author  string
	Created string
	Body    string
}

// Parsed is an Item file as read. Reads are lenient (ADR 0005): a value
// that is missing or of the wrong type is nil, and the file still reads.
type Parsed struct {
	Title, Status, Author, Assignee, Created, Updated *string
	Kind                                              *Kind
	Labels                                            []string

	// Parent and BlockedBy are the relation wikilinks as written, such as
	// "[[OTM-1 Title]]". A value that is not a wikilink is left out.
	Parent    *string
	BlockedBy []string

	Body     string
	Comments []Comment

	// FrontmatterErr is why the frontmatter could not be read, leaving
	// every field nil; nil when it was read or there is none.
	FrontmatterErr error
}

// Parse reads an Item file. Without a comments marker line, the body ends
// at the last "## Comments" heading, if any.
func Parse(b []byte) Parsed {
	var p Parsed
	fm, rest, ok := frontmatter.Split(b)
	if !ok {
		rest = b
	} else {
		p.readFrontmatter(fm)
	}
	body, comments := splitComments(string(rest))
	p.Body = strings.TrimRight(body, "\r\n")
	p.Comments = parseComments(comments)
	return p
}

func (p *Parsed) readFrontmatter(fm []byte) {
	var doc yaml.Node
	if err := yaml.Unmarshal(fm, &doc); err != nil {
		p.FrontmatterErr = err
		return
	}
	if len(doc.Content) == 0 {
		return
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		p.FrontmatterErr = errors.New("not a mapping of keys to values")
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i].Value, m.Content[i+1]
		switch k {
		case "title":
			p.Title = scalar(v)
		case "kind":
			if k := scalar(v); k != nil {
				kind := Kind(*k)
				p.Kind = &kind
			}
		case "status":
			p.Status = scalar(v)
		case "author":
			p.Author = scalar(v)
		case "assignee":
			p.Assignee = scalar(v)
		case "created":
			p.Created = scalar(v)
		case "updated":
			p.Updated = scalar(v)
		case "labels":
			p.Labels = scalars(v)
		case "parent":
			if l := scalar(v); l != nil && IsLink(*l) {
				p.Parent = l
			}
		case "blocked_by":
			for _, l := range scalars(v) {
				if IsLink(l) {
					p.BlockedBy = append(p.BlockedBy, l)
				}
			}
		}
	}
}

// scalar is a non-null scalar's text, or nil.
func scalar(n *yaml.Node) *string {
	if n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
		return nil
	}
	s := n.Value
	return &s
}

// scalars is a sequence of non-null scalars, or nil.
func scalars(n *yaml.Node) []string {
	if n.Kind != yaml.SequenceNode {
		return nil
	}
	out := []string{}
	for _, c := range n.Content {
		if s := scalar(c); s != nil {
			out = append(out, *s)
		}
	}
	return out
}

// SetStatus sets an Item file's status and, when that changes it, its
// updated time to now, splicing only those keys (ADR 0006). An Item that
// already has status is returned unchanged, but only once the splicer has
// accepted it: a file otman could not rewrite fails with a
// *frontmatter.UnsafeError either way.
func SetStatus(file []byte, status string, now time.Time) (out []byte, changed bool, err error) {
	return rewrite(file, []frontmatter.Edit{{Key: "status", Value: str(status)}}, nil, now)
}

// Update is an edit to an Item file. A nil field is left alone.
type Update struct {
	Kind *Kind
	// Body replaces the main body; "" clears it.
	Body *string
	// Assignee sets the assignee, or clears it when *Assignee is nil.
	Assignee **string
}

// ErrNoMarker refuses a body rewrite of an Item file that has no comments
// marker line, whose body/comments boundary otman will not guess at.
var ErrNoMarker = errors.New("the Item has no " + CommentsMarker + " line")

// Apply makes update to an Item file and, when that changes it, sets its
// updated time to now. Frontmatter keys are spliced (ADR 0006); a body
// rewrite replaces only the text between the frontmatter and the comments
// marker, keeping the comments section byte for byte, and uses the file's
// line endings. As with SetStatus, a file otman could not rewrite fails
// with a *frontmatter.UnsafeError, or ErrNoMarker for a body rewrite,
// even when update would change nothing.
func Apply(file []byte, update Update, now time.Time) (out []byte, changed bool, err error) {
	var edits []frontmatter.Edit
	if update.Kind != nil {
		edits = append(edits, frontmatter.Edit{Key: "kind", Value: str(string(*update.Kind))})
	}
	if update.Assignee != nil {
		edits = append(edits, frontmatter.Edit{Key: "assignee", Value: optional(*update.Assignee)})
	}
	return rewrite(file, edits, update.Body, now)
}

// rewrite splices edits into file and replaces its body when body is not
// nil. When that changes the file, it does so again with updated set to
// now.
func rewrite(file []byte, edits []frontmatter.Edit, body *string, now time.Time) ([]byte, bool, error) {
	apply := func(edits []frontmatter.Edit) ([]byte, error) {
		out, err := frontmatter.Splice(file, edits)
		if err != nil || body == nil {
			return out, err
		}
		return replaceBody(out, *body)
	}
	out, err := apply(edits)
	if err != nil || bytes.Equal(out, file) {
		return out, false, err
	}
	out, err = apply(append(edits[:len(edits):len(edits)], frontmatter.Edit{Key: "updated", Value: timestamp(now)}))
	return out, err == nil, err
}

// replaceBody replaces the body of file, which has frontmatter, with body
// laid out as Render lays it out, in the line endings of the frontmatter's
// opening line.
func replaceBody(file []byte, body string) ([]byte, error) {
	_, rest, _ := frontmatter.Split(file)
	marker := -1
	offset := 0
	for line := range strings.Lines(string(rest)) {
		if isLine(line, CommentsMarker) {
			marker = offset
			break
		}
		offset += len(line)
	}
	if marker < 0 {
		return nil, ErrNoMarker
	}
	eol := "\n"
	if bytes.HasPrefix(file, []byte("---\r\n")) {
		eol = "\r\n"
	}
	var b bytes.Buffer
	b.Write(file[:len(file)-len(rest)])
	if body = strings.TrimRight(normalizeLineEndings(body), "\n"); body != "" {
		b.WriteString(strings.ReplaceAll(body, "\n", eol))
		b.WriteString(eol + eol)
	}
	b.Write(rest[marker:])
	return b.Bytes(), nil
}

// splitComments splits text after the frontmatter at the comments marker
// into the body and the comments that follow the heading. Without a
// marker it falls back to the last "## Comments" heading.
func splitComments(text string) (body, comments string) {
	var lastHeading = -1
	offset := 0
	for line := range strings.Lines(text) {
		switch {
		case isLine(line, CommentsMarker):
			after := text[offset+len(line):]
			first, _, _ := strings.Cut(after, "\n")
			if isLine(first, CommentsHeading) {
				after = strings.TrimPrefix(after, first)
				after = strings.TrimPrefix(after, "\n")
			}
			return text[:offset], after
		case isLine(line, CommentsHeading):
			lastHeading = offset
		}
		offset += len(line)
	}
	if lastHeading < 0 {
		return text, ""
	}
	rest := text[lastHeading:]
	_, after, _ := strings.Cut(rest, "\n")
	return text[:lastHeading], after
}

var commentHeading = regexp.MustCompile(`^### (\S+) · (.+?)\s*$`)

// parseComments reads the "### <timestamp> · <author>" entries of a
// comments section.
func parseComments(text string) []Comment {
	out := []Comment{}
	var cur *Comment
	var body strings.Builder
	flush := func() {
		if cur != nil {
			cur.Body = strings.Trim(body.String(), "\r\n")
			out = append(out, *cur)
		}
		body.Reset()
	}
	for line := range strings.Lines(text) {
		if m := commentHeading.FindStringSubmatch(strings.TrimRight(line, "\r\n")); m != nil {
			flush()
			cur = &Comment{Created: m[1], Author: m[2]}
			continue
		}
		if cur != nil {
			body.WriteString(line)
		}
	}
	flush()
	return out
}

// wikilink is a whole relation value: [[target]], optionally with a
// #heading and an |alias.
var wikilink = regexp.MustCompile(`^\[\[([^\[\]|#]+)(?:#[^\[\]|]*)?(?:\|[^\[\]]*)?\]\]$`)

// IsLink reports whether s is a relation wikilink.
func IsLink(s string) bool { return wikilink.MatchString(s) }

// LinkName is the note name a relation wikilink points at, which Obsidian
// resolves by basename: the target's last path segment, without ".md".
// ok is false when link is not a wikilink.
func LinkName(link string) (name string, ok bool) {
	m := wikilink.FindStringSubmatch(link)
	if m == nil {
		return "", false
	}
	target := strings.TrimSpace(m[1])
	target = target[strings.LastIndexByte(target, '/')+1:]
	return strings.TrimSuffix(target, ".md"), true
}

// TruncateText cuts s to at most limit Unicode characters, reporting
// whether it cut anything.
func TruncateText(s string, limit int) (string, bool) {
	if utf8.RuneCountInString(s) <= limit {
		return s, false
	}
	return string([]rune(s)[:limit]), true
}
