package item

import (
	"errors"
	"strings"
	"time"

	"github.com/talvor/otman/internal/frontmatter"
	"github.com/talvor/otman/internal/wikilink"
)

// Repairs are the deterministic fixes doctor makes to an Item file (ADR
// 0005). A field left at its zero value changes nothing.
type Repairs struct {
	// ID resets the frontmatter id to the Item's identity, the filename
	// prefix.
	ID bool
	// Title sets the title, replacing the old one among the aliases.
	Title *string
	// Kind sets the kind.
	Kind *Kind
	// Labels replaces the labels with exactly these.
	Labels *[]string
	// Relink retargets each parent and blocked_by link it accepts, given the
	// relation key it is in, and leaves every other link alone, the body
	// included.
	Relink func(key, target string) (string, bool)
	// Marker restores the comments marker line, as RestoreMarker does.
	Marker bool
}

// ErrNotRestorable refuses to restore the comments marker of an Item file
// whose trailing text does not parse as comments, or that has no "##
// Comments" heading to restore it before.
var ErrNotRestorable = errors.New("the comments marker cannot be restored without losing text")

// Any reports whether r asks for any change at all.
func (r Repairs) Any() bool {
	return r.ID || r.Title != nil || r.Kind != nil || r.Labels != nil || r.Relink != nil || r.Marker
}

// Repair applies r to Item file, whose identity and Kind folder are d, and
// reports the file's bytes after it and whether they changed. It changes
// only what r asks for, and sets the updated time to now. A repair that
// changes nothing returns the file unchanged.
func Repair(file []byte, d Derived, r Repairs, now time.Time) ([]byte, bool, error) {
	var edits []frontmatter.Edit
	if r.ID {
		edits = append(edits, frontmatter.Edit{Key: "id", Value: str(d.ID)})
	}
	if r.Title != nil {
		edits = append(edits, frontmatter.Edit{Key: "title", Value: str(*r.Title)})
		if aliases := aliasesFor(file, *r.Title); aliases != nil {
			edits = append(edits, frontmatter.Edit{Key: "aliases", Value: aliases})
		}
	}
	if r.Kind != nil {
		edits = append(edits, frontmatter.Edit{Key: "kind", Value: str(string(*r.Kind))})
	}
	if r.Labels != nil {
		edits = append(edits, frontmatter.Edit{Key: "labels", Value: list(*r.Labels)})
	}
	if r.Relink != nil {
		edits = append(edits, retargetRelations(file, func(key string, l wikilink.Link) (string, bool) {
			if l.Target == "" {
				return "", false
			}
			return r.Relink(key, l.Target)
		})...)
	}
	var transform func([]byte) ([]byte, error)
	if r.Marker {
		transform = RestoreMarker
	}
	return rewrite(file, d, edits, transform, now, false)
}

// RestorableMarker reports whether file, an Item file with frontmatter, has
// no comments marker line but a "## Comments" heading whose trailing text
// parses as comments, so that RestoreMarker can put the marker back without
// losing anything.
func RestorableMarker(file []byte) bool {
	_, rest, ok := frontmatter.Split(file)
	if !ok {
		return false
	}
	section := locateComments(string(rest))
	return section.markers == 0 && section.start >= 0 && trailingComments(string(rest)[section.start:])
}

// RestoreMarker puts the comments marker line back just before the last
// "## Comments" heading of file, an Item file with frontmatter that has no
// marker line, where a read already finds the comments section. It fails
// with ErrNotRestorable unless RestorableMarker holds.
func RestoreMarker(file []byte) ([]byte, error) {
	if !RestorableMarker(file) {
		return nil, ErrNotRestorable
	}
	_, rest, _ := frontmatter.Split(file)
	section := locateComments(string(rest))
	return restoreMarker(file, len(file)-len(rest), section.start, lineEnding(file)), nil
}

// trailingComments reports whether text, which starts at a "## Comments"
// heading, is that heading followed by nothing but comments: only blank
// lines may come before the first comment heading, and text after that is
// comment text.
func trailingComments(text string) bool {
	_, after, _ := strings.Cut(text, "\n")
	for line := range strings.Lines(after) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		_, ok := commentHeadingOf(line)
		return ok
	}
	return true
}
