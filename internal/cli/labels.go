package cli

import (
	"cmp"
	"slices"
	"strings"

	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

// parseLabels reads the values of a repeatable label flag such as
// --label: lowercased, without repeats, failing with invalid_label on the
// first that is not a valid Label.
func parseLabels(flag string, values []string) ([]string, error) {
	labels := []string{}
	for _, v := range values {
		l, ok := item.ParseLabel(v)
		if !ok {
			return nil, invalid("invalid_label", "invalid Label "+quoteArg(v)+" for "+flag,
				map[string]any{"flag": flag, "label": v, "pattern": item.LabelPattern, "max_length": item.LabelLimit},
				"use lowercase letters, digits and . _ : / -, starting with a letter or digit, at most 64 characters")
		}
		if !slices.Contains(labels, l) {
			labels = append(labels, l)
		}
	}
	return labels, nil
}

// labelSet is the Labels in use, lowercased.
type labelSet map[string]bool

func (s labelSet) add(labels []string) {
	for _, l := range labels {
		s[strings.ToLower(l)] = true
	}
}

// labelsInUse is the Labels the Items of Project key carry, except the
// Item at the Vault-relative path skip. Items whose frontmatter cannot be
// read carry none.
func labelsInUse(v *vault.Vault, key, skip string) (labelSet, error) {
	files, err := v.ItemFiles(key)
	if err != nil {
		return nil, err
	}
	inUse := labelSet{}
	for _, f := range files {
		if f.Path == skip {
			continue
		}
		data, err := v.ReadItemFile(f)
		if err != nil {
			return nil, err
		}
		inUse.add(item.Parse(data).Labels)
	}
	return inUse, nil
}

// closest is up to three Labels in s nearest to label, nearest first: those
// within a few edits of it, or containing it or contained in it.
func (s labelSet) closest(label string) []string {
	type candidate struct {
		name     string
		distance int
	}
	var found []candidate
	limit := max(2, len([]rune(label))/3)
	for l := range s {
		d := editDistance(label, l)
		short := min(len(label), len(l))
		if d <= limit || (short >= 3 && (strings.Contains(l, label) || strings.Contains(label, l))) {
			found = append(found, candidate{l, d})
		}
	}
	slices.SortFunc(found, func(a, b candidate) int {
		return cmp.Or(cmp.Compare(a.distance, b.distance), cmp.Compare(a.name, b.name))
	})
	out := []string{}
	for _, c := range found[:min(3, len(found))] {
		out = append(out, c.name)
	}
	return out
}

// editDistance is the Levenshtein distance between a and b, in runes.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// suggestionHint points at the closest Labels, or at label list.
func suggestionHint(closest []string) string {
	if len(closest) == 0 {
		return "check the spelling; 'otman label list' shows the Labels in use"
	}
	return "did you mean " + strings.Join(closest, ", ") + "? 'otman label list' shows the Labels in use"
}

// newLabelWarnings warn new_label for each of added that no other Item in
// Project key carries.
func newLabelWarnings(key string, added []string, inUse labelSet) []output.Problem {
	var ws []output.Problem
	for _, l := range added {
		if inUse[l] {
			continue
		}
		closest := inUse.closest(l)
		ws = append(ws, output.Warning("new_label",
			"no other Item in "+key+" has the label "+quoteArg(l),
			map[string]any{"label": l, "project": key, "closest": closest},
			suggestionHint(closest)))
	}
	return ws
}

// unknownLabelWarnings warn unknown_label for each of labels, which flag
// names, that no Item in scope carries.
func unknownLabelWarnings(flag string, labels []string, inUse labelSet) []output.Problem {
	var ws []output.Problem
	for _, l := range labels {
		if inUse[l] {
			continue
		}
		closest := inUse.closest(l)
		ws = append(ws, output.Warning("unknown_label",
			"no Item in scope has the label "+quoteArg(l)+" that "+flag+" names",
			map[string]any{"label": l, "flag": flag, "closest": closest},
			suggestionHint(closest)))
	}
	return ws
}
