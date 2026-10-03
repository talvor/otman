package vault

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/talvor/otman/internal/item"
)

// vaultTemplatesDir holds the Vault-wide Template overrides.
const vaultTemplatesDir = "Templates/otman"

//go:embed templates/*.md
var builtinTemplates embed.FS

// Template is the text a new Item with no body starts from.
type Template struct {
	Path string // Vault-relative, slash-separated; "" for the built-in
	Text string
}

// Template returns Kind kind's Template for Project key: the first of
// Projects/<KEY>/Templates/<kind>.md, Templates/otman/<kind>.md and the
// built-in. A Template is copied verbatim, so its text is not checked here.
func (v *Vault) Template(key string, kind item.Kind) (Template, error) {
	name := string(kind) + ".md"
	for _, p := range []string{
		path.Join(ProjectsDir, key, TemplatesDir, name),
		path.Join(vaultTemplatesDir, name),
	} {
		b, err := os.ReadFile(filepath.Join(v.Root, filepath.FromSlash(p)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return Template{}, err
		}
		return Template{Path: p, Text: string(b)}, nil
	}
	b, err := builtinTemplates.ReadFile("templates/" + name)
	if err != nil {
		return Template{}, err
	}
	return Template{Text: string(b)}, nil
}
