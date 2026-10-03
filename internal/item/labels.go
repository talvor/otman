package item

import (
	"regexp"
	"slices"
	"strings"

	"github.com/talvor/otman/internal/frontmatter"
	"go.yaml.in/yaml/v3"
)

// LabelLimit is the most characters a Label can have.
const LabelLimit = 64

// LabelPattern is what a Label must match once lowercased.
const LabelPattern = `[a-z0-9][a-z0-9._:/-]*`

var labelRegexp = regexp.MustCompile(`^` + LabelPattern + `$`)

// ParseLabel lowercases s, the way every Label is written, and reports
// whether the result is a valid Label.
func ParseLabel(s string) (string, bool) {
	l := strings.ToLower(s)
	return l, len(l) <= LabelLimit && labelRegexp.MatchString(l)
}

// NormalizeLabels lowercases labels and drops repeats, keeping the first
// of each in order. Labels match case-insensitively, so Bug and bug are
// one Label.
func NormalizeLabels(labels []string) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if l = strings.ToLower(l); !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

// HasLabel reports whether labels carries label, ignoring case.
func HasLabel(labels []string, label string) bool {
	return slices.ContainsFunc(labels, func(l string) bool { return strings.EqualFold(l, label) })
}

// storedLabels is the labels key of file's frontmatter as Parse reads it:
// the plain values of a list, or none for any other value. lossless is
// true when that list is all there is, so rewriting it loses nothing; it
// is false for a list holding null or nested entries, and for a missing
// key or any other value.
func storedLabels(file []byte) (labels []string, lossless bool) {
	fm, _, found := frontmatter.Split(file)
	if !found {
		return nil, false
	}
	var doc yaml.Node
	if yaml.Unmarshal(fm, &doc) != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, false
	}
	m := doc.Content[0]
	var value *yaml.Node
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == "labels" {
			value = m.Content[i+1]
		}
	}
	if value == nil || value.Kind != yaml.SequenceNode {
		return nil, false
	}
	labels = scalars(value)
	return labels, len(labels) == len(value.Content)
}

// labelEdits are the edits an Update makes to the labels of file: none
// when adding and removing leave the same Labels, ignoring case.
func labelEdits(file []byte, add, remove []string) []frontmatter.Edit {
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	// Setting the Labels replaces a value that is not a list, which reads
	// as no Labels, and drops entries of a list that are not plain values.
	stored, _ := storedLabels(file)
	before := NormalizeLabels(stored)
	after := slices.DeleteFunc(slices.Clone(before), func(l string) bool { return HasLabel(remove, l) })
	after = NormalizeLabels(append(after, add...))
	if slices.Equal(before, after) {
		return nil
	}
	return []frontmatter.Edit{{Key: "labels", Value: list(after)}}
}

// healLabels is the edit that lowercases and dedupes the stored labels of
// file, or nil when they need no healing or cannot be rewritten without
// loss. Labels still invalid once lowercased are kept.
func healLabels(file []byte) *frontmatter.Edit {
	stored, lossless := storedLabels(file)
	if !lossless {
		return nil
	}
	if healed := NormalizeLabels(stored); !slices.Equal(stored, healed) {
		return &frontmatter.Edit{Key: "labels", Value: list(healed)}
	}
	return nil
}
