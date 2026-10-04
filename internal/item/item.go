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
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/talvor/otman/internal/frontmatter"
	"github.com/talvor/otman/internal/wikilink"
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

// ContainsCommentHeading reports whether text has a line in the exact form
// of a comment heading otman writes, which would read back as the start
// of another comment, splitting on every line break Render normalises.
func ContainsCommentHeading(text string) bool {
	for line := range strings.Lines(normalizeLineEndings(text)) {
		if _, ok := commentHeadingOf(line); ok {
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
	// Parent and BlockedBy are relation wikilinks, such as
	// "[[OTM-1 Title]]"; nil for none.
	Parent    *string
	BlockedBy []string
	Created   time.Time
	Updated   time.Time
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
	add("parent", optionalLink(f.Parent))
	add("blocked_by", links(f.BlockedBy))
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

// quoted is s as a double-quoted scalar, the way relation wikilinks are
// written.
func quoted(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s, Style: yaml.DoubleQuotedStyle}
}

func optionalLink(link *string) *yaml.Node {
	if link == nil {
		return null()
	}
	return quoted(*link)
}

// links is a list of quoted relation wikilinks.
func links(items []string) *yaml.Node { return sequence(quotedAll(items)) }

func quotedAll(items []string) []*yaml.Node {
	nodes := make([]*yaml.Node, len(items))
	for i, s := range items {
		nodes[i] = quoted(s)
	}
	return nodes
}

// sequence is a list of nodes, written [] when empty.
func sequence(nodes []*yaml.Node) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: nodes}
	if len(nodes) == 0 {
		n.Style = yaml.FlowStyle
	}
	return n
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
	// ID is the frontmatter id as written. An Item's identity is its
	// filename prefix; id is a copy that may have drifted from it.
	ID                                                *string
	Title, Status, Author, Assignee, Created, Updated *string
	// Kind is nil when kind is missing or is not a Kind.
	Kind   *Kind
	Labels []string
	// BadValues are the owned values, in frontmatter order, of the wrong
	// shape, such as a labels value that is not a list or an assignee
	// that is a list. Each reads as absent.
	BadValues []BadValue

	// Parent and BlockedBy are the relation wikilinks as written, such as
	// "[[OTM-1 Title]]". A value that is not a wikilink is left out and
	// listed in BadRelations.
	Parent       *string
	BlockedBy    []string
	BadRelations []BadRelation

	Body     string
	Comments []Comment
	// MarkerLines is how many comments marker lines follow the
	// frontmatter: one, unless the file drifted. Without one, the comments
	// section starts at the last "## Comments" heading; with several, at
	// the first marker.
	MarkerLines int
	// StrayText is true when, with no marker line, the last "## Comments"
	// heading is followed by text that is not a comment, before the first
	// comment. A read does not show it, and a comment is refused, since
	// restoring the marker would make it part of the comments.
	StrayText bool

	// HasFrontmatter is true when the file has frontmatter between ---
	// lines. Every write to a file without it is refused.
	HasFrontmatter bool
	// FrontmatterErr is why the frontmatter could not be read, leaving
	// every field nil; nil when it was read or there is none.
	FrontmatterErr error
}

// BadValue is an owned frontmatter value of the wrong shape, which reads
// as absent and which only a command that sets its key replaces.
type BadValue struct {
	Key string
	// Value is the text of a scalar, or nil for a list or mapping.
	Value *string
}

// BadRelation is a relation value that is not a wikilink: the whole
// parent value, or the whole blocked_by value or one of its entries. Such
// a value reads as absent.
type BadRelation struct {
	Key string // parent or blocked_by
	// Entry is true for one entry of a blocked_by list, false for a
	// whole value.
	Entry bool
	// Value is the text of a scalar, or nil for a list or mapping.
	Value *string
}

// Parse reads an Item file. Without a comments marker line, the body ends
// at the last "## Comments" heading, if any.
func Parse(b []byte) Parsed {
	var p Parsed
	fm, rest, ok := frontmatter.Split(b)
	if p.HasFrontmatter = ok; ok {
		p.readFrontmatter(fm)
	}
	section := locateComments(string(rest))
	p.MarkerLines, p.StrayText = section.markers, section.strayText
	body, comments := section.split(string(rest))
	p.Body = strings.TrimRight(body, "\r\n")
	p.Comments = parseComments(comments)
	return p
}

