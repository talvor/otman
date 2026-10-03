package cli

import (
	"path"

	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/vault"
)

// refJSON is a reference to another Item: {id, path, resolved:true}, or
// the raw wikilink with null id and path and resolved:false.
type refJSON struct {
	ID       *string `json:"id"`
	Path     *string `json:"path"`
	Resolved bool    `json:"resolved"`
}

// itemSummary is the Item every result carries.
type itemSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Kind      *string   `json:"kind"`
	Status    *string   `json:"status"`
	Labels    []string  `json:"labels"`
	Assignee  *string   `json:"assignee"`
	Parent    *refJSON  `json:"parent"`
	BlockedBy []refJSON `json:"blocked_by"`
	Path      string    `json:"path"`
	Rev       string    `json:"rev"`
}

// newItemSummary reads the summary from a parsed Item file. Identity comes
// from the filename; a missing title or Kind is derived from the filename
// and the folder.
func newItemSummary(f vault.ItemFile, p item.Parsed, data []byte) itemSummary {
	s := itemSummary{
		ID:        f.ID(),
		Title:     itemTitle(f, p),
		Kind:      p.Kind,
		Status:    p.Status,
		Labels:    p.Labels,
		Assignee:  p.Assignee,
		BlockedBy: []refJSON{},
		Path:      f.Path,
		Rev:       item.Rev(data),
	}
	if s.Labels == nil {
		s.Labels = []string{}
	}
	if s.Kind == nil {
		if k, ok := item.KindOfFolder(path.Base(path.Dir(f.Path))); ok {
			kind := string(k)
			s.Kind = &kind
		}
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

// itemFull is the complete Item that view returns.
type itemFull struct {
	itemSummary
	Author   *string       `json:"author"`
	Body     string        `json:"body"`
	Created  *string       `json:"created"`
	Updated  *string       `json:"updated"`
	Comments []commentJSON `json:"comments"`
	Children []refJSON     `json:"children"`
	Blocks   []refJSON     `json:"blocks"`
}

func newItemFull(f vault.ItemFile, p item.Parsed, data []byte) itemFull {
	full := itemFull{
		itemSummary: newItemSummary(f, p, data),
		Author:      p.Author,
		Body:        p.Body,
		Created:     p.Created,
		Updated:     p.Updated,
		Comments:    make([]commentJSON, len(p.Comments)),
		Children:    []refJSON{},
		Blocks:      []refJSON{},
	}
	for i, c := range p.Comments {
		full.Comments[i] = commentJSON(c)
	}
	return full
}
