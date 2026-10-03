package cli

import (
	"path"
	"strings"

	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/vault"
)

// refJSON is a reference to another Item: {id, path, resolved:true}, or
// the raw wikilink with null id and path and resolved:false.
type refJSON struct {
	ID       *string `json:"id"`
	Path     *string `json:"path"`
	Resolved bool    `json:"resolved"`
	Link     *string `json:"link,omitempty"`
}

func resolvedRef(f vault.ItemFile) refJSON {
	id, p := f.ID(), f.Path
	return refJSON{ID: &id, Path: &p, Resolved: true}
}

// itemLinks resolves relation wikilinks within one Project the way
// Obsidian does: by basename, ignoring case. It maps each lowercased file
// name, without ".md", to the Item files that carry it.
type itemLinks map[string][]vault.ItemFile

func newItemLinks(files []vault.ItemFile) itemLinks {
	links := itemLinks{}
	for _, f := range files {
		name := strings.ToLower(strings.TrimSuffix(f.Name(), ".md"))
		links[name] = append(links[name], f)
	}
	return links
}

// target is the one Item link names, and false when it names none or
// more than one.
func (l itemLinks) target(link string) (vault.ItemFile, bool) {
	name, ok := item.LinkName(link)
	if !ok {
		return vault.ItemFile{}, false
	}
	found := l[strings.ToLower(name)]
	if len(found) != 1 {
		return vault.ItemFile{}, false
	}
	return found[0], true
}

func (l itemLinks) ref(link string) refJSON {
	if f, ok := l.target(link); ok {
		return resolvedRef(f)
	}
	return refJSON{Link: &link}
}

// itemSummary is the Item every result carries.
type itemSummary struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Kind      *item.Kind `json:"kind"`
	Status    *string    `json:"status"`
	Labels    []string   `json:"labels"`
	Assignee  *string    `json:"assignee"`
	Parent    *refJSON   `json:"parent"`
	BlockedBy []refJSON  `json:"blocked_by"`
	Path      string     `json:"path"`
	Rev       string     `json:"rev"`
}

// newItemSummary reads the summary from a parsed Item file, resolving its
// relations with links. Identity comes from the filename; a missing title
// or Kind is derived from the filename and the folder.
func newItemSummary(f vault.ItemFile, p item.Parsed, data []byte, links itemLinks) itemSummary {
	s := itemSummary{
		ID:        f.ID(),
		Title:     itemTitle(f, p),
		Kind:      p.Kind,
		Status:    p.Status,
		Labels:    p.Labels,
		Assignee:  p.Assignee,
		BlockedBy: make([]refJSON, len(p.BlockedBy)),
		Path:      f.Path,
		Rev:       item.Rev(data),
	}
	if s.Labels == nil {
		s.Labels = []string{}
	}
	if s.Kind == nil {
		if k, ok := item.KindOfFolder(path.Base(path.Dir(f.Path))); ok {
			s.Kind = &k
		}
	}
	if p.Parent != nil {
		ref := links.ref(*p.Parent)
		s.Parent = &ref
	}
	for i, l := range p.BlockedBy {
		s.BlockedBy[i] = links.ref(l)
	}
	return s
}

func itemTitle(f vault.ItemFile, p item.Parsed) string {
	if p.Title != nil {
		return *p.Title
	}
	_, _, title, _ := item.ParseFilename(f.Name())
	return title
}

// commentJSON is one comment of a full Item.
type commentJSON struct {
	Author  string `json:"author"`
	Created string `json:"created"`
	Body    string `json:"body"`
}

// itemFull is the complete Item that view returns. Children are its direct
// children and Blocks the Items it blocks, both derived from the links of
// the other Items in its Project.
type itemFull struct {
	itemSummary
	Author  *string `json:"author"`
	Body    string  `json:"body"`
	Created *string `json:"created"`
	Updated *string `json:"updated"`
	// Comments is nil only when a display leaves the comments out.
	Comments *[]commentJSON `json:"comments,omitempty"`
	Children []refJSON      `json:"children"`
	Blocks   []refJSON      `json:"blocks"`
}

// related is how the other Items of a Project link to one Item.
type related struct {
	children, blocks []refJSON
}

// relatedTo scans files, a Project's Item files, for the ones whose parent
// or blocked_by resolves to f.
func relatedTo(v *vault.Vault, f vault.ItemFile, files []vault.ItemFile, links itemLinks) (related, error) {
	r := related{children: []refJSON{}, blocks: []refJSON{}}
	for _, g := range files {
		data, err := v.ReadItemFile(g)
		if err != nil {
			return related{}, err
		}
		p := item.Parse(data)
		if p.Parent != nil {
			if t, ok := links.target(*p.Parent); ok && t == f {
				r.children = append(r.children, resolvedRef(g))
			}
		}
		for _, l := range p.BlockedBy {
			if t, ok := links.target(l); ok && t == f {
				r.blocks = append(r.blocks, resolvedRef(g))
				break
			}
		}
	}
	return r, nil
}

func newItemFull(f vault.ItemFile, p item.Parsed, data []byte, links itemLinks, r related) itemFull {
	comments := make([]commentJSON, len(p.Comments))
	for i, c := range p.Comments {
		comments[i] = commentJSON(c)
	}
	return itemFull{
		itemSummary: newItemSummary(f, p, data, links),
		Author:      p.Author,
		Body:        p.Body,
		Created:     p.Created,
		Updated:     p.Updated,
		Comments:    &comments,
		Children:    r.children,
		Blocks:      r.blocks,
	}
}
