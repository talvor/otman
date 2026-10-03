package vault

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/talvor/otman/internal/frontmatter"
	"github.com/talvor/otman/internal/fsutil"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"go.yaml.in/yaml/v3"
)

// ProjectsDir holds one folder per Project.
const ProjectsDir = "Projects"

// ErrProjectExists means a Project folder already exists for the key.
var ErrProjectExists = errors.New("project already exists")

var keyPattern = regexp.MustCompile(`^` + item.KeyPattern + `$`)

// ValidKey reports whether key can name a Project: an uppercase letter
// followed by up to 15 uppercase letters or digits, such as OTM.
func ValidKey(key string) bool { return keyPattern.MatchString(key) }

// Project is a Project as found on disk. A Project exists because its
// folder exists; its Project note is optional.
type Project struct {
	Key  string
	Name *string // from the Project note; nil when the note gives none
	Path string  // Vault-relative folder, slash-separated
	Note *string // Vault-relative Project note path; nil when it is missing
}

// Projects lists every Project folder under Projects/, sorted by key.
// Folders whose names are not valid keys are skipped with a warning.
func (v *Vault) Projects() ([]Project, []output.Problem, error) {
	keys, warnings, err := v.projectKeys()
	if err != nil {
		return nil, nil, err
	}
	ps := make([]Project, 0, len(keys))
	for _, k := range keys {
		p, ws, err := v.loadProject(k)
		if err != nil {
			return nil, nil, err
		}
		ps = append(ps, p)
		warnings = append(warnings, ws...)
	}
	return ps, warnings, nil
}

// Project returns the Project with key, and false when it does not exist.
func (v *Vault) Project(key string) (Project, bool, []output.Problem, error) {
	if !ValidKey(key) {
		return Project{}, false, nil, nil
	}
	if ok, err := isDir(filepath.Join(v.Root, ProjectsDir, key)); err != nil || !ok {
		return Project{}, false, nil, err
	}
	p, ws, err := v.loadProject(key)
	return p, err == nil, ws, err
}

