package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/talvor/otman/internal/fsutil"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/wikilink"
)

// JournalDir is the folder under StateDir that holds the journals of
// multi-file operations still in progress.
const JournalDir = "journal"

// Journal is the roll-forward record of one multi-file operation, such as
// a retitle: renaming an Item file and rewriting every link to it across
// the Vault (ADR 0002, ADR 0005). It is written atomically to
// .otman/journal/ before the first step and removed after the last, so an
// operation interrupted part-way is finished by the next otman command.
type Journal struct {
	Version   int    `json:"version"`
	Operation string `json:"operation"` // RetitleOperation, MoveOperation or RenumberOperation
	Item      string `json:"item"`      // the Item's ID
	From      string `json:"from"`      // the Item file's Vault-relative path before
	To        string `json:"to"`        // and after
	Steps     []Step `json:"steps"`
}

// journalVersion is the Version of the journals otman writes.
const journalVersion = 1

// The journaled operations.
const (
	// RetitleOperation renames an Item file for a new title, and moves it
	// too when its Kind changes as well.
	RetitleOperation = "retitle"
	// MoveOperation moves an Item file to another Kind's folder alone, or
	// a misplaced one into its own Project.
	MoveOperation = "move"
	// RenumberOperation gives an Item file whose number another Item
	// holds a new number.
	RenumberOperation = "renumber"
)

// Step is one file change of a journaled operation. Each step checks the
// file's rev before it acts, so replaying a journal skips the steps
// already applied and refuses content it does not expect.
type Step struct {
	Action string `json:"action"` // "rename" or "write"
	Path   string `json:"path"`   // Vault-relative
	// To is where a rename moves the file.
	To string `json:"to,omitempty"`
	// PreRev is the file's rev before the step.
	PreRev string `json:"pre_rev"`
	// PostRev and Content are a write's rev and bytes after the step.
	PostRev string `json:"post_rev,omitempty"`
	Content []byte `json:"content,omitempty"`
	// PreContent is a write's bytes before the step, which a resume that
	// finds the step applied needs to tell the sides the write changed.
	PreContent []byte `json:"pre_content,omitempty"`
}

// The step actions.
const (
	renameStep = "rename"
	writeStep  = "write"
)

// JournalConflictError stops a journal at a step whose file holds content
// the step does not expect. The journal stays pending: otman never
// guesses how to finish it.
type JournalConflictError struct {
	Journal   string // the journal's Vault-relative path
	Operation string
	Item      string
	Path      string // the Vault-relative path of the file in question
	Reason    string
}

func (e *JournalConflictError) Error() string { return "cannot rewrite " + e.Path + ": " + e.Pending() }

// Pending is why the journal stopped, without the file in question: "the
// pending <operation> of <item> in <journal> cannot continue: <reason>".
func (e *JournalConflictError) Pending() string {
	return "the pending " + e.Operation + " of " + e.Item + " in " + e.Journal + " cannot continue: " + e.Reason
}

// LinkRewriteError refuses a rename because the file at Path links to the
// Item but its links cannot be rewritten, such as when its frontmatter
// cannot be spliced. Nothing has been written.
type LinkRewriteError struct {
	Path string
	Err  error
}

func (e *LinkRewriteError) Error() string {
	return "cannot rewrite the links in " + e.Path + ": " + e.Err.Error()
}

func (e *LinkRewriteError) Unwrap() error { return e.Err }

// AmbiguousLinkError refuses a rename because the file at Path links to
// Note, another note, with Target, and the link would name the renamed
// Item too. Nothing has been written.
type AmbiguousLinkError struct {
	Path, Target, Note string
}

func (e *AmbiguousLinkError) Error() string {
	return "the link [[" + e.Target + "]] in " + e.Path + " to " + e.Note + " would become ambiguous"
}

