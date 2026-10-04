package cli

import (
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/config"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
	"github.com/talvor/otman/internal/wikilink"
)

// doctor reports the Drift of every Item in scope (ADR 0005) and, with
// --fix, repairs what can be repaired deterministically. A finding is
// "auto" when --fix repairs it, "choice" when it needs --prefer to, and
// "none" when otman leaves it to a person. Without --fix nothing changes,
// and the run exits 4 while any finding remains.

type doctorFlags struct {
	fix         bool
	prefer      string
	allProjects bool
}

// doctorFinding is one piece of Drift: its code, the Item's file, what is
// wrong and whether and how --fix can repair it.
type doctorFinding struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
}

// doctorFix is a finding --fix repaired: old → new.
type doctorFix struct {
	Code string `json:"code"`
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

type doctorResult struct {
	Findings []doctorFinding `json:"findings"`
	Fixed    []doctorFix     `json:"fixed"`
}

func (r doctorResult) RenderHuman(w io.Writer) error {
	for _, x := range r.Fixed {
		if _, err := fmt.Fprintf(w, "fixed %s %s: %s → %s\n", x.Code, x.Path, x.Old, x.New); err != nil {
			return err
		}
	}
	if len(r.Findings) == 0 {
		_, err := fmt.Fprintln(w, "No findings remain")
		return err
	}
	for _, f := range r.Findings {
		if _, err := fmt.Fprintf(w, "%s %s: %s (fix: %s)\n", f.Code, f.Path, f.Message, f.Fix); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) newDoctorCmd() *cobra.Command {
	var f doctorFlags
	cmd := &cobra.Command{
		Use:   "doctor [REF] [--fix] [--prefer frontmatter|file] [--all-projects]",
		Short: "Report the Drift of Items, and with --fix repair what can be repaired",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := ""
			if len(args) == 1 {
				ref = args[0]
			}
			return a.doctor(cmd, ref, f)
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&f.fix, "fix", false, "repair the findings that need no choice, and those --prefer settles")
	fl.StringVar(&f.prefer, "prefer", "", "settle every finding that needs a choice in favour of frontmatter or file")
	fl.BoolVar(&f.allProjects, "all-projects", false, "check every Project, not only the selected one")
	return cmd
}

func (a *app) doctor(cmd *cobra.Command, ref string, f doctorFlags) error {
	if cmd.Flags().Changed("prefer") && f.prefer != "frontmatter" && f.prefer != "file" {
		return invalid("invalid_prefer", "unknown --prefer "+quoteArg(f.prefer),
			map[string]any{"flag": "--prefer", "value": f.prefer, "allowed": []string{"frontmatter", "file"}},
			"use --prefer frontmatter or file")
	}
	s, err := a.settings()
	if err != nil {
		return err
	}
	if ref != "" && f.allProjects {
		return conflictingFlags("--all-projects", "a REF", "pass either a REF or --all-projects, not both")
	}
	// Only an explicit --project conflicts; a default Project does not.
	if f.allProjects && s.Project.Source == config.FromFlag {
		return conflictingFlags("--all-projects", "--project", "pass either --all-projects or --project, not both")
	}
	var sel config.Value
	if ref == "" && !f.allProjects {
		if sel, err = selectedProject(s, "--all-projects"); err != nil {
			return err
		}
	}
	v, warnings, err := a.openVault(s)
	if err != nil {
		// A journal that cannot be finished stops every command before it
		// runs, so doctor reports it as the one finding it can make.
		var e *Error
		if errors.As(err, &e) {
			if journal, ok := e.Details["journal"].(string); ok {
				return a.reportDoctor(doctorResult{
					Findings: []doctorFinding{{Code: "unfinished_journal", Path: journal, Message: e.Message, Fix: "none"}},
					Fixed:    []doctorFix{},
				}, nil)
			}
		}
		return err
	}
	res, ws, err := a.runDoctor(v, s, ref, sel, f)
	if cerr := v.Close(); err == nil && cerr != nil {
		err = ioError(cerr)
	}
	if err != nil {
		return err
	}
	return a.reportDoctor(res, append(warnings, ws...))
}

// reportDoctor writes res. Findings that remain then exit 4, after the
// report, so the status says what the report already showed.
func (a *app) reportDoctor(res doctorResult, warnings []output.Problem) error {
	if err := a.emit(res, warnings); err != nil {
		return err
	}
	if len(res.Findings) == 0 {
		return nil
	}
	return &Error{Exit: ExitConflict, Code: "findings", Silent: true,
		Message: fmt.Sprintf("%d findings remain", len(res.Findings))}
}

// runDoctor checks the Items in scope and, when f.fix is set, repairs them.
// Each Project's Items are read again after a file moves, since the moved
// file's links are rewritten and the Project's names change.
func (a *app) runDoctor(v *vault.Vault, s resolved, ref string, sel config.Value, f doctorFlags) (doctorResult, []output.Problem, error) {
	res := doctorResult{Findings: []doctorFinding{}, Fixed: []doctorFix{}}
	var keys []string
	var only []vault.ItemFile
	var ws []output.Problem
	if ref != "" {
		file, err := resolveRef(s, v, ref)
		if err != nil {
			return res, nil, err
		}
		keys, only = []string{file.Key}, []vault.ItemFile{file}
	} else {
		var err error
		if keys, ws, err = listScope(v, sel); err != nil {
			return res, nil, err
		}
	}
	for _, key := range keys {
		all, err := v.ItemFiles(key)
		if err != nil {
			return res, ws, ioError(err)
		}
		targets := all
		if only != nil {
			targets = only
		}
		for _, file := range targets {
			data, err := v.ReadItemFile(file)
			if err != nil {
				return res, ws, ioError(err)
			}
			var snap *vault.Snapshot
			if got, ok, err := v.Snapshot(file.Key, file.Number); err != nil {
				return res, ws, ioError(err)
			} else if ok {
				snap = &got
			}
			pl := planDoctor(v, file, data, all, snap, f.prefer)
			if !f.fix {
				for _, fd := range pl.findings {
					res.Findings = append(res.Findings, fd.doctorFinding)
				}
				continue
			}
			moved, changed, err := a.applyDoctor(v, file, data, pl)
			if err != nil && changed {
				return res, ws, err
			}
			if err != nil {
				ws = append(ws, repairBlocked(file, err))
			}
			for _, fd := range pl.findings {
				if changed && fd.applied {
					res.Fixed = append(res.Fixed, doctorFix{Code: fd.Code, Path: file.Path, Old: fd.old, New: fd.new})
				} else {
					res.Findings = append(res.Findings, fd.doctorFinding)
				}
			}
			if moved.Path != file.Path || err != nil {
				if all, err = v.ItemFiles(key); err != nil {
					return res, ws, ioError(err)
				}
			}
		}
	}
	return res, ws, nil
}

// repairBlocked is the warning for a repair the vault or the Item's text
// refused. The Item keeps its findings, and the run goes on.
func repairBlocked(f vault.ItemFile, err error) output.Problem {
	return output.Warning("repair_blocked", fmt.Sprintf("cannot repair %s: %v", f.Path, err),
		map[string]any{"path": f.Path}, "")
}

// doctorPlan is what doctor finds in one Item file and how it would repair
// it.
type doctorPlan struct {
	findings []*planned
	repairs  item.Repairs
	// to is the Vault-relative path the file moves to, or "" to stay.
	to string
	// keepTitle and keepKind are set when title or Kind Drift is left
	// unrepaired, so that its side of the snapshot keeps what otman last
	// wrote, and a later run can still tell which side changed.
	keepTitle, keepKind bool
}

// planned is a finding and the change --fix makes to repair it.
type planned struct {
	doctorFinding
	applied  bool
	old, new string
}

// set records the change old → new that repairs the finding, as fix:
// "auto", or "choice" when --prefer has settled it. Callers only set a
// finding they can repair, so it is applied unless fix is "none".
func (fd *planned) set(fix, old, new string) {
	fd.Fix, fd.old, fd.new = fix, old, new
	fd.applied = fix != "none"
}

// planDoctor finds the Drift of Item file f, whose bytes are data, among
// the Items of its Project, all, and plans how --fix repairs it. snap is
// the snapshot of f, nil when the db has none, and prefer settles the
// findings that need a choice.
func planDoctor(v *vault.Vault, f vault.ItemFile, data []byte, all []vault.ItemFile, snap *vault.Snapshot, prefer string) doctorPlan {
	var pl doctorPlan
	p := item.Parse(data)
	d := f.Derived()
	links := newItemLinks(all)
	labels, lossless := item.StoredLabels(data)
	// Nothing can be spliced without frontmatter that reads, so only the
	// findings are made.
	spliceable := p.HasFrontmatter && p.FrontmatterErr == nil

	// Title and Kind Drift is settled by the snapshot when only one side
	// changed since otman last wrote the Item (ADR 0005).
	var titleFM, titleFile, kindFM, folderMoved bool
	if snap != nil {
		titleFM = !sameText(p.Title, snap.Title)
		titleFile = f.Name() != snap.Filename
		kindFM = !sameKind(p.Kind, snap.Kind)
		folderMoved = path.Dir(f.Path) != snap.Folder
	}
	titleWinner, titleFix := sideOf(snap != nil, titleFM, titleFile, prefer)
	kindWinner, kindFix := sideOf(snap != nil, kindFM, folderMoved, prefer)

	var moves []*planned // the findings that move or rename the file
	var moveKind *item.Kind
	var renameTitle, relink, fixLabels bool
	for _, pr := range itemProblems(f, p, links) {
		fd := &planned{doctorFinding: doctorFinding{Code: pr.Code, Path: f.Path, Message: pr.Message, Fix: "none"}}
		pl.findings = append(pl.findings, fd)
		if !spliceable {
			continue
		}
		switch pr.Code {
		case "id_mismatch":
			fd.set("auto", idText(p), quoteArg(d.ID))
			pl.repairs.ID = true
		case "missing_key":
			key, _ := pr.Details["key"].(string)
			switch {
			case key == "id":
				fd.set("auto", "(missing)", quoteArg(d.ID))
				pl.repairs.ID = true
			case key == "title" && d.Title != "":
				title := d.Title
				fd.set("auto", "(missing)", quoteArg(title))
				pl.repairs.Title = &title
			case key == "kind" && d.Kind != nil:
				kind := *d.Kind
				fd.set("auto", "(missing)", quoteArg(string(kind)))
				pl.repairs.Kind = &kind
			}
		case "title_drift":
			fd.Fix = titleFix
			switch titleWinner {
			case "frontmatter":
				renameTitle = true
				fd.set(titleFix, quoteArg(f.Name()), quoteArg(item.Filename(f.Key, f.Number, *p.Title)))
				moves = append(moves, fd)
			case "file":
				if d.Title == "" {
					// A filename without a title cannot give the Item one.
					fd.Fix = "none"
					break
				}
				title := d.Title
				fd.set(titleFix, quoteArg(*p.Title), quoteArg(title))
				pl.repairs.Title = &title
			}
		case "kind_drift":
			fd.Fix = kindFix
			switch kindWinner {
			case "frontmatter":
				kind := *p.Kind
				moveKind = &kind
				fd.set(kindFix, quoteArg(path.Dir(f.Path)), quoteArg(path.Dir(f.KindPath(kind))))
				moves = append(moves, fd)
			case "file":
				kind := *d.Kind
				fd.set(kindFix, quoteArg(string(*p.Kind)), quoteArg(string(kind)))
				pl.repairs.Kind = &kind
			}
		case "unexpected_folder":
			if p.Kind != nil {
				kind := *p.Kind
				moveKind = &kind
				fd.set("auto", quoteArg(path.Dir(f.Path)), quoteArg(path.Dir(f.KindPath(kind))))
				moves = append(moves, fd)
			}
		case "missing_comments_marker":
			if item.RestorableMarker(data) {
				fd.set("auto", "(no marker)", "restored before "+item.CommentsHeading)
				pl.repairs.Marker = true
			}
		case "invalid_label":
			label, _ := pr.Details["label"].(string)
			if slug, ok := item.SlugLabel(label); ok && lossless {
				fd.set("auto", quoteArg(label), quoteArg(slug))
				fixLabels = true
			}
		case "dangling_link":
			link, _ := pr.Details["link"].(string)
			if wl, ok := wikilink.Parse(link); ok {
				if target, ok := repointTarget(v, all, links, f, wl.Target); ok {
					fd.set("auto", quoteArg(link), quoteArg("[["+target+"]]"))
					relink = true
				}
			}
		}
	}
	if spliceable {
		for _, dup := range duplicateLabels(labels) {
			fd := &planned{doctorFinding: doctorFinding{Code: "duplicate_label", Path: f.Path,
				Message: fmt.Sprintf("%s has the label %s %d times, ignoring case", f.ID(), quoteArg(dup.label), len(dup.spellings)),
				Fix:     "none"}}
			if lossless {
				spellings := make([]string, len(dup.spellings))
				for i, s := range dup.spellings {
					spellings[i] = quoteArg(s)
				}
				fd.set("auto", strings.Join(spellings, ", "), quoteArg(dup.label))
				fixLabels = true
			}
			pl.findings = append(pl.findings, fd)
		}
	}

	if fixLabels {
		healed := item.RepairLabels(labels)
		pl.repairs.Labels = &healed
	}
	if relink {
		pl.repairs.Relink = func(target string) (string, bool) { return repointTarget(v, all, links, f, target) }
	}

	// A rename or move goes to one path: the Kind folder the file belongs
	// in, under its new name. Where that path is taken, its findings stay
	// as they are, and nothing moves.
	if moveKind != nil || renameTitle {
		dir := path.Dir(f.Path)
		if moveKind != nil {
			dir = path.Dir(f.KindPath(*moveKind))
		}
		name := f.Name()
		if renameTitle {
			name = item.Filename(f.Key, f.Number, *p.Title)
		}
		if to := path.Join(dir, name); v.CheckMove(f, to) != nil {
			for _, fd := range moves {
				fd.Fix, fd.applied = "none", false
			}
		} else if to != f.Path {
			pl.to = to
		}
	}

	for _, fd := range pl.findings {
		if fd.applied {
			continue
		}
		switch fd.Code {
		case "title_drift":
			pl.keepTitle = true
		case "kind_drift":
			pl.keepKind = true
		}
	}
	return pl
}

// sideOf says which side of a disagreement wins and how it was settled. A
// change to one side since the snapshot settles it ("auto"). Otherwise it
// is a choice, which prefer settles, or which stays open with "" when
// prefer is not set. Without a snapshot, nothing else can settle it.
func sideOf(hasSnap, fmChanged, fileChanged bool, prefer string) (winner, fix string) {
	switch {
	case hasSnap && fmChanged && !fileChanged:
		return "frontmatter", "auto"
	case hasSnap && fileChanged && !fmChanged:
		return "file", "auto"
	}
	return prefer, "choice"
}

// applyDoctor makes the repairs of pl to Item file f, whose bytes are data,
// and reports the file's Vault-relative path after them and whether
// anything changed. A repair that moves the file goes through a journal (see
// RenameItem), and a write records the Item's snapshot, which keepBaseline
// then keeps for any Drift left unrepaired.
func (a *app) applyDoctor(v *vault.Vault, f vault.ItemFile, data []byte, pl doctorPlan) (vault.ItemFile, bool, error) {
	if pl.to == "" && !pl.repairs.Any() {
		// Nothing to write, so the file is not spliced at all: a file that
		// cannot be spliced, such as flow-style frontmatter, is left as it is.
		return f, false, nil
	}
	prev, hadPrev, err := v.Snapshot(f.Key, f.Number)
	if err != nil {
		return f, false, ioError(err)
	}
	out, changed, err := item.Repair(data, f.Derived(), pl.repairs, a.opts.Now())
	if err != nil {
		return f, false, writeError(f, err)
	}
	moved := f
	switch {
	case pl.to != "":
		op := vault.MoveOperation
		if path.Base(pl.to) != f.Name() {
			op = vault.RetitleOperation
		}
		if moved, _, err = v.RenameItem(f, pl.to, out, op); err != nil {
			return f, false, renameError(f, err)
		}
	case changed:
		if err := v.WriteItemFile(f, out); err != nil {
			return f, false, writeError(f, err)
		}
	default:
		return f, false, nil
	}
	if pl.keepTitle || pl.keepKind {
		if err := keepBaseline(v, f, prev, hadPrev, pl); err != nil {
			return moved, true, ioError(err)
		}
	}
	return moved, true, nil
}

// keepBaseline rewrites the snapshot of Item file f, just written, so that
// each side pl leaves unrepaired keeps what otman last wrote there. It has
// none to keep when it had no snapshot before, so the snapshot is removed.
func keepBaseline(v *vault.Vault, f vault.ItemFile, prev vault.Snapshot, hadPrev bool, pl doctorPlan) error {
	if !hadPrev {
		return v.SetSnapshot(f.Key, f.Number, nil)
	}
	cur, _, err := v.Snapshot(f.Key, f.Number)
	if err != nil {
		return err
	}
	if pl.keepTitle {
		cur.Filename, cur.Title = prev.Filename, prev.Title
	}
	if pl.keepKind {
		cur.Kind, cur.Folder = prev.Kind, prev.Folder
	}
	return v.SetSnapshot(f.Key, f.Number, &cur)
}

// repointTarget is the target that repoints the dangling relation link
// target of holder to: the full filename, without ".md", of the one Item of
// Project all whose <KEY>-n prefix target carries. It fails when target
// resolves, has no such prefix, or its prefix matches no Item or several,
// and when that Item cannot be holder's parent (see unusableParent).
func repointTarget(v *vault.Vault, all []vault.ItemFile, links itemLinks, holder vault.ItemFile, target string) (string, bool) {
	if len(links.index.Resolve(target)) != 0 {
		return "", false
	}
	prefix, _, _ := strings.Cut(path.Base(target), " ")
	key, n, ok := item.ParseID(strings.TrimSuffix(prefix, ".md"))
	if !ok {
		return "", false
	}
	var found []vault.ItemFile
	for _, f := range all {
		if f.Key == key && f.Number == n {
			found = append(found, f)
		}
	}
	if len(found) != 1 || unusableParent(v, links, holder, found[0]) {
		return "", false
	}
	return strings.TrimSuffix(found[0].Name(), ".md"), true
}

// unusableParent reports whether holder cannot take f as its parent: f is
// holder itself, holder is among f's ancestors, or an ancestor link does not
// resolve. Each would make a self-edge, a parent cycle or a chain the
// relation commands refuse to follow.
func unusableParent(v *vault.Vault, links itemLinks, holder, f vault.ItemFile) bool {
	seen := map[string]bool{}
	for cur := f; !seen[cur.Path]; {
		if cur.Path == holder.Path {
			return true
		}
		seen[cur.Path] = true
		next, err := followLinks(v, links, cur, "parent")
		if err != nil {
			return true
		}
		if len(next) == 0 {
			return false
		}
		cur = next[0]
	}
	return false
}

// duplicateLabel is a Label an Item carries more than once, ignoring case.
type duplicateLabel struct {
	label     string   // lowercased
	spellings []string // as stored, in order
}

// duplicateLabels are the Labels that labels holds more than once, ignoring
// case, in the order they first appear. Labels that are not valid are left
// to the invalid_label finding.
func duplicateLabels(labels []string) []duplicateLabel {
	var out []duplicateLabel
	index := map[string]int{}
	for _, l := range labels {
		if _, ok := item.ParseLabel(l); !ok {
			continue
		}
		key := strings.ToLower(l)
		i, seen := index[key]
		if !seen {
			out = append(out, duplicateLabel{label: key})
			i = len(out) - 1
			index[key] = i
		}
		out[i].spellings = append(out[i].spellings, l)
	}
	var dups []duplicateLabel
	for _, d := range out {
		if len(d.spellings) > 1 {
			dups = append(dups, d)
		}
	}
	return dups
}

// sameText reports whether a and b are both nil or both hold the same text.
func sameText(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// sameKind reports whether a and b are both nil or both hold the same Kind.
func sameKind(a, b *item.Kind) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// idText is the frontmatter id of p as written, quoted, or a placeholder
// when it is not text.
func idText(p item.Parsed) string {
	if p.ID != nil {
		return quoteArg(*p.ID)
	}
	return "a list or mapping"
}
