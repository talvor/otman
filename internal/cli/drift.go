package cli

import (
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

// Drift (ADR 0005) is read leniently: an Item that drifted still reads,
// and each disagreement is warned about. Writes heal only lossless Drift
// (see item.Derived) and refuse with unsafe_write rather than guess.

// itemProblems are the warnings a read of Item file f, parsed as p,
// carries: its Drift, then each relation link that does not resolve.
func itemProblems(f vault.ItemFile, p item.Parsed, links itemLinks) []output.Problem {
	return append(driftProblems(f, p), relationProblems(f, p, links)...)
}

// driftProblems warn about each way Item file f, parsed as p, drifted.
// Frontmatter that cannot be read warns only that.
func driftProblems(f vault.ItemFile, p item.Parsed) []output.Problem {
	if p.FrontmatterErr != nil {
		return []output.Problem{malformedFrontmatter(f, p.FrontmatterErr)}
	}
	d := f.Derived()
	var ws []output.Problem
	if w, ok := idMismatch(f, p); ok {
		ws = append(ws, w)
	}
	var title, kind *string
	if d.Title != "" {
		title = &d.Title
	}
	if d.Kind != nil {
		k := string(*d.Kind)
		kind = &k
	}
	for _, m := range []struct {
		key     string
		absent  bool
		derived *string // nil when the file says nothing
		from    string
	}{
		{"id", p.ID == nil, &d.ID, "filename"},
		{"title", p.Title == nil, title, "filename"},
		{"kind", p.Kind == nil, kind, "folder"},
	} {
		if !m.absent || hasBadValue(p, m.key) {
			continue
		}
		msg := f.ID() + " has no " + m.key
		hint := "add " + m.key + " to " + f.Path
		if m.derived != nil {
			msg += ", so it reads as " + quoteArg(*m.derived) + " from its " + m.from
			hint = "the next otman write to " + f.ID() + " adds " + m.key + " from its " + m.from
		}
		if !p.HasFrontmatter {
			hint = noFrontmatterHint(f)
		}
		ws = append(ws, output.Warning("missing_key", msg,
			map[string]any{"id": f.ID(), "path": f.Path, "key": m.key, "derived": m.derived}, hint))
	}
	if p.Title != nil && item.FilenameTitle(*p.Title) != d.Title {
		ws = append(ws, output.Warning("title_drift",
			f.ID()+" has title "+quoteArg(*p.Title)+", but its filename says "+quoteArg(d.Title),
			map[string]any{"id": f.ID(), "path": f.Path, "title": *p.Title, "filename_title": d.Title},
			"otman shows the frontmatter title; 'otman doctor --fix --prefer frontmatter|file' settles which is right"))
	}
	folder := path.Dir(f.Path)
	switch {
	case f.Misplaced():
		ws = append(ws, misplacedItem(f, p.Kind))
	case d.Kind == nil:
		details := map[string]any{"id": f.ID(), "path": f.Path, "folder": folder}
		hint := "move it into " + kindFolders(f.Key)
		if p.Kind != nil {
			details["expected"] = f.KindPath(*p.Kind)
			hint = "move it to " + f.KindPath(*p.Kind) + ", or run 'otman doctor --fix'"
		}
		ws = append(ws, output.Warning("unexpected_folder",
			f.ID()+" is in "+folder+", not in a Kind folder of Project "+f.Key, details, hint))
	case p.Kind != nil && *p.Kind != *d.Kind:
		ws = append(ws, output.Warning("kind_drift",
			f.ID()+" has kind "+string(*p.Kind)+", but it is in "+folder+", the folder for "+string(*d.Kind),
			map[string]any{"id": f.ID(), "path": f.Path, "kind": *p.Kind, "folder_kind": *d.Kind},
			"otman shows the frontmatter Kind; 'otman doctor --fix --prefer frontmatter|file' settles which is right"))
	}
	if !hasValidStatus(p) {
		ws = append(ws, invalidStatus(f, p, false))
	}
	for _, bad := range p.BadValues {
		if bad.Key != "id" {
			ws = append(ws, badValue(f, bad))
		}
	}
	ws = append(ws, invalidLabels(f, p)...)
	if w, ok := markerProblem(f, p); ok {
		ws = append(ws, w)
	}
	return ws
}

// misplacedItem warns that Item file f is filed under another Project's
// folder, so it is read under the Project its prefix names. kind is its
// Kind, nil when it has none, which names the folder it belongs in.
func misplacedItem(f vault.ItemFile, kind *item.Kind) output.Problem {
	details := map[string]any{"id": f.ID(), "path": f.Path, "project": f.Key, "folder_project": f.FolderKey()}
	hint := "move it into " + kindFolders(f.Key)
	if kind != nil {
		details["expected"] = f.KindPath(*kind)
		hint = "move it to " + f.KindPath(*kind) + ", or run 'otman doctor --fix'"
	}
	return output.Warning("misplaced_item",
		f.ID()+" is in "+path.Dir(f.Path)+", a folder of Project "+f.FolderKey()+", so it is read as an Item of Project "+f.Key,
		details, hint)
}

// strayItem is the finding for file f, filed under a Project's folder with
// the prefix of a Project the Vault does not have. It is no Project's
// Item, so nothing reads it and doctor only reports it.
func strayItem(f vault.ItemFile) output.Problem {
	return output.Warning("misplaced_item",
		f.ID()+" is in "+path.Dir(f.Path)+", a folder of Project "+f.FolderKey()+", but there is no Project "+f.Key,
		map[string]any{"id": f.ID(), "path": f.Path, "project": f.Key, "folder_project": f.FolderKey()},
		"rename it to an Item of Project "+f.FolderKey()+", or create Project "+f.Key+" and move it there")
}

// markerProblem warns when Item file f, parsed as p, has no comments
// marker line or more than one.
func markerProblem(f vault.ItemFile, p item.Parsed) (output.Problem, bool) {
	details := map[string]any{"id": f.ID(), "path": f.Path}
	msg := f.ID() + " has no " + item.CommentsMarker + " line, so its comments are read from the last " + item.CommentsHeading + " heading"
	switch {
	case p.MarkerLines > 1:
		return output.Warning("duplicate_comments_markers",
			f.ID()+" has "+strconv.Itoa(p.MarkerLines)+" "+item.CommentsMarker+" lines, so its comments are read from the first",
			details, "keep only the marker just before the comments' "+item.CommentsHeading+" heading; until then body edits and comments are refused"), true
	case p.MarkerLines == 1:
		return output.Problem{}, false
	case !p.HasFrontmatter:
		return output.Warning("missing_comments_marker", msg, details, noFrontmatterHint(f)), true
	}
	return output.Warning("missing_comments_marker", msg,
		details, "'otman comment "+f.ID()+"' restores the marker; until then body edits are refused"), true
}

// noFrontmatterHint is the hint of each warning about Item file f, which
// has no frontmatter: every write refuses until it has some.
func noFrontmatterHint(f vault.ItemFile) string {
	return "otman refuses every write to " + f.ID() + " until it has frontmatter: add id, title, kind and status between --- lines at the top of " + f.Path
}

// hasValidStatus reports whether p's status is open or closed.
func hasValidStatus(p item.Parsed) bool {
	return p.Status != nil && (*p.Status == item.Open || *p.Status == item.Closed)
}

// invalidStatus warns that Item file f, parsed as p, has no status or one
// that is neither open nor closed. unlisted says list or frontier left it
// out for that.
func invalidStatus(f vault.ItemFile, p item.Parsed, unlisted bool) output.Problem {
	msg := f.ID() + " has no status"
	if p.Status != nil {
		msg = f.ID() + " has status " + quoteArg(*p.Status)
	}
	msg += ", not open or closed"
	hint := "run 'otman reopen " + f.ID() + "' or 'otman close " + f.ID() + "' to set it"
	if !p.HasFrontmatter {
		hint = noFrontmatterHint(f)
	}
	if unlisted {
		msg += ", so it is not listed"
		hint = "'otman view " + f.ID() + "' shows it; " + hint
	}
	return output.Warning("invalid_status", msg,
		map[string]any{"id": f.ID(), "path": f.Path, "status": p.Status}, hint)
}

// invalidLabels warn about each Label of Item file f, parsed as p, that
// is not a valid Label even lowercased. Reads show it as written, and
// writes keep it.
func invalidLabels(f vault.ItemFile, p item.Parsed) []output.Problem {
	var ws []output.Problem
	seen := labelSet{}
	for _, l := range p.Labels {
		if _, ok := item.ParseLabel(l); ok || seen[strings.ToLower(l)] {
			continue
		}
		seen.add([]string{l})
		ws = append(ws, output.Warning("invalid_label",
			f.ID()+" has the label "+quoteArg(l)+", which is not a valid Label even lowercased",
			map[string]any{"id": f.ID(), "path": f.Path, "label": l, "pattern": item.LabelPattern, "max_length": item.LabelLimit},
			"otman keeps it; rename it in "+f.Path+" to "+labelRules))
	}
	return ws
}

// idMismatch warns when the frontmatter id of Item file f, parsed as p,
// is present but is not its filename prefix.
func idMismatch(f vault.ItemFile, p item.Parsed) (output.Problem, bool) {
	var written string
	switch {
	case p.ID != nil && *p.ID != f.ID():
		written = quoteArg(*p.ID)
	case hasBadValue(p, "id"):
		written = "that is a list or mapping"
	default:
		return output.Problem{}, false
	}
	return output.Warning("id_mismatch",
		f.ID()+" has frontmatter id "+written+"; its identity is its filename prefix "+f.ID(),
		map[string]any{"id": f.ID(), "path": f.Path, "frontmatter_id": p.ID},
		"the next otman write to "+f.ID()+" resets id to "+f.ID()), true
}

// kindFolders names the Kind folders of Project key.
func kindFolders(key string) string {
	names := make([]string, len(item.Kinds))
	for i, k := range item.Kinds {
		names[i] = path.Join(vault.ProjectsDir, key, k.Folder()) + "/"
	}
	return strings.Join(names, ", ")
}

// wellFormed is what the value of each owned key should be.
var wellFormed = map[string]string{
	"title": "text", "kind": `"issue", "prd" or "spec"`, "author": "a name", "assignee": "a name",
	"created": "a timestamp", "updated": "a timestamp", "labels": "a list of Labels",
}

// badValue warns that bad, an owned value of Item file f, has the wrong
// shape, so it reads as absent.
func badValue(f vault.ItemFile, bad item.BadValue) output.Problem {
	what := "a list or mapping"
	if bad.Value != nil {
		what = quoteArg(*bad.Value)
	}
	return output.Warning("malformed_value",
		"the "+bad.Key+" value of "+f.ID()+" is "+what+", not "+wellFormed[bad.Key]+", so it reads as absent",
		map[string]any{"id": f.ID(), "path": f.Path, "key": bad.Key, "value": bad.Value},
		"fix "+bad.Key+" in "+f.Path+"; otman keeps it as written until a command sets "+bad.Key)
}

// malformedFrontmatter warns that the Item file f was left out because
// its frontmatter cannot be read.
func malformedFrontmatter(f vault.ItemFile, err error) output.Problem {
	return output.Warning("malformed_frontmatter",
		"cannot read the frontmatter of "+f.Path+": "+err.Error(),
		map[string]any{"path": f.Path},
		"fix the YAML between the --- lines in "+f.Path)
}

// hasBadValue reports whether the value of key, parsed as p, has the
// wrong shape.
func hasBadValue(p item.Parsed, key string) bool {
	return slices.ContainsFunc(p.BadValues, func(b item.BadValue) bool { return b.Key == key })
}