// RenameItem replaces the bytes of Item file f with data and moves it to
// the Vault-relative path to, rewriting every wikilink to it across the
// Vault, in frontmatter relations and bodies, to name the new path. The
// links in data itself are rewritten too. The whole operation is planned
// before anything is written: a target that exists fails with a
// *TargetExistsError, a file whose links cannot be rewritten with a
// *LinkRewriteError and a link anywhere in a note, frontmatter included,
// to another note that the new path would make ambiguous with an
// *AmbiguousLinkError. It then runs through a journal. operation names it
// in the journal: RetitleOperation, MoveOperation or RenumberOperation.
// RenameItem returns the moved Item file and its final bytes.
func (v *Vault) RenameItem(f ItemFile, to string, data []byte, operation string) (ItemFile, []byte, error) {
	if err := v.CheckMove(f, to); err != nil {
		return f, nil, err
	}
	current, err := os.ReadFile(v.abs(f.Path))
	if err != nil {
		return f, nil, err
	}
	notes, err := v.notes()
	if err != nil {
		return f, nil, err
	}
	ix := wikilink.NewIndex(notes)
	moved := []string{to}
	for _, p := range notes {
		if p != f.Path {
			moved = append(moved, p)
		}
	}
	after := wikilink.NewIndex(moved)
	retarget := func(target string) (string, bool) {
		found := ix.Resolve(target)
		if len(found) != 1 || found[0] != f.Path {
			return "", false
		}
		return newTarget(target, to, after), true
	}
	ambiguous := func(p string, text []byte) error {
		for _, l := range wikilink.Scan(string(text)) {
			found := ix.Resolve(l.Target)
			if len(found) == 1 && found[0] != f.Path && len(after.Resolve(l.Target)) > 1 {
				return &AmbiguousLinkError{Path: p, Target: l.Target, Note: found[0]}
			}
		}
		return nil
	}
	if err := ambiguous(f.Path, data); err != nil {
		return f, nil, err
	}
	if data, err = item.RetargetLinks(data, retarget); err != nil {
		return f, nil, &LinkRewriteError{Path: f.Path, Err: err}
	}
	j := Journal{Version: journalVersion, Operation: operation, Item: f.ID(), From: f.Path, To: to,
		Steps: []Step{{Action: renameStep, Path: f.Path, To: to, PreRev: item.Rev(current)}}}
	if item.Rev(data) != item.Rev(current) {
		j.Steps = append(j.Steps, Step{Action: writeStep, Path: to,
			PreRev: item.Rev(current), PostRev: item.Rev(data), Content: data, PreContent: current})
	}
	for _, p := range notes {
		if p == f.Path {
			continue
		}
		before, err := os.ReadFile(v.abs(p))
		if err != nil {
			return f, nil, err
		}
		if err := ambiguous(p, before); err != nil {
			return f, nil, err
		}
		after, err := item.RetargetLinks(before, retarget)
		if err != nil {
			return f, nil, &LinkRewriteError{Path: p, Err: err}
		}
		if item.Rev(after) != item.Rev(before) {
			j.Steps = append(j.Steps, Step{Action: writeStep, Path: p,
				PreRev: item.Rev(before), PostRev: item.Rev(after), Content: after, PreContent: before})
		}
	}
	if err := v.run(j); err != nil {
		return f, nil, err
	}
	f.Path = to
	return f, data, nil
}

// newTarget is a link target, written as old, rewritten to name the note
// at the Vault-relative path to. It keeps the form of old: a bare name
// stays a name, a path keeps as many trailing segments and any leading
// "/", and a ".md" extension is kept as written. When that would be
// ambiguous in ix, the Vault's notes after the move, it names more of
// the path, as many trailing segments as it takes to resolve to to alone.
func newTarget(old, to string, ix *wikilink.Index) string {
	lead := ""
	if strings.HasPrefix(old, "/") {
		lead = "/"
	}
	bare, ext := strings.TrimPrefix(old, "/"), ""
	if len(bare) > 3 && strings.EqualFold(bare[len(bare)-3:], ".md") {
		bare, ext = bare[:len(bare)-3], bare[len(bare)-3:]
	}
	segments := strings.Split(strings.TrimSuffix(to, ".md"), "/")
	keep := min(strings.Count(bare, "/")+1, len(segments))
	for keep < len(segments) && len(ix.Resolve(strings.Join(segments[len(segments)-keep:], "/"))) > 1 {
		keep++
	}
	return lead + strings.Join(segments[len(segments)-keep:], "/") + ext
}