// projectKeys returns the keys of the Project folders, sorted.
func (v *Vault) projectKeys() ([]string, []output.Problem, error) {
	dir := filepath.Join(v.Root, ProjectsDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var keys []string
	var warnings []output.Problem
	for _, e := range entries {
		if ok, err := isDir(filepath.Join(dir, e.Name())); err != nil {
			return nil, nil, err
		} else if !ok {
			continue
		}
		if !ValidKey(e.Name()) {
			rel := path.Join(ProjectsDir, e.Name())
			warnings = append(warnings, output.Warning("ignored_project_folder",
				"ignoring "+rel+": its name is not a Project key",
				map[string]any{"path": rel},
				"rename the folder to a Project key such as OTM (uppercase letters and digits)"))
			continue
		}
		keys = append(keys, e.Name())
	}
	sort.Strings(keys)
	return keys, warnings, nil
}

// isDir reports whether p is a directory, following symlinks. A missing p
// is not an error.
func isDir(p string) (bool, error) {
	fi, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil && fi.IsDir(), err
}

// loadProject reads a Project's note. A missing or unreadable note leaves the
// name unset and warns, but the Project still exists.
func (v *Vault) loadProject(key string) (Project, []output.Problem, error) {
	p := Project{Key: key, Path: path.Join(ProjectsDir, key)}
	note := path.Join(p.Path, key+".md")
	b, err := os.ReadFile(filepath.Join(v.Root, filepath.FromSlash(note)))
	if errors.Is(err, os.ErrNotExist) {
		return p, []output.Problem{output.Warning("missing_project_note",
			"Project "+key+" has no Project note "+note,
			map[string]any{"project": key, "path": note},
			"create "+note+" with frontmatter name: and kind: project")}, nil
	}
	if err != nil {
		return p, nil, err
	}
	p.Note = &note
	fm, _, ok := frontmatter.Split(b)
	if !ok {
		return p, nil, nil
	}
	var doc map[string]any
	if err := yaml.Unmarshal(fm, &doc); err != nil {
		return p, []output.Problem{output.Warning("malformed_frontmatter",
			"cannot read the frontmatter of "+note+": "+err.Error(),
			map[string]any{"path": note},
			"fix the YAML between the --- lines in "+note)}, nil
	}
	if name, ok := doc["name"].(string); ok && name != "" {
		p.Name = &name
	}
	return p, nil, nil
}

// adopt records every Project folder in the db registry, so a fresh or
// rebuilt db knows every Project in the Vault, and reconciles each
// Project's high-water mark with its Item files (ADR 0003): the mark is
// raised to the highest number on disk and never lowered.
func (v *Vault) adopt() error {
	keys, _, err := v.projectKeys()
	if err != nil || len(keys) == 0 {
		return err
	}
	highest := make(map[string]int, len(keys))
	for _, k := range keys {
		files, err := v.ItemFiles(k)
		if err != nil {
			return err
		}
		highest[k] = highestNumber(files)
	}
	return inImmediateTx(v.db, func(tx *sql.Tx) error {
		for _, k := range keys {
			if err := raiseHighWater(tx, k, highest[k]); err != nil {
				return err
			}
		}
		return nil
	})
}

// raiseHighWater registers Project key if needed and raises its high-water
// mark to at least n.
func raiseHighWater(tx *sql.Tx, key string, n int) error {
	if _, err := tx.Exec(`INSERT OR IGNORE INTO projects (key) VALUES (?)`, key); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE projects SET high_water = MAX(high_water, ?) WHERE key = ?`, n, key)
	return err
}

// register adds Projects to the db registry, keeping any existing row and
// its high-water mark.
func (v *Vault) register(keys ...string) error {
	return inImmediateTx(v.db, func(tx *sql.Tx) error {
		for _, k := range keys {
			if err := raiseHighWater(tx, k, 0); err != nil {
				return err
			}
		}
		return nil
	})
}

// CreateProject creates Projects/<KEY>/ with its Project note and its two
// Bases views. An existing folder is ErrProjectExists; nothing is ever
// overwritten. If writing the files fails, the new folder is removed, so a
// retry is not blocked by a half-made Project.
func (v *Vault) CreateProject(key, name string) (Project, error) {
	if !ValidKey(key) {
		return Project{}, fmt.Errorf("invalid project key %q", key)
	}
	projects := filepath.Join(v.Root, ProjectsDir)
	if err := os.MkdirAll(projects, 0o755); err != nil {
		return Project{}, err
	}
	dir := filepath.Join(projects, key)
	if err := os.Mkdir(dir, 0o755); errors.Is(err, os.ErrExist) {
		return Project{}, ErrProjectExists
	} else if err != nil {
		return Project{}, err
	}
	if err := writeProject(dir, key, name); err != nil {
		os.RemoveAll(dir)
		return Project{}, err
	}
	if err := fsutil.SyncDir(projects); err != nil {
		return Project{}, err
	}
	if err := v.register(key); err != nil {
		return Project{}, err
	}
	p, _, err := v.loadProject(key)
	return p, err
}

// writeProject writes a new Project's note and Bases views into dir.
func writeProject(dir, key, name string) error {
	note, err := projectNote(name)
	if err != nil {
		return err
	}
	files := []struct {
		name string
		data []byte
	}{
		{key + ".md", note},
		{key + " Board.base", []byte(boardBase(key))},
		{key + " Triage.base", []byte(triageBase(key))},
	}
	for _, f := range files {
		if err := fsutil.WriteFile(filepath.Join(dir, f.name), f.data); err != nil {
			return err
		}
	}
	return nil
}

func projectNote(name string) ([]byte, error) {
	fm, err := yaml.Marshal(struct {
		Name string `yaml:"name"`
		Kind string `yaml:"kind"`
	}{name, "project"})
	if err != nil {
		return nil, err
	}
	return append(append([]byte("---\n"), fm...), "---\n"...), nil
}

// itemFilters selects a Project's Items: its Markdown files other than
// the Project note and its Templates.
func itemFilters(key string) string {
	return fmt.Sprintf(`filters:
  and:
    - file.inFolder("Projects/%[1]s")
    - file.ext == "md"
    - 'kind != "project"'
    - not:
        - file.inFolder("Projects/%[1]s/Templates")
`, key)
}

// boardBase is the status board: every Item, grouped by status.
func boardBase(key string) string {
	return itemFilters(key) + `views:
  - type: table
    name: Board
    groupBy:
      property: status
      direction: ASC
    order:
      - file.name
      - kind
      - labels
      - assignee
      - parent
      - blocked_by
`
}

// triageBase is the triage view: open Items, grouped by labels.
func triageBase(key string) string {
	return itemFilters(key) + `views:
  - type: table
    name: Triage
    filters:
      and:
        - 'status == "open"'
    groupBy:
      property: labels
      direction: ASC
    order:
      - file.name
      - kind
      - labels
      - assignee
`
}