func (p *Parsed) readFrontmatter(fm []byte) {
	// Frontmatter that every write would refuse (flow style, a repeated
	// key, invalid YAML) does not read either.
	var unsafe *frontmatter.UnsafeError
	if err := frontmatter.Check(fm); errors.As(err, &unsafe) {
		p.FrontmatterErr = errors.New(unsafe.Reason)
		return
	} else if err != nil {
		p.FrontmatterErr = err
		return
	}
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
		case "id":
			p.ID = p.readScalar(k, v)
		case "title":
			p.Title = p.readScalar(k, v)
		case "kind":
			if text := p.readScalar(k, v); text != nil {
				if kind, ok := ParseKind(*text); ok {
					p.Kind = &kind
				} else {
					p.BadValues = append(p.BadValues, BadValue{Key: k, Value: text})
				}
			}
		case "status":
			p.Status = scalar(v)
		case "author":
			p.Author = p.readScalar(k, v)
		case "assignee":
			p.Assignee = p.readScalar(k, v)
		case "created":
			p.Created = p.readScalar(k, v)
		case "updated":
			p.Updated = p.readScalar(k, v)
		case "labels":
			if v.Kind != yaml.SequenceNode && !isNull(v) {
				p.BadValues = append(p.BadValues, BadValue{Key: k, Value: scalarText(v)})
			}
			p.Labels = scalars(v)
		case "parent":
			if l := scalar(v); l != nil && IsLink(*l) {
				p.Parent = l
			} else if !isNull(v) {
				p.BadRelations = append(p.BadRelations, BadRelation{Key: "parent", Value: scalarText(v)})
			}
		case "blocked_by":
			if v.Kind != yaml.SequenceNode {
				if !isNull(v) {
					p.BadRelations = append(p.BadRelations, BadRelation{Key: "blocked_by", Value: scalarText(v)})
				}
				break
			}
			for _, c := range v.Content {
				if l := scalar(c); l != nil && IsLink(*l) {
					p.BlockedBy = append(p.BlockedBy, *l)
				} else {
					p.BadRelations = append(p.BadRelations, BadRelation{Key: "blocked_by", Entry: true, Value: scalarText(c)})
				}
			}
		}
	}
}

// readScalar is the text of v, the value of the owned key k, or nil when
// it is null. A list or mapping reads as nil too, and is noted in
// p.BadValues.
func (p *Parsed) readScalar(k string, v *yaml.Node) *string {
	if v.Kind != yaml.ScalarNode {
		p.BadValues = append(p.BadValues, BadValue{Key: k})
	}
	return scalar(v)
}

// isNull reports whether n is a null scalar, such as null, ~ or nothing.
func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

