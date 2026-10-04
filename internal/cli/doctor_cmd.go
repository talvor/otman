package cli

import (
	"bytes"
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
		// Duplicate numbers are settled first, so that a misplaced Item
		// whose number is taken in its own Project can then move there.
		dup, err := a.settleDuplicates(v, s, all, only, f.fix, &res, &ws)
		if err != nil {
			return res, ws, err
		}
		all, only, vacating := dup.all, dup.only, dup.vacating
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
			pl := planDoctor(v, file, data, all, snap, f.prefer, vacating)
			if fd, ok := dup.findings[file.Path]; ok {
				res.Findings = append(res.Findings, fd)
			}
			if !f.fix {
				for _, fd := range pl.findings {
					res.Findings = append(res.Findings, fd.doctorFinding)
				}
				continue
			}
			moved, changed, err := a.applyDoctor(v, file, data, pl)
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
		if ref == "" {
			// A file under the Project's folder whose prefix names no
			// Project is no Item, so it is only reported.
			strays, err := v.StrayFiles(key)
			if err != nil {
				return res, ws, ioError(err)
			}
			for _, file := range strays {
				pr := strayItem(file)
				res.Findings = append(res.Findings, doctorFinding{Code: pr.Code, Path: file.Path, Message: pr.Message, Fix: "none"})
			}
		}
	}
	if ref != "" {
		// A run narrowed to one Item may leave duplicates it does not
		// report as findings, so it warns about them as other commands do.
		dups, err := duplicateWarnings(v)
		if err != nil {
			return res, ws, ioError(err)
		}
		ws = append(ws, dups...)
	}
	return res, ws, nil
}

// settledDuplicates is what settleDuplicates leaves for the rest of a
// doctor run over one Project.
type settledDuplicates struct {
	all  []vault.ItemFile // the Project's Item files after any renumbering
	only []vault.ItemFile // the Item a REF narrows the run to, renumbered or not; nil without a REF
	// findings are the duplicate_number findings left for the per-Item
	// pass to report, by path.
	findings map[string]doctorFinding
	// vacating holds the paths --fix would renumber away, so that a report
	// can tell a move whose target they hold will go ahead.
	vacating map[string]bool
}

