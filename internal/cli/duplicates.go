package cli

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

// Duplicate numbers (ADR 0003) come from a second device, a copied file or
// a git merge. They are detected, not prevented: every command that reads
// a Project holding them warns, a bare ambiguous ID fails, and doctor
// --fix renumbers all but the Item created first.

// duplicateGroups is the Item files of one Project, sorted by number, that
// share a number with another, one group per number, in number order.
func duplicateGroups(files []vault.ItemFile) [][]vault.ItemFile {
	var groups [][]vault.ItemFile
	for i := 0; i < len(files); {
		j := i + 1
		for j < len(files) && files[j].Number == files[i].Number {
			j++
		}
		if j-i > 1 {
			groups = append(groups, files[i:j])
		}
		i = j
	}
	return groups
}

// duplicateWarnings warn about each number that several Item files share
// in the Projects whose Items v has listed.
func duplicateWarnings(v *vault.Vault) ([]output.Problem, error) {
	var ws []output.Problem
	for _, key := range v.Scanned() {
		files, err := v.ItemFiles(key)
		if err != nil {
			return nil, err
		}
		for _, g := range duplicateGroups(files) {
			paths := make([]string, len(g))
			for i, f := range g {
				paths[i] = f.Path
			}
			id := g[0].ID()
			ws = append(ws, output.Warning("duplicate_number",
				fmt.Sprintf("%s is the number of %d Items: %s", id, len(g), strings.Join(paths, ", ")),
				map[string]any{"id": id, "paths": paths},
				"name one by its full filename or Vault-relative path; 'otman doctor --fix' renumbers all but the one created first"))
		}
	}
	return ws, nil
}

// renumbering is an Item file that doctor --fix gives a new number,
// because keeper, created before it, holds its number.
type renumbering struct {
	file   vault.ItemFile
	keeper vault.ItemFile
	// byFilename is set when keeper keeps the number because its
	// filename comes first, not because it was created first: they were
	// created at the same time, or keeper has no created time.
	byFilename bool
	// rewritable is set when the file's text can be rewritten for its new
	// number: its frontmatter reads, and its comments marker is there or
	// can be restored without guessing (ADR 0005).
	rewritable bool
	// restoresMarker is set when the renumbering comment restores the
	// missing comments marker on the way.
	restoresMarker bool
}

// planRenumbers is how doctor --fix settles the duplicate numbers among
// files, the Item files of one Project: in each group sharing a number,
// the Item with the earlier created time keeps it, and ties, or Items with
// no created time, go by filename, then path. An Item with a created time
// comes before one without. Every other Item is renumbered.
func planRenumbers(v *vault.Vault, files []vault.ItemFile) ([]renumbering, error) {
	var plans []renumbering
	for _, g := range duplicateGroups(files) {
		type entry struct {
			file                       vault.ItemFile
			created                    *time.Time
			rewritable, restoresMarker bool
		}
		entries := make([]entry, len(g))
		for i, f := range g {
			data, err := v.ReadItemFile(f)
			if err != nil {
				return nil, err
			}
			p := item.Parse(data)
			e := entry{file: f, restoresMarker: p.MarkerLines == 0 && item.RestorableMarker(data)}
			e.rewritable = p.HasFrontmatter && p.FrontmatterErr == nil && (p.MarkerLines == 1 || e.restoresMarker)
			if p.Created != nil {
				if t, err := time.Parse(time.RFC3339, *p.Created); err == nil {
					e.created = &t
				}
			}
			entries[i] = e
		}
		sort.SliceStable(entries, func(i, j int) bool {
			a, b := entries[i], entries[j]
			switch {
			case a.created != nil && b.created != nil && !a.created.Equal(*b.created):
				return a.created.Before(*b.created)
			case (a.created == nil) != (b.created == nil):
				return a.created != nil
			case a.file.Name() != b.file.Name():
				return a.file.Name() < b.file.Name()
			}
			return a.file.Path < b.file.Path
		})
		keeper := entries[0]
		for _, e := range entries[1:] {
			plans = append(plans, renumbering{
				file:           e.file,
				keeper:         keeper.file,
				byFilename:     keeper.created == nil || e.created != nil && e.created.Equal(*keeper.created),
				rewritable:     e.rewritable,
				restoresMarker: e.restoresMarker,
			})
		}
	}
	return plans, nil
}

// finding is the duplicate_number finding of r: auto when --fix can
// renumber it, none when its text cannot be rewritten.
func (r renumbering) finding() doctorFinding {
	why := "created earlier"
	if r.byFilename {
		why = "first by filename"
	}
	fix := "auto"
	if !r.rewritable {
		fix = "none"
	}
	return doctorFinding{Code: "duplicate_number", Path: r.file.Path,
		Message: r.file.ID() + " is also the number of " + r.keeper.Path + ", which keeps it as the Item " + why,
		Fix:     fix}
}

// renumber gives r.file the next number of its Project from the
// allocator: it is renamed to the new number in the same folder, its id
// reset and a comment by the actor, or by otman without one, recording
// the old number appended, and every link to it is rewritten, all through
// the journal. It returns the renumbered Item file.
func (a *app) renumber(v *vault.Vault, s resolved, r renumbering) (vault.ItemFile, error) {
	f := r.file
	data, err := v.ReadItemFile(f)
	if err != nil {
		return f, ioError(err)
	}
	n, err := v.Allocate(f.Key)
	if err != nil {
		return f, ioError(err)
	}
	to := vault.ItemFile{Key: f.Key, Number: n}
	to.Path = path.Join(path.Dir(f.Path), to.ID()+strings.TrimPrefix(f.Name(), f.ID()))
	author := "otman"
	if s.Actor.IsSet() && singleLine(s.Actor.Value) {
		author = s.Actor.Value
	}
	now := a.opts.Now()
	c := item.Comment{Author: author, Created: now.UTC().Format(time.RFC3339),
		Body: "Renumbered from " + f.ID() + " by otman doctor"}
	out, err := item.Renumber(data, to.Derived(), c, now)
	if err != nil {
		return f, writeError(f, err)
	}
	if _, _, err := v.RenameItem(f, to.Path, out, vault.RenumberOperation); err != nil {
		return f, renameError(f, err)
	}
	return to, nil
}