// scalarText is a scalar's text as written, null included, or nil for a
// list or mapping.
func scalarText(n *yaml.Node) *string {
	if n.Kind != yaml.ScalarNode {
		return nil
	}
	s := n.Value
	return &s
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
// updated time to now, splicing only those keys (ADR 0006). A comment that
// is not nil is appended in the same rewrite, which changes the file even
// when the status already matches. An Item that already has status, with
// no comment, is returned unchanged, but only once the splicer has
// accepted it: a file otman could not rewrite fails with a
// *frontmatter.UnsafeError either way.
func SetStatus(file []byte, d Derived, status string, comment *Comment, now time.Time) (out []byte, changed bool, err error) {
	var transform func([]byte) ([]byte, error)
	if comment != nil {
		transform = func(b []byte) ([]byte, error) { return appendComment(b, *comment) }
	}
	return rewrite(file, d, []frontmatter.Edit{{Key: "status", Value: str(status)}}, transform, now)
}

// AppendComment appends c at the end of an Item file's comments section
// and sets its updated time to now. Everything already in the file is
// kept byte for byte, and the comment uses the file's line endings. A
// missing comments marker line is restored, unless text that is not a
// comment follows the last "## Comments" heading (ErrStrayText); a file
// with several fails with ErrDuplicateMarkers, and one otman could not
// rewrite with a *frontmatter.UnsafeError.
func AppendComment(file []byte, d Derived, c Comment, now time.Time) ([]byte, error) {
	out, _, err := rewrite(file, d, nil, func(b []byte) ([]byte, error) { return appendComment(b, c) }, now)
	return out, err
}

// Update is an edit to an Item file. A nil field is left alone.
type Update struct {
	// Title sets the title, and replaces the old title among the aliases.
	Title *string
	Kind  *Kind
	// Body replaces the main body; "" clears it.
	Body *string
	// Assignee sets the assignee; ClearAssignee clears it.
	Assignee      *string
	ClearAssignee bool
	// AddLabels and RemoveLabels are valid, lowercase Labels to add and
	// remove. Adding a Label the Item has, or removing one it lacks,
	// changes nothing.
	AddLabels, RemoveLabels []string
	// Parent sets the parent to a relation wikilink; ClearParent clears
	// it.
	Parent      *string
	ClearParent bool
	// AddBlockers are relation wikilinks to append to blocked_by, and
	// RemoveBlockers entries to drop from it, exactly as written. Other
	// entries are kept as they are.
	AddBlockers, RemoveBlockers []string
}

// ErrNoMarker refuses a body rewrite of an Item file that has no comments
// marker line, whose body/comments boundary otman will not guess at.
var ErrNoMarker = errors.New("the Item has no " + CommentsMarker + " line")

// ErrStrayText refuses a comment on, or a body rewrite of, an Item file
// with no comments marker line whose last "## Comments" heading is
// followed by text that is not a comment: restoring the marker there
// would make that text part of the comments (ADR 0005).
var ErrStrayText = errors.New("the Item has no " + CommentsMarker + " line, and text after its last " +
	CommentsHeading + " heading is not a comment")

// ErrDuplicateMarkers refuses a body rewrite of, or a comment on, an Item
// file with more than one comments marker line, whose body/comments
// boundary otman will not guess at.
var ErrDuplicateMarkers = errors.New("the Item has more than one " + CommentsMarker + " line")

// Apply makes update to an Item file and, when that changes it, sets its
// updated time to now. Frontmatter keys are spliced (ADR 0006); a body
// rewrite replaces only the text between the frontmatter and the comments
// marker, keeping the comments section byte for byte, and uses the file's
// line endings. As with SetStatus, a file otman could not rewrite fails
// with a *frontmatter.UnsafeError, or ErrNoMarker, ErrStrayText or
// ErrDuplicateMarkers for a body rewrite, even when update would change
// nothing.
func Apply(file []byte, d Derived, update Update, now time.Time) (out []byte, changed bool, err error) {
	var edits []frontmatter.Edit
	if update.Title != nil {
		edits = append(edits, frontmatter.Edit{Key: "title", Value: str(*update.Title)})
		if aliases := aliasesFor(file, *update.Title); aliases != nil {
			edits = append(edits, frontmatter.Edit{Key: "aliases", Value: aliases})
		}
	}
	if update.Kind != nil {
		edits = append(edits, frontmatter.Edit{Key: "kind", Value: str(string(*update.Kind))})
	}
	if update.ClearAssignee {
		edits = append(edits, frontmatter.Edit{Key: "assignee", Value: null()})
	} else if update.Assignee != nil {
		edits = append(edits, frontmatter.Edit{Key: "assignee", Value: str(*update.Assignee)})
	}
	edits = append(edits, labelEdits(file, update.AddLabels, update.RemoveLabels)...)
	if update.ClearParent {
		edits = append(edits, frontmatter.Edit{Key: "parent", Value: null()})
	} else if update.Parent != nil {
		edits = append(edits, frontmatter.Edit{Key: "parent", Value: quoted(*update.Parent)})
	}
	if len(update.AddBlockers) > 0 || len(update.RemoveBlockers) > 0 {
		blockers, err := blockerList(file, update.AddBlockers, update.RemoveBlockers)
		if err != nil {
			return nil, false, err
		}
		edits = append(edits, frontmatter.Edit{Key: "blocked_by", Value: blockers})
	}
	var transform func([]byte) ([]byte, error)
	if body := update.Body; body != nil {
		transform = func(b []byte) ([]byte, error) { return replaceBody(b, *body) }
	}
	return rewrite(file, d, edits, transform, now)
}

// rewrite splices edits into file, then applies transform to the text
// when it is not nil. When that changes the file, it does so again with
// updated set to now and, unless edits set them, the lossless Drift of
// the file healed: the identity keys restored from d (see healEdits) and
// the labels lowercased and deduped where nothing else is lost (see
// healLabels). A write that changes nothing heals nothing.
func rewrite(file []byte, d Derived, edits []frontmatter.Edit, transform func([]byte) ([]byte, error), now time.Time) ([]byte, bool, error) {
	apply := func(edits []frontmatter.Edit) ([]byte, error) {
		out, err := frontmatter.Splice(file, edits)
		if err != nil || transform == nil {
			return out, err
		}
		return transform(out)
	}
	out, err := apply(edits)
	if err != nil || bytes.Equal(out, file) {
		return out, false, err
	}
	// The full slice expression makes append copy rather than write into
	// the caller's backing array.
	edits = append(edits[:len(edits):len(edits)], healEdits(file, d, edits)...)
	edits = append(edits, frontmatter.Edit{Key: "updated", Value: timestamp(now)})
	if heal := healLabels(file); heal != nil && !sets(edits, "labels") {
		edits = append(edits, *heal)
	}
	out, err = apply(edits)
	return out, err == nil, err
}

// aliasesFor is the aliases list of file with its current title replaced
// by title, the way Render pairs them, or nil when the list does not hold
// the current title, or is not a list, and so is left alone. Other
// entries are kept as they are.
func aliasesFor(file []byte, title string) *yaml.Node {
	old, aliases := frontmatterValue(file, "title"), frontmatterValue(file, "aliases")
	if old == nil || scalar(old) == nil || aliases == nil || aliases.Kind != yaml.SequenceNode {
		return nil
	}
	from := *scalar(old)
	if from == title {
		return nil
	}
	has := func(s string) bool {
		return slices.ContainsFunc(aliases.Content, func(c *yaml.Node) bool {
			v := scalar(c)
			return v != nil && *v == s
		})
	}
	if !has(from) {
		return nil
	}
	out := *aliases
	out.Content = nil
	replaced := has(title) // the title is already an alias: drop the old one
	for _, c := range aliases.Content {
		switch v := scalar(c); {
		case v == nil || *v != from:
			out.Content = append(out.Content, c)
		case !replaced:
			out.Content = append(out.Content, str(title))
			replaced = true
		}
	}
	return &out
}

// RetargetLinks rewrites the wikilinks of file that retarget accepts: the
// relation values in its frontmatter (parent and the entries of
// blocked_by) and every link in the text after it, comments included.
// retarget is given a link's target as written and returns the new one,
// or false to leave the link alone. Only the target changes; the alias,
// heading and embed marker are kept, links inside code are left alone,
// and updated is not touched, since the file's own content has not
// changed. Relation keys are spliced (ADR 0006), so a file whose
// relations need rewriting but cannot be spliced fails with a
// *frontmatter.UnsafeError. A file with nothing to rewrite is returned
// unchanged.
func RetargetLinks(file []byte, retarget func(target string) (string, bool)) ([]byte, error) {
	link := func(l wikilink.Link) (string, bool) {
		if l.Target == "" {
			return "", false
		}
		return retarget(l.Target)
	}
	relation := func(n *yaml.Node) (*yaml.Node, bool) {
		s := scalar(n)
		if s == nil {
			return nil, false
		}
		l, ok := wikilink.Parse(*s)
		if !ok {
			return nil, false
		}
		target, ok := link(l)
		if !ok {
			return nil, false
		}
		return quoted(wikilink.Retarget(*s, l, target)), true
	}
	var edits []frontmatter.Edit
	if v := frontmatterValue(file, "parent"); v != nil {
		if n, ok := relation(v); ok {
			edits = append(edits, frontmatter.Edit{Key: "parent", Value: n})
		}
	}
	if v := frontmatterValue(file, "blocked_by"); v != nil && v.Kind == yaml.SequenceNode {
		out, changed := *v, false
		out.Content = make([]*yaml.Node, len(v.Content))
		for i, c := range v.Content {
			out.Content[i] = c
			if n, ok := relation(c); ok {
				out.Content[i], changed = n, true
			}
		}
		if changed {
			edits = append(edits, frontmatter.Edit{Key: "blocked_by", Value: &out})
		}
	}
	out := file
	if len(edits) > 0 {
		var err error
		if out, err = frontmatter.Splice(file, edits); err != nil {
			return nil, err
		}
	}
	_, rest, _ := frontmatter.Split(out)
	text := wikilink.Rewrite(string(rest), link)
	if text == string(rest) {
		return out, nil
	}
	head := len(out) - len(rest)
	return append(out[:head:head], text...), nil
}

// Derived is what an Item file's name and folder say about it, which a
// write restores into frontmatter that lacks or contradicts it (ADR 0005).
type Derived struct {
	// ID is the filename prefix, the Item's identity.
	ID string
	// Title is the title part of the filename.
	Title string
	// Kind is the Kind of the folder the file is in; nil outside a Kind
	// folder.
	Kind *Kind
}

// healEdits are the edits that heal the identity Drift of file, unless
// edits already set those keys: an id other than d.ID is reset from the
// filename, and a missing or null title or kind is filled in from d. A
// title or kind that is present, even of the wrong shape, is kept.
func healEdits(file []byte, d Derived, edits []frontmatter.Edit) []frontmatter.Edit {
	missing := func(key string) bool {
		v := frontmatterValue(file, key)
		return v == nil || isNull(v)
	}
	var heal []frontmatter.Edit
	if id := frontmatterValue(file, "id"); !sets(edits, "id") && (id == nil || !equalText(scalar(id), d.ID)) {
		heal = append(heal, frontmatter.Edit{Key: "id", Value: str(d.ID)})
	}
	if d.Title != "" && !sets(edits, "title") && missing("title") {
		heal = append(heal, frontmatter.Edit{Key: "title", Value: str(d.Title)})
	}
	if d.Kind != nil && !sets(edits, "kind") && missing("kind") {
		heal = append(heal, frontmatter.Edit{Key: "kind", Value: str(string(*d.Kind))})
	}
	return heal
}

// equalText reports whether text is not nil and is s.
func equalText(text *string, s string) bool { return text != nil && *text == s }

// sets reports whether edits set or remove key.
func sets(edits []frontmatter.Edit, key string) bool {
	return slices.ContainsFunc(edits, func(e frontmatter.Edit) bool { return e.Key == key })
}

// replaceBody replaces the body of file, which has frontmatter, with body
// laid out as Render lays it out, in the line endings of the frontmatter's
// opening line.
func replaceBody(file []byte, body string) ([]byte, error) {
	_, rest, _ := frontmatter.Split(file)
	marker, err := locateComments(string(rest)).marker()
	if err != nil {
		return nil, err
	}
	eol := lineEnding(file)
	var b bytes.Buffer
	b.Write(file[:len(file)-len(rest)])
	if body = strings.TrimRight(normalizeLineEndings(body), "\n"); body != "" {
		b.WriteString(strings.ReplaceAll(body, "\n", eol))
		b.WriteString(eol + eol)
	}
	b.Write(rest[marker:])
	return b.Bytes(), nil
}

// appendComment appends c to file, which has frontmatter, as its last
// entry: one blank line, the "### <created> · <author>" heading and the
// comment's text, in the line endings of the file's first line.
// Leading and trailing line breaks of the text are not kept. A file with
// no comments marker line gets it back where a read found the comments
// section: just before the last "## Comments" heading or, with none, in a
// new section at the end. When text that is not a comment follows that
// heading, it fails with ErrStrayText rather than make the text part of
// the comments; with several marker lines, with ErrDuplicateMarkers.
func appendComment(file []byte, c Comment) ([]byte, error) {
	_, rest, _ := frontmatter.Split(file)
	eol := lineEnding(file)
	section := locateComments(string(rest))
	if _, err := section.marker(); errors.Is(err, ErrNoMarker) {
		file = restoreMarker(file, len(file)-len(rest), section.start, eol)
	} else if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.Write(file)
	b.WriteString(blankLine(file, eol))
	b.WriteString("### " + c.Created + " · " + c.Author + eol)
	text := strings.Trim(normalizeLineEndings(c.Body), "\n")
	b.WriteString(strings.ReplaceAll(text, "\n", eol) + eol)
	return b.Bytes(), nil
}

// lineEnding is the line break of file's first line: CRLF or LF.
func lineEnding(file []byte) string {
	if line, _, _ := bytes.Cut(file, []byte("\n")); bytes.HasSuffix(line, []byte("\r")) {
		return "\r\n"
	}
	return "\n"
}

// restoreMarker puts the comments marker line back into file, whose text
// after the frontmatter starts at offset start and has no marker: just
// before its last "## Comments" heading, at offset heading of that text,
// where a read starts the comments section, or, with no heading (-1), at
// the end with a heading after it and a blank line before it. It uses the
// line endings eol.
func restoreMarker(file []byte, start, heading int, eol string) []byte {
	var b bytes.Buffer
	if heading >= 0 {
		b.Write(file[:start+heading])
		b.WriteString(CommentsMarker + eol)
		b.Write(file[start+heading:])
		return b.Bytes()
	}
	b.Write(file)
	if len(file) > start {
		b.WriteString(blankLine(file, eol))
	}
	b.WriteString(CommentsMarker + eol + CommentsHeading + eol)
	return b.Bytes()
}

// blankLine is what ends the last line of file and leaves one blank line
// after it, in the line endings eol: nothing when file already ends in a
// blank line.
func blankLine(file []byte, eol string) string {
	switch {
	case !bytes.HasSuffix(file, []byte("\n")):
		return eol + eol
	case !bytes.HasSuffix(file, []byte("\n\n")) && !bytes.HasSuffix(file, []byte("\n\r\n")):
		return eol
	}
	return ""
}

// commentsSection is where the comments section of text, an Item file
// after its frontmatter, starts.
type commentsSection struct {
	// markers is how many comments marker lines text has.
	markers int
	// start is the offset of the first marker line or, with none, of the
	// last "## Comments" heading; -1 when text has neither.
	start int
	// strayText is true when, with no marker line, a line after the last
	// "## Comments" heading and before the first comment is not blank.
	strayText bool
}

// locateComments finds the comments section of text, an Item file after
// its frontmatter, in one pass.
func locateComments(text string) commentsSection {
	s := commentsSection{start: -1}
	heading, offset := -1, 0
	for line := range strings.Lines(text) {
		switch {
		case isLine(line, CommentsMarker):
			if s.markers == 0 {
				s.start = offset
			}
			s.markers++
		case isLine(line, CommentsHeading):
			heading = offset
		}
		offset += len(line)
	}
	if s.markers == 0 && heading >= 0 {
		s.start = heading
		_, after, _ := strings.Cut(text[heading:], "\n")
		for line := range strings.Lines(after) {
			if _, ok := commentHeadingOf(line); ok {
				break
			}
			if strings.TrimSpace(line) != "" {
				s.strayText = true
				break
			}
		}
	}
	return s
}

// marker is the offset of the one comments marker line, or fails with
// ErrNoMarker (ErrStrayText when text that is not a comment follows the
// last "## Comments" heading) or ErrDuplicateMarkers.
func (s commentsSection) marker() (int, error) {
	switch {
	case s.markers == 1:
		return s.start, nil
	case s.markers > 1:
		return -1, ErrDuplicateMarkers
	case s.strayText:
		return -1, ErrStrayText
	}
	return -1, ErrNoMarker
}

// split splits text, located as s, into the body and the comments that
// follow the section's heading: at the first marker line or, without
// one, at the last "## Comments" heading.
func (s commentsSection) split(text string) (body, comments string) {
	if s.start < 0 {
		return text, ""
	}
	_, after, _ := strings.Cut(text[s.start:], "\n")
	if s.markers > 0 {
		first, _, _ := strings.Cut(after, "\n")
		if isLine(first, CommentsHeading) {
			after = strings.TrimPrefix(after, first)
			after = strings.TrimPrefix(after, "\n")
		}
	}
	return text[:s.start], after
}

var commentHeading = regexp.MustCompile(`^### (\S+) · (.+?)\s*$`)

// commentHeadingOf reads line as a "### <created> · <author>" heading, as
// appendComment writes it: created must be a UTC RFC3339 timestamp in
// the form otman formats. Any other "### a · b" line is comment text.
func commentHeadingOf(line string) (Comment, bool) {
	m := commentHeading.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
	if m == nil {
		return Comment{}, false
	}
	t, err := time.Parse(time.RFC3339, m[1])
	if err != nil || t.UTC().Format(time.RFC3339) != m[1] {
		return Comment{}, false
	}
	return Comment{Created: m[1], Author: m[2]}, true
}

// parseComments reads the "### <timestamp> · <author>" entries of a
// comments section. A comment's text reads with LF line breaks, whatever
// the file uses.
func parseComments(text string) []Comment {
	out := []Comment{}
	var cur *Comment
	var body strings.Builder
	flush := func() {
		if cur != nil {
			cur.Body = strings.Trim(normalizeLineEndings(body.String()), "\n")
			out = append(out, *cur)
		}
		body.Reset()
	}
	for line := range strings.Lines(text) {
		if c, ok := commentHeadingOf(line); ok {
			flush()
			cur = &c
			continue
		}
		if cur != nil {
			body.WriteString(line)
		}
	}
	flush()
	return out
}

// IsLink reports whether s is a relation wikilink: one whole link to a
// note, such as "[[OTM-1 Title]]", optionally with a heading, an alias,
// a path or the embed marker.
func IsLink(s string) bool {
	l, ok := wikilink.Parse(s)
	return ok && l.Target != ""
}

// Link is the relation wikilink to the Item file named name: its full
// filename without ".md".
func Link(name string) string { return "[[" + strings.TrimSuffix(name, ".md") + "]]" }

// ErrBlockersNotList refuses to rewrite a blocked_by value that is
// neither a list nor null, which otman would have to discard.
var ErrBlockersNotList = errors.New("its blocked_by is not a list, and rewriting it would lose that value")

// blockerList is the blocked_by value of file with the entries remove
// names dropped and the links add appended as quoted scalars. Entries of
// an existing list are otherwise kept as they are. A missing or null
// value is no blockers; any other value that is not a list fails with
// ErrBlockersNotList rather than be replaced.
func blockerList(file []byte, add, remove []string) (*yaml.Node, error) {
	var entries []*yaml.Node
	if v := frontmatterValue(file, "blocked_by"); v != nil && v.Kind == yaml.SequenceNode {
		for _, c := range v.Content {
			if s := scalar(c); s == nil || !slices.Contains(remove, *s) {
				entries = append(entries, c)
			}
		}
	} else if v != nil && !isNull(v) {
		return nil, ErrBlockersNotList
	}
	return sequence(append(entries, quotedAll(add)...)), nil
}

// frontmatterValue is the value of the top-level key of file's
// frontmatter, or nil when it is missing or the frontmatter cannot be read.
func frontmatterValue(file []byte, key string) *yaml.Node {
	fm, _, found := frontmatter.Split(file)
	if !found {
		return nil
	}
	var doc yaml.Node
	if yaml.Unmarshal(fm, &doc) != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	m := doc.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// TruncateText cuts s to at most limit Unicode characters, reporting
// whether it cut anything.
func TruncateText(s string, limit int) (string, bool) {
	if utf8.RuneCountInString(s) <= limit {
		return s, false
	}
	return string([]rune(s)[:limit]), true
}