// notes lists the Markdown notes of the Vault, Vault-relative and
// slash-separated, leaving out hidden folders such as .otman and
// .obsidian, as Obsidian does.
func (v *Vault) notes() ([]string, error) {
	var out []string
	err := filepath.WalkDir(v.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == v.Root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && strings.EqualFold(path.Ext(d.Name()), ".md") {
			rel, _ := filepath.Rel(v.Root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out, err
}

// journalDir is the file-system path of .otman/journal.
func (v *Vault) journalDir() string { return filepath.Join(v.Root, StateDir, JournalDir) }

// run writes journal j, applies its steps in order and removes it.
func (v *Vault) run(j Journal) error {
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	dir := v.journalDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := fsutil.SyncDir(filepath.Dir(dir)); err != nil {
		return err
	}
	name := j.Operation + "-" + j.Item + ".json"
	if err := fsutil.WriteFile(filepath.Join(dir, name), append(b, '\n')); err != nil {
		return err
	}
	return v.finish(name, j)
}

// finish applies the steps of journal j, stored in the journal folder as
// name, and removes it, along with the folder once it is empty. It
// consults v.Fault, when set, around the steps.
func (v *Vault) finish(name string, j Journal) error {
	for i, s := range j.Steps {
		if v.Fault != nil {
			if err := v.Fault(i); err != nil {
				return err
			}
		}
		if err := v.applyStep(s); err != nil {
			var c *JournalConflictError
			if errors.As(err, &c) {
				c.Journal = path.Join(StateDir, JournalDir, name)
				c.Operation, c.Item = j.Operation, j.Item
			}
			return err
		}
	}
	if v.Fault != nil {
		if err := v.Fault(len(j.Steps)); err != nil {
			return err
		}
	}
	dir := v.journalDir()
	if err := os.Remove(filepath.Join(dir, name)); err != nil {
		return err
	}
	if err := fsutil.SyncDir(dir); err != nil {
		return err
	}
	// The folder is removed once no journal is pending, so .otman/ holds
	// it only while an operation is in progress.
	if err := os.Remove(dir); err == nil {
		return fsutil.SyncDir(filepath.Dir(dir))
	}
	return nil
}

// applyStep applies one step, or skips it when it was already applied.
// Content the step does not expect stops it with a *JournalConflictError.
func (v *Vault) applyStep(s Step) error {
	switch s.Action {
	case renameStep:
		return v.applyRename(s)
	case writeStep:
		cur, err := os.ReadFile(v.abs(s.Path))
		if errors.Is(err, os.ErrNotExist) {
			return conflict(s.Path, "it is missing")
		}
		if err != nil {
			return err
		}
		switch rev := item.Rev(cur); rev {
		case s.PostRev:
			// Written already, but a crash may have come before the
			// snapshot was recorded.
			return v.recordSnapshot(s.Path, s.Path, s.PreContent, s.Content)
		case s.PreRev:
			return v.writeFile(s.Path, s.Content)
		default:
			return changed(s.Path, rev, s.PreRev)
		}
	}
	return fmt.Errorf("unknown journal step %q", s.Action)
}

// applyRename applies a rename step, or skips it when it was already
// applied.
func (v *Vault) applyRename(s Step) error {
	from := s.Path
	if _, err := os.Lstat(v.abs(caseTemp(s.To))); err == nil {
		from = caseTemp(s.To) // a case-only rename stopped between its moves
	}
	src, srcErr := os.ReadFile(v.abs(from))
	_, dstErr := os.Lstat(v.abs(s.To))
	free := errors.Is(dstErr, os.ErrNotExist)
	if srcErr == nil && dstErr == nil {
		// On a case-insensitive file system a case-only rename finds one
		// file under both names; it is applied once the folder no longer
		// lists the old spelling.
		same, err := v.sameFile(from, s.To)
		if err != nil {
			return err
		}
		if same {
			pending, err := v.listed(from)
			if err != nil {
				return err
			}
			if !pending {
				return v.renamed(s)
			}
			free = true
		}
	}
	switch {
	case srcErr == nil && free:
		if rev := item.Rev(src); rev != s.PreRev {
			return changed(s.Path, rev, s.PreRev)
		}
		return v.renameFile(from, s.To, src)
	case errors.Is(srcErr, os.ErrNotExist) && dstErr == nil:
		return v.renamed(s)
	case srcErr == nil && dstErr == nil:
		return conflict(s.To, "it already exists, so "+s.Path+" cannot be renamed to it")
	case errors.Is(srcErr, os.ErrNotExist) && free:
		return conflict(s.Path, "it is missing, and so is "+s.To)
	case srcErr != nil:
		return srcErr
	default:
		return dstErr
	}
}

// renamed finishes rename step s found already applied: a crash may have
// come before the moved Item's snapshot was recorded, so it is recorded
// again, unless the file has been written since, when the write step
// after it records it.
func (v *Vault) renamed(s Step) error {
	data, err := os.ReadFile(v.abs(s.To))
	if err != nil || item.Rev(data) != s.PreRev {
		return err
	}
	return v.recordSnapshot(s.Path, s.To, data, data)
}

// conflict is the *JournalConflictError of a step stopped at the
// Vault-relative path p for reason; finish names the journal.
func conflict(p, reason string) error { return &JournalConflictError{Path: p, Reason: reason} }

// changed is the conflict of a step whose file at p has rev, not the
// want it expects.
func changed(p, rev, want string) error {
	return conflict(p, fmt.Sprintf(
		"its rev is %s, not the %s the operation expects; it has changed since the operation began", rev, want))
}

// caseTemp is the hidden name, beside to, that a case-only rename moves
// the file through when renaming it straight to to does not change its
// spelling.
func caseTemp(to string) string { return path.Join(path.Dir(to), "."+path.Base(to)+".rename") }

// renameFile renames the file at the Vault-relative path from, whose bytes
// are data, to the path to, creating its folder, and records the moved
// Item's snapshot.
func (v *Vault) renameFile(from, to string, data []byte) error {
	src, dst := v.abs(from), v.abs(to)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := fsutil.SyncDir(filepath.Dir(filepath.Dir(dst))); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	// Where both names reach one file, as a case-only rename finds it on
	// a case-insensitive file system, renaming may change nothing; moving
	// the file aside first changes its spelling.
	if stale, err := v.listed(from); err != nil {
		return err
	} else if stale {
		tmp := v.abs(caseTemp(to))
		if err := os.Rename(src, tmp); err != nil {
			return err
		}
		if err := os.Rename(tmp, dst); err != nil {
			return err
		}
		// A rename onto another hard link to the same file changes
		// nothing either, leaving the spare link to remove.
		if _, err := os.Lstat(tmp); err == nil {
			if err := os.Remove(tmp); err != nil {
				return err
			}
		}
	}
	if err := fsutil.SyncDir(filepath.Dir(dst)); err != nil {
		return err
	}
	if err := fsutil.SyncDir(filepath.Dir(src)); err != nil {
		return err
	}
	return v.recordSnapshot(from, to, data, data)
}

// resume finishes every pending journal, in name order, with a
// resumed_operation warning for each. It runs under the lock before any
// command does its own work. A journal that cannot be finished stops it
// with a *JournalConflictError and stays pending.
func (v *Vault) resume() ([]output.Problem, error) {
	entries, err := os.ReadDir(v.journalDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var warnings []output.Problem
	for _, e := range entries {
		// Anything else, such as a temporary file a crash left behind,
		// is not a journal.
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		rel := path.Join(StateDir, JournalDir, e.Name())
		b, err := os.ReadFile(filepath.Join(v.journalDir(), e.Name()))
		if err != nil {
			return nil, err
		}
		var j Journal
		if err := json.Unmarshal(b, &j); err != nil || j.Version != journalVersion {
			reason := fmt.Sprintf("it is not a version %d journal", journalVersion)
			if err != nil {
				reason = "it cannot be read: " + err.Error()
			}
			return nil, &JournalConflictError{Journal: rel, Operation: "operation", Item: "an unknown Item",
				Path: rel, Reason: reason}
		}
		if err := v.finish(e.Name(), j); err != nil {
			return nil, err
		}
		warnings = append(warnings, output.Warning("resumed_operation",
			"finished an interrupted "+j.Operation+" of "+j.Item+": "+j.From+" → "+j.To,
			map[string]any{"journal": rel, "operation": j.Operation, "item": j.Item, "from": j.From, "to": j.To},
			"nothing to do; otman completed the operation before running this command"))
	}
	return warnings, nil
}
