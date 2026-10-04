package cli

import (
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

// Relations (parent and blocked_by) are forward edges stored on the
// child or the blocked Item as quoted full-filename wikilinks. Both graphs
// stay within one Project and are independent of each other: no
// self-edges, no missing targets and no cycles in either. Edges to closed
// Items are valid.

// relationLinks is how relation key of an Item reads: each wikilink it
// holds.
func relationLinks(p item.Parsed, key string) []string {
	if key == "parent" {
		if p.Parent == nil {
			return nil
		}
		return []string{*p.Parent}
	}
	return p.BlockedBy
}

// linkProblem is what is wrong with link, the value of relation key of
// holder, which matches candidates: a dangling_link when it matches no
// Item, an ambiguous_link when it matches several; ok is false when it
// matches exactly one.
func linkProblem(holder vault.ItemFile, key, link string, candidates []vault.ItemFile) (p output.Problem, ok bool) {
	details := map[string]any{"id": holder.ID(), "path": holder.Path, "key": key, "link": link}
	hint := "fix or remove the link in " + holder.Path
	switch len(candidates) {
	case 1:
		return output.Problem{}, false
	case 0:
		return output.Warning("dangling_link",
			holder.ID()+" "+key+" link "+link+" matches no Item in Project "+holder.Key,
			details, hint), true
	}
	paths := make([]string, len(candidates))
	for i, c := range candidates {
		paths[i] = c.Path
	}
	details["paths"] = paths
	return output.Warning("ambiguous_link",
		fmt.Sprintf("%s %s link %s matches %d Items: %s", holder.ID(), key, link, len(paths), strings.Join(paths, ", ")),
		details, hint+", or name one Item by its path, such as [["+strings.TrimSuffix(paths[0], ".md")+"]]"), true
}

// badRelationProblem warns about bad, a relation value of holder that is
// not a wikilink, which a read ignores.
func badRelationProblem(holder vault.ItemFile, bad item.BadRelation) output.Problem {
	p := badRelation(holder, bad)
	p.Message += ", so it is ignored"
	return p
}

// badRelation is what is wrong with bad, a relation value of holder that
// is not a wikilink.
func badRelation(holder vault.ItemFile, bad item.BadRelation) output.Problem {
	what := "a list or mapping"
	if bad.Value != nil {
		what = quoteArg(*bad.Value)
	}
	where, want := "the parent", "a wikilink"
	example := `parent: "[[` + holder.Key + `-1 Title]]"`
	if bad.Key == "blocked_by" {
		where, want = "the blocked_by", "a list of wikilinks"
		example = `blocked_by: ["[[` + holder.Key + `-1 Title]]"]`
		if bad.Entry {
			where, want = "a blocked_by entry", "a wikilink"
		}
	}
	return output.Warning("malformed_relation",
		where+" of "+holder.ID()+" is "+what+", not "+want,
		map[string]any{"id": holder.ID(), "path": holder.Path, "key": bad.Key, "value": bad.Value},
		"write "+bad.Key+" in "+holder.Path+" as quoted wikilinks, such as "+example)
}

// relationProblems warn about every relation value of holder, parsed as
// p, that does not resolve to exactly one Item. They never make a read or
// a scalar edit fail.
func relationProblems(holder vault.ItemFile, p item.Parsed, links itemLinks) []output.Problem {
	var ws []output.Problem
	for _, key := range []string{"parent", "blocked_by"} {
		for _, l := range relationLinks(p, key) {
			if w, bad := linkProblem(holder, key, l, links.candidates(l)); bad {
				ws = append(ws, w)
			}
		}
		for _, bad := range p.BadRelations {
			if bad.Key == key {
				ws = append(ws, badRelationProblem(holder, bad))
			}
		}
	}
	return ws
}

// graphError turns a relation problem a graph operation depends on into
// its failure, exit 4.
func graphError(p output.Problem) *Error {
	e := &Error{Exit: ExitConflict, Code: p.Code, Message: p.Message, Details: p.Details}
	if p.Hint != nil {
		e.Hint = *p.Hint + ", then retry"
	}
	return e
}

// summarize is the summary of Item file f, whose bytes are data, with a
// warning for each of its relation links that does not resolve.
func summarize(v *vault.Vault, f vault.ItemFile, data []byte) (itemSummary, []output.Problem, error) {
	files, err := v.ItemFiles(f.Key)
	if err != nil {
		return itemSummary{}, nil, ioError(err)
	}
	links := newItemLinks(files)
	p := item.Parse(data)
	return newItemSummary(f, p, data, links), relationProblems(f, p, links), nil
}

// resolveTarget finds the Item ref names as the target of a relation of
// an Item in Project key: a qualified ID, a bare number (in Project key,
// whatever Project is selected), a unique full filename or an exact
// Vault-relative path. role names the target in failures, such as
// "parent" or "--by". A target in another Project fails with
// cross_project and one that matches nothing with missing_target, both
// exit 4.
func resolveTarget(v *vault.Vault, key, ref, role string) (vault.ItemFile, error) {
	targetKey := key
	var match func([]vault.ItemFile) []vault.ItemFile
	idKey, idNumber, isID := item.ParseID(ref)
	switch {
	case isID:
		targetKey = idKey
		match = func(fs []vault.ItemFile) []vault.ItemFile { return matchNumber(fs, idNumber) }
	case numberRef.MatchString(ref):
		n, err := strconv.Atoi(ref)
		if err != nil {
			return vault.ItemFile{}, missingTarget(ref, key, role)
		}
		match = func(fs []vault.ItemFile) []vault.ItemFile { return matchNumber(fs, n) }
	default:
		k, _, _, ok := item.ParseFilename(path.Base(ref))
		if !ok {
			return vault.ItemFile{}, missingTarget(ref, key, role)
		}
		targetKey = k
		if strings.Contains(ref, "/") {
			match = func(fs []vault.ItemFile) []vault.ItemFile {
				return slices.DeleteFunc(slices.Clone(fs), func(f vault.ItemFile) bool { return f.Path != ref })
			}
		} else {
			match = func(fs []vault.ItemFile) []vault.ItemFile { return matchName(fs, ref) }
		}
	}
	if targetKey != key {
		return vault.ItemFile{}, &Error{Exit: ExitConflict, Code: "cross_project",
			Message: "the " + role + " " + ref + " is in Project " + targetKey + ", not " + key + "; relations stay within one Project",
			Details: map[string]any{"ref": ref, "role": role, "project": key, "target_project": targetKey},
			Hint:    "name an Item of Project " + key}
	}
	f, err := uniqueItem(v, ref, key, match)
	if e, ok := err.(*Error); ok && e.Code == "item_not_found" {
		return vault.ItemFile{}, missingTarget(ref, key, role)
	}
	return f, err
}

func missingTarget(ref, key, role string) error {
	return &Error{Exit: ExitConflict, Code: "missing_target",
		Message: "the " + role + " " + quoteArg(ref) + " matches no Item in Project " + key,
		Details: map[string]any{"ref": ref, "role": role, "project": key},
		Hint:    "name an existing Item of Project " + key + " by its ID, its number, its full filename or its Vault-relative path"}
}

func selfEdge(f vault.ItemFile, relation string) error {
	return &Error{Exit: ExitConflict, Code: "self_edge",
		Message: f.ID() + " cannot be its own " + relation,
		Details: map[string]any{"id": f.ID(), "path": f.Path, "relation": relation},
		Hint:    "name another Item"}
}

// unreadableTarget fails a graph operation that must read what, such as
// the relations of Item file f, because the frontmatter of f, err, cannot
// be read.
func unreadableTarget(f vault.ItemFile, what string, err error) *Error {
	return &Error{Exit: ExitConflict, Code: "malformed_frontmatter",
		Message: "cannot read " + what + ": " + err.Error(),
		Details: map[string]any{"id": f.ID(), "path": f.Path},
		Hint:    "fix the YAML between the --- lines in " + f.Path + ", then retry"}
}

// followLinks reads the relation key of Item file f and returns the Items
// its links name. Every link must resolve to exactly one Item, and f must
// be readable, or the graph operation that follows them fails.
func followLinks(v *vault.Vault, links itemLinks, f vault.ItemFile, key string) ([]vault.ItemFile, error) {
	data, err := v.ReadItemFile(f)
	if err != nil {
		return nil, ioError(err)
	}
	p := item.Parse(data)
	if p.FrontmatterErr != nil {
		return nil, unreadableTarget(f, "the relations of "+f.ID(), p.FrontmatterErr)
	}
	return resolveLinks(links, f, p, key)
}

// resolveLinks returns the Items named by the links of relation key of
// Item file f, parsed as p. Every link must resolve to exactly one Item,
// or the graph operation that follows them fails.
func resolveLinks(links itemLinks, f vault.ItemFile, p item.Parsed, key string) ([]vault.ItemFile, error) {
	for _, bad := range p.BadRelations {
		if bad.Key == key {
			return nil, graphError(badRelation(f, bad))
		}
	}
	var out []vault.ItemFile
	for _, l := range relationLinks(p, key) {
		found := links.candidates(l)
		if prob, bad := linkProblem(f, key, l, found); bad {
			return nil, graphError(prob)
		}
		out = append(out, found[0])
	}
	return out, nil
}

// checkParentCycle fails with parent_cycle when making parent the parent
// of child would close a cycle: when child is already an ancestor of
// parent. Every parent link on the way up must resolve.
func checkParentCycle(v *vault.Vault, links itemLinks, child, parent vault.ItemFile) error {
	chain := []string{child.ID(), parent.ID()}
	seen := map[string]bool{}
	for cur := parent; ; {
		if seen[cur.Path] {
			return nil // a cycle child is not in, made by hand
		}
		seen[cur.Path] = true
		next, err := followLinks(v, links, cur, "parent")
		if err != nil || len(next) == 0 {
			return err
		}
		cur = next[0]
		chain = append(chain, cur.ID())
		if cur.Path == child.Path {
			return &Error{Exit: ExitConflict, Code: "parent_cycle",
				Message: "setting the parent of " + child.ID() + " to " + parent.ID() +
					" would make a cycle: " + strings.Join(chain, " → ") + ", each the child of the next",
				Details: map[string]any{"id": child.ID(), "parent": parent.ID(), "cycle": chain},
				Hint:    child.ID() + " is an ancestor of " + parent.ID() + "; pick a parent outside its descendants"}
		}
	}
}

// checkBlockingCycle fails with blocking_cycle when making blocker block
// blocked would close a cycle: when blocker is already blocked, directly
// or through other Items, by blocked. Every blocked_by link reached must resolve.
func checkBlockingCycle(v *vault.Vault, links itemLinks, blocked, blocker vault.ItemFile) error {
	seen := map[string]bool{}
	var visit func(cur vault.ItemFile, chain []string) error
	visit = func(cur vault.ItemFile, chain []string) error {
		if cur.Path == blocked.Path {
			return &Error{Exit: ExitConflict, Code: "blocking_cycle",
				Message: "blocking " + blocked.ID() + " by " + blocker.ID() + " would make a cycle: " +
					strings.Join(chain, " ← ") + ", each blocked by the next",
				Details: map[string]any{"id": blocked.ID(), "blocker": blocker.ID(), "cycle": chain},
				Hint:    blocker.ID() + " already waits on " + blocked.ID() + "; remove that dependency first"}
		}
		if seen[cur.Path] {
			return nil
		}
		seen[cur.Path] = true
		next, err := followLinks(v, links, cur, "blocked_by")
		if err != nil {
			return err
		}
		for _, n := range next {
			if err := visit(n, append(slices.Clip(chain), n.ID())); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(blocker, []string{blocked.ID(), blocker.ID()})
}

// matchingLinks are the entries, the links of relation key held by
// holder, that resolve to target. An entry that is ambiguous between
// target and other Items fails, since otman cannot tell whether it is the
// edge asked about.
func matchingLinks(holder vault.ItemFile, key string, entries []string, links itemLinks, target vault.ItemFile) ([]string, error) {
	var out []string
	for _, l := range entries {
		found := links.candidates(l)
		if !slices.Contains(found, target) {
			continue
		}
		if prob, bad := linkProblem(holder, key, l, found); bad {
			return nil, graphError(prob)
		}
		out = append(out, l)
	}
	return out, nil
}
