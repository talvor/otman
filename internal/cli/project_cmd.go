package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/config"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

func (a *app) newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Create, list, view and link Projects",
	}

	var name string
	create := &cobra.Command{
		Use:   "create KEY --name NAME",
		Short: "Create a Project folder, its Project note and its Bases views",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.projectCreate(args[0], name)
		},
	}
	create.Flags().StringVar(&name, "name", "", "the Project's display name (required)")
	cmd.AddCommand(create)

	list := &cobra.Command{
		Use:   "list",
		Short: "List the Projects in the Vault",
		Args:  cobra.NoArgs,
	}
	p := addPaging(list)
	list.RunE = func(*cobra.Command, []string) error { return a.projectList(p) }
	cmd.AddCommand(list)

	cmd.AddCommand(&cobra.Command{
		Use:   "view [KEY]",
		Short: "Show a Project (default: the selected Project)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			key := ""
			if len(args) == 1 {
				key = args[0]
			}
			return a.projectView(key)
		},
	})

	var force bool
	link := &cobra.Command{
		Use:   "link KEY",
		Short: "Point the current repo (or directory) at a Project with " + config.PointerFile,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.projectLink(args[0], force)
		},
	}
	link.Flags().BoolVar(&force, "force", false, "replace a pointer to a different Project")
	cmd.AddCommand(link)
	return cmd
}

// projectJSON is a Project in every result.
type projectJSON struct {
	Key  string  `json:"key"`
	Name *string `json:"name"`
	Path string  `json:"path"`
	Note *string `json:"note"`
}

func newProjectJSON(p vault.Project) projectJSON {
	return projectJSON{Key: p.Key, Name: p.Name, Path: p.Path, Note: p.Note}
}

func (p projectJSON) displayName() string {
	if p.Name == nil {
		return "-"
	}
	return *p.Name
}

func checkKey(key string) error {
	if vault.ValidKey(key) {
		return nil
	}
	return invalid("invalid_project_key", "invalid Project key "+quoteArg(key),
		map[string]any{"key": key},
		"use an uppercase letter followed by up to 15 uppercase letters or digits, such as OTM")
}

func projectNotFound(key string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	details["key"] = key
	return &Error{Exit: ExitNotFound, Code: "project_not_found",
		Message: "no Project " + key + " in the Vault", Details: details,
		Hint: "run 'otman project list' to see the Projects, or 'otman project create " + key + " --name NAME'"}
}

// withVault opens the Vault, runs fn and closes the Vault, releasing the
// lock.
func (a *app) withVault(fn func(resolved, *vault.Vault, []output.Problem) error) error {
	s, err := a.settings()
	if err != nil {
		return err
	}
	v, warnings, err := a.openVault(s)
	if err != nil {
		return err
	}
	err = fn(s, v, warnings)
	if cerr := v.Close(); err == nil && cerr != nil {
		err = ioError(cerr)
	}
	return err
}

type projectCreateResult struct {
	Project projectJSON `json:"project"`
	Changed bool        `json:"changed"`
}

func (r projectCreateResult) RenderHuman(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Created Project %s (%s) in %s\n", r.Project.Key, r.Project.displayName(), r.Project.Path)
	return err
}

func (a *app) projectCreate(key, name string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return invalid("invalid_arguments", "--name is required and cannot be empty",
			map[string]any{"flag": "--name"}, "pass the Project's display name with --name")
	}
	if strings.ContainsAny(name, "\r\n") {
		return invalid("invalid_arguments", "--name must be a single line",
			map[string]any{"flag": "--name"}, "")
	}
	return a.withVault(func(_ resolved, v *vault.Vault, warnings []output.Problem) error {
		p, err := v.CreateProject(key, name)
		if errors.Is(err, vault.ErrProjectExists) {
			path := vault.ProjectsDir + "/" + key
			return &Error{Exit: ExitConflict, Code: "project_exists",
				Message: "Project " + key + " already exists at " + path,
				Details: map[string]any{"key": key, "path": path},
				Hint:    "choose another key; otman never overwrites a Project"}
		}
		if err != nil {
			return ioError(err)
		}
		return a.emit(projectCreateResult{newProjectJSON(p), true}, warnings)
	})
}

type projectListResult struct {
	collection[projectJSON]
}