// settleDuplicates settles the duplicate numbers among all, the Item files
// of one Project: it renumbers every Item planRenumbers picks when fix is
// set, and otherwise leaves them as findings. A REF, only, narrows it to
// the duplicates of that Item's number. Repairs go to res, warnings for
// repairs that fail to ws.
func (a *app) settleDuplicates(v *vault.Vault, s resolved, all, only []vault.ItemFile, fix bool, res *doctorResult, ws *[]output.Problem) (settledDuplicates, error) {
	out := settledDuplicates{all: all, only: only, findings: map[string]doctorFinding{}, vacating: map[string]bool{}}
	renumbers, err := planRenumbers(v, all)
	if err != nil {
		return out, ioError(err)
	}
	narrowed := only != nil
	var onlyPath string
	if narrowed {
		onlyPath = only[0].Path
	}
	renumbered := false
	for _, r := range renumbers {
		if narrowed && r.file.Path != onlyPath && r.keeper.Path != onlyPath {
			continue
		}
		if !fix || !r.rewritable {
			if narrowed && r.file.Path != onlyPath {
				// The per-Item pass sees only the narrowed Item.
				res.Findings = append(res.Findings, r.finding())
			} else {
				out.findings[r.file.Path] = r.finding()
			}
			if r.rewritable {
				out.vacating[r.file.Path] = true
			}
			continue
		}
		moved, err := a.renumber(v, s, r)
		if err != nil {
			// The Item may be part-way through its journal, so it is left
			// for the next run.
			*ws = append(*ws, repairBlocked(r.file, err))
			res.Findings = append(res.Findings, r.finding())
			if r.file.Path == onlyPath {
				out.only = []vault.ItemFile{}
			}
			continue
		}
		renumbered = true
		res.Fixed = append(res.Fixed, doctorFix{Code: "duplicate_number", Path: r.file.Path, Old: r.file.ID(), New: moved.ID()})
		if r.restoresMarker {
			res.Fixed = append(res.Fixed, doctorFix{Code: "missing_comments_marker", Path: r.file.Path,
				Old: "(no marker)", New: "restored before " + item.CommentsHeading})
		}
		if r.file.Path == onlyPath {
			out.only = []vault.ItemFile{moved}
		}
	}
	if renumbered {
		if out.all, err = v.ItemFiles(all[0].Key); err != nil {
			return out, ioError(err)
		}
	}
	return out, nil
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
	// settleTitle and settleKind are set when title or Kind Drift is
	// repaired, so that both its sides of the snapshot take what the
	// repair leaves, and the next hand edit to one side shows as that side.
	settleTitle, settleKind bool
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
// findings that need a choice. vacating holds the paths that --fix
// renumbers away before it gets to f, which a move may then take.
func planDoctor(v *vault.Vault, f vault.ItemFile, data []byte, all []vault.ItemFile, snap *vault.Snapshot, prefer string, vacating map[string]bool) doctorPlan {
	var pl doctorPlan
	p := item.Parse(data)
	d := f.Derived()
	links := newItemLinks(all)
	labels, lossless := item.StoredLabels(data)
	healed := item.RepairLabels(labels)
	// Nothing can be spliced without frontmatter that reads, so only the
	// findings are made.
	spliceable := p.HasFrontmatter && p.FrontmatterErr == nil

	// Title and Kind Drift is settled by the snapshot when only one side
	// changed since otman last wrote the Item (ADR 0005).
	var titleFM, titleFile, kindFM, folderMoved bool
	if snap != nil {
		titleFM = !item.Same(p.Title, snap.Title)
		titleFile = f.Name() != snap.Filename
		kindFM = !item.Same(p.Kind, snap.Kind)
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
		case "unexpected_folder", "misplaced_item":
			kind := p.Kind
			if kind == nil && pr.Code == "misplaced_item" {
				// A misplaced Item with no kind goes to the Kind of the
				// folder it was filed in, which a missing_key repair writes.
				kind = d.Kind
			}
			if kind != nil {
				moveKind = kind
				fd.set("auto", quoteArg(path.Dir(f.Path)), quoteArg(path.Dir(f.KindPath(*kind))))
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
				fd.set("auto", quoteArg(label), quoteArg(keptLabel(healed, slug)))
				fixLabels = true
			}
		case "dangling_link":
			link, _ := pr.Details["link"].(string)
			key, _ := pr.Details["key"].(string)
			if wl, ok := wikilink.Parse(link); ok {
				if target, ok := repointTarget(v, all, links, f, key, wl.Target); ok {
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
				fd.set("auto", strings.Join(spellings, ", "), quoteArg(keptLabel(healed, dup.label)))
				fixLabels = true
			}
			pl.findings = append(pl.findings, fd)
		}
	}

	if fixLabels {
		pl.repairs.Labels = &healed
	}
	if relink {
		pl.repairs.Relink = func(key, target string) (string, bool) { return repointTarget(v, all, links, f, key, target) }
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
		if to := path.Join(dir, name); !movable(v, f, to, vacating) {
			for _, fd := range moves {
				fd.Fix, fd.applied = "none", false
			}
		} else if to != f.Path {
			pl.to = to
		}
	}

	for _, fd := range pl.findings {
		switch fd.Code {
		case "title_drift":
			pl.keepTitle, pl.settleTitle = !fd.applied, fd.applied
		case "kind_drift":
			pl.keepKind, pl.settleKind = !fd.applied, fd.applied
		}
	}
	return pl
}

// movable reports whether Item file f can move to the Vault-relative path
// to: nothing else is there, or what is there is in vacating, renumbered
// away before f moves. When f is in vacating itself, it moves under a new
// number above every number on disk, so no file is in its way.
func movable(v *vault.Vault, f vault.ItemFile, to string, vacating map[string]bool) bool {
	if vacating[f.Path] {
		return true
	}
	err := v.CheckMove(f, to)
	var exists *vault.TargetExistsError
	return err == nil || errors.As(err, &exists) && vacating[exists.Target]
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
// and reports the file's Vault-relative path after them and whether the
// vault was written, even when a repair then fails. A repair that moves the
// file goes through a journal (see RenameItem), and a write records the
// Item's snapshot, which keepBaseline then keeps for any Drift left
// unrepaired and settles for any Drift repaired.
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
	moved, written := f, false
	switch {
	case pl.to != "":
		op := vault.MoveOperation
		if path.Base(pl.to) != f.Name() {
			op = vault.RetitleOperation
		}
		var rerr error
		if moved, _, rerr = v.RenameItem(f, pl.to, out, op); rerr != nil {
			err = renameError(f, rerr)
			moved, written = landedAt(v, f, pl.to)
		} else {
			written = true
		}
	case changed:
		if werr := v.WriteItemFile(f, out); werr != nil {
			err = writeError(f, werr)
			written = writtenBytes(v, f, out)
		} else {
			written = true
		}
	default:
		return f, false, nil
	}
	if written && (pl.keepTitle || pl.keepKind || err == nil && (pl.settleTitle || pl.settleKind)) {
		if kerr := keepBaseline(v, moved, out, prev, hadPrev, pl, err == nil); kerr != nil {
			err = ioError(kerr)
		}
	}
	return moved, written, err
}

// landedAt is where Item file f is after a rename to the Vault-relative
// path to failed: to, with true, when the rename moved it there before it
// stopped, and f, with false, otherwise.
func landedAt(v *vault.Vault, f vault.ItemFile, to string) (vault.ItemFile, bool) {
	at, ok, err := v.ItemAt(to)
	if err != nil || !ok {
		return f, false
	}
	if _, here, err := v.ItemAt(f.Path); err != nil || here {
		return f, false
	}
	return at, true
}

// writtenBytes reports whether Item file f holds the bytes out, which a
// write that failed after it landed leaves there.
func writtenBytes(v *vault.Vault, f vault.ItemFile, out []byte) bool {
	b, err := v.ReadItemFile(f)
	return err == nil && bytes.Equal(b, out)
}

// keepBaseline rewrites the snapshot of Item file f, just written with the
// bytes out, so that the filename and title of a title pl leaves
// unrepaired, and the folder of a Kind pl leaves unrepaired, keep what otman
// last wrote there. It has none to keep when it had no snapshot before, so
// the snapshot is removed. When every repair landed, settled, the title and
// filename of a title pl repairs, and the Kind and folder of a Kind pl
// repairs, take what f now holds.
func keepBaseline(v *vault.Vault, f vault.ItemFile, out []byte, prev vault.Snapshot, hadPrev bool, pl doctorPlan, settled bool) error {
	if !hadPrev {
		if pl.keepTitle || pl.keepKind {
			return v.SetSnapshot(f.Key, f.Number, nil)
		}
		// A write without an earlier snapshot records what it leaves.
		return nil
	}
	cur, _, err := v.Snapshot(f.Key, f.Number)
	if err != nil {
		return err
	}
	if pl.keepTitle {
		cur.Filename, cur.Title = prev.Filename, prev.Title
	}
	if pl.keepKind {
		cur.Folder = prev.Folder
	}
	if settled {
		p := item.Parse(out)
		if pl.settleTitle {
			cur.Filename, cur.Title = f.Name(), p.Title
		}
		if pl.settleKind {
			cur.Folder, cur.Kind = path.Dir(f.Path), p.Kind
		}
	}
	return v.SetSnapshot(f.Key, f.Number, &cur)
}

// repointTarget is the target that repoints the dangling relation link
// target of holder to: the full filename, without ".md", of the one Item of
// Project all whose <KEY>-n prefix target carries. It fails when target
// resolves, has no such prefix, or its prefix matches no Item or several,
// and when that Item cannot be holder's target for relation (see
// repointable).
func repointTarget(v *vault.Vault, all []vault.ItemFile, links itemLinks, holder vault.ItemFile, relation, target string) (string, bool) {
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
	if len(found) != 1 || !repointable(v, links, holder, relation, found[0]) {
		return "", false
	}
	return strings.TrimSuffix(found[0].Name(), ".md"), true
}

// repointable reports whether holder may take target for relation key
// without a state the relation commands refuse: a self-edge, or a parent or
// blocking cycle.
func repointable(v *vault.Vault, links itemLinks, holder vault.ItemFile, key string, target vault.ItemFile) bool {
	if target.Path == holder.Path {
		return false
	}
	if key == "parent" {
		return checkParentCycle(v, links, holder, target) == nil
	}
	return checkBlockingCycle(v, links, holder, target) == nil
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

// keptLabel is the spelling of label that the repaired labels keep: the
// first of them that matches it, ignoring case.
func keptLabel(repaired []string, label string) string {
	for _, l := range repaired {
		if strings.EqualFold(l, label) {
			return l
		}
	}
	return label
}

// idText is the frontmatter id of p as written, quoted, or a placeholder
// when it is not text.
func idText(p item.Parsed) string {
	if p.ID != nil {
		return quoteArg(*p.ID)
	}
	return "a list or mapping"
}