func (r projectListResult) RenderHuman(w io.Writer) error {
	if r.Total == 0 {
		_, err := fmt.Fprintln(w, "No Projects in the Vault")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "KEY\tNAME\tPATH")
	for _, p := range r.Items {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", p.Key, p.displayName(), p.Path)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return r.renderMore(w)
}

func (a *app) projectList(p *paging) error {
	if err := p.validate(); err != nil {
		return err
	}
	return a.withVault(func(_ resolved, v *vault.Vault, warnings []output.Problem) error {
		ps, ws, err := v.Projects()
		if err != nil {
			return ioError(err)
		}
		all := make([]projectJSON, len(ps))
		for i, pr := range ps {
			all[i] = newProjectJSON(pr)
		}
		return a.emit(projectListResult{page(p, all)}, append(warnings, ws...))
	})
}

type projectViewResult struct {
	Project projectJSON `json:"project"`
}

func (r projectViewResult) RenderHuman(w io.Writer) error {
	p := r.Project
	note := "-"
	if p.Note != nil {
		note = *p.Note
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "key\t%s\nname\t%s\npath\t%s\nnote\t%s\n", p.Key, p.displayName(), p.Path, note)
	return tw.Flush()
}

// projectView shows the Project named by key, or else the selected
// Project. It never falls back to some other Project.
func (a *app) projectView(key string) error {
	if key != "" {
		if err := checkKey(key); err != nil {
			return err
		}
	}
	return a.withVault(func(s resolved, v *vault.Vault, warnings []output.Problem) error {
		var details map[string]any
		if key == "" {
			sel, err := s.project()
			if err != nil {
				return err
			}
			if !sel.IsSet() {
				return invalid("no_project", "no Project selected", nil,
					"pass a KEY or --project, set "+config.Project.EnvVar+", run 'otman project link KEY', or 'otman config set project KEY'")
			}
			key = sel.Value
			details = map[string]any{"source": string(sel.Source)}
		}
		p, ok, ws, err := v.Project(key)
		if err != nil {
			return ioError(err)
		}
		if !ok {
			return projectNotFound(key, details)
		}
		return a.emit(projectViewResult{newProjectJSON(p)}, append(warnings, ws...))
	})
}

type projectLinkResult struct {
	Project  string  `json:"project"`
	Path     string  `json:"path"`
	Previous *string `json:"previous"`
	Changed  bool    `json:"changed"`
}

func (r projectLinkResult) RenderHuman(w io.Writer) error {
	var err error
	switch {
	case !r.Changed:
		_, err = fmt.Fprintf(w, "%s already points to Project %s\n", r.Path, r.Project)
	case r.Previous != nil:
		_, err = fmt.Fprintf(w, "Linked %s to Project %s (was %s)\n", r.Path, r.Project, *r.Previous)
	default:
		_, err = fmt.Fprintf(w, "Linked %s to Project %s\n", r.Path, r.Project)
	}
	return err
}

// projectLink writes the repo pointer at the git root, or in the working
// directory outside git. Replacing a pointer to another Project, or one
// otman cannot read, needs --force.
func (a *app) projectLink(key string, force bool) error {
	if err := checkKey(key); err != nil {
		return err
	}
	return a.withVault(func(_ resolved, v *vault.Vault, warnings []output.Problem) error {
		if _, ok, _, err := v.Project(key); err != nil {
			return ioError(err)
		} else if !ok {
			return projectNotFound(key, nil)
		}
		dir, err := config.LinkDir(a.opts.Dir)
		if err != nil {
			return ioError(err)
		}
		path := filepath.Join(dir, config.PointerFile)
		r := projectLinkResult{Project: key, Path: path, Changed: true}

		current, err := config.ReadPointer(path)
		var parseErr *config.ParseError
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil && !errors.As(err, &parseErr):
			return ioError(err)
		case err != nil && !force:
			return pointerError(err, "fix "+path+", or pass --force to replace it")
		case err != nil:
		case current == key:
			r.Changed = false
			return a.emit(r, warnings)
		case !force:
			return &Error{Exit: ExitConflict, Code: "pointer_conflict",
				Message: path + " already points to Project " + current,
				Details: map[string]any{"path": path, "current": current, "requested": key},
				Hint:    "pass --force to point it at " + key + " instead"}
		default:
			r.Previous = &current
		}
		if err := config.WritePointer(path, key); err != nil {
			return ioError(err)
		}
		return a.emit(r, warnings)
	})
}
