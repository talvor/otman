package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/config"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

type createFlags struct {
	title      string
	kind       string
	body       string
	bodyFile   string
	noTemplate bool
	assignee   string
}

func (a *app) newCreateCmd() *cobra.Command {
	var f createFlags
	cmd := &cobra.Command{
		Use:   "create --title TEXT [--kind issue|prd|spec] [--body TEXT | --body-file PATH|- | --no-template] [--assignee NAME|@me]",
		Short: "Create an Item in the selected Project",
		Args:  cobra.NoArgs,
	}
	fl := cmd.Flags()
	fl.StringVar(&f.title, "title", "", "the Item's title (required)")
	fl.StringVar(&f.kind, "kind", string(item.Issue), "issue, prd or spec")
	fl.StringVar(&f.body, "body", "", "the Item's body, as Markdown")
	fl.StringVar(&f.bodyFile, "body-file", "", "read the body from PATH, or from stdin with -")
	fl.BoolVar(&f.noTemplate, "no-template", false, "start with an empty body instead of the Kind's Template")
	fl.StringVar(&f.assignee, "assignee", "", "assign the Item to NAME, or to the actor with @me")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return a.create(cmd, f) }
	return cmd
}

// createResult is a mutation result: the Item's summary and whether
// anything changed, which for create is always true.
type createResult struct {
	Item    itemSummary `json:"item"`
	Changed bool        `json:"changed"`
}

func (r createResult) RenderHuman(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Created %s · %s\n%s\n", r.Item.ID, r.Item.Title, r.Item.Path)
	return err
}

func (a *app) create(cmd *cobra.Command, f createFlags) error {
	// The whole request is validated before the Vault is touched.
	// title keeps the exact text; only the filename is a projection.
	title := f.title
	if strings.TrimSpace(title) == "" {
		return invalid("invalid_arguments", "--title is required and cannot be empty",
			map[string]any{"flag": "--title"}, "pass the Item's title with --title")
	}
	if !utf8.ValidString(title) || strings.IndexFunc(title, func(r rune) bool { return unicode.IsControl(r) && r != '\t' }) >= 0 {
		return invalid("invalid_arguments", "--title must be a single line of UTF-8 text",
			map[string]any{"flag": "--title"}, "")
	}
	kind, ok := item.ParseKind(f.kind)
	if !ok {
		return invalid("invalid_kind", "unknown Kind "+quoteArg(f.kind),
			map[string]any{"kind": f.kind, "allowed": item.Kinds}, "use --kind issue, prd or spec")
	}
	// An explicit body, even an empty one, is stored as given; only an
	// omitted body takes the Kind's Template.
	bodyGiven := cmd.Flags().Changed("body") || cmd.Flags().Changed("body-file")
	if f.noTemplate && bodyGiven {
		flag := "--body"
		if !cmd.Flags().Changed("body") {
			flag = "--body-file"
		}
		return invalid("conflicting_flags", "--no-template conflicts with "+flag,
			map[string]any{"flags": []string{"--no-template", flag}},
			"pass a body, or --no-template for an empty one, not both")
	}
	body, err := a.readBody(cmd, f.body, f.bodyFile)
	if err != nil {
		return err
	}
	s, err := a.settings()
	if err != nil {
		return err
	}
	if s.Actor.IsSet() && !utf8.ValidString(s.Actor.Value) {
		return invalid("invalid_arguments", "the actor is not valid UTF-8",
			map[string]any{"source": string(s.Actor.Source)}, "")
	}
	var assignee *string
	if cmd.Flags().Changed("assignee") {
		name, err := nameOrActor(s, "--assignee", f.assignee)
		if err != nil {
			return err
		}
		assignee = &name
	}
	var author *string
	if s.Actor.IsSet() {
		author = &s.Actor.Value
	}
	key, err := selectedProject(s)
	if err != nil {
		return err
	}

	return a.withVault(s, func(v *vault.Vault, warnings []output.Problem) error {
		if err := requireProject(v, key.Value, map[string]any{"source": string(key.Source)}); err != nil {
			return err
		}
		if !bodyGiven && !f.noTemplate {
			if body, err = templateBody(v, key.Value, kind); err != nil {
				return err
			}
		}
		file, data, err := v.CreateItem(key.Value, vault.NewItem{
			Title: title, Kind: kind, Body: body,
			Author: author, Assignee: assignee, Now: a.opts.Now(),
		})
		if err != nil {
			return ioError(err)
		}
		files, err := v.ItemFiles(key.Value)
		if err != nil {
			return ioError(err)
		}
		summary := newItemSummary(file, item.Parse(data), data, newItemLinks(files))
		return a.emit(createResult{summary, true}, warnings)
	})
}

// readBody returns the body from --body or --body-file (- is stdin), which
// conflict. It must be UTF-8 and must not contain the comments marker.
func (a *app) readBody(cmd *cobra.Command, body, bodyFile string) (string, error) {
	fl := cmd.Flags()
	if fl.Changed("body") && fl.Changed("body-file") {
		return "", invalid("conflicting_flags", "--body conflicts with --body-file",
			map[string]any{"flags": []string{"--body", "--body-file"}},
			"pass the body with either --body or --body-file, not both")
	}
	source := "--body"
	if fl.Changed("body-file") {
		var b []byte
		var err error
		if bodyFile == "-" {
			source = "stdin"
			if a.opts.Stdin != nil {
				b, err = io.ReadAll(a.opts.Stdin)
			}
		} else {
			source = bodyFile
			b, err = os.ReadFile(a.abs(bodyFile))
		}
		if err != nil {
			msg := err.Error()
			if errors.Is(err, os.ErrNotExist) {
				msg = "no such file " + bodyFile
			}
			return "", invalid("unreadable_body_file", "cannot read --body-file: "+msg,
				map[string]any{"path": bodyFile}, "pass a readable file, or - for stdin")
		}
		body = string(b)
	}
	if !utf8.ValidString(body) {
		return "", invalid("invalid_body", "the body from "+source+" is not valid UTF-8",
			map[string]any{"source": source}, "pass the body as UTF-8 Markdown")
	}
	if item.ContainsMarker(body) {
		return "", invalid("reserved_marker",
			"the body from "+source+" contains the reserved line "+item.CommentsMarker,
			map[string]any{"source": source, "marker": item.CommentsMarker},
			"remove that line; otman uses it to mark where comments begin")
	}
	return body, nil
}

// templateBody is the body a new Item of Kind kind in Project key starts
// from: its Template, copied verbatim. A Template must be UTF-8 and must
// not contain the comments marker.
func templateBody(v *vault.Vault, key string, kind item.Kind) (string, error) {
	t, err := v.Template(key, kind)
	if err != nil {
		return "", ioError(err)
	}
	if !utf8.ValidString(t.Text) {
		return "", invalid("invalid_template_encoding", "the Template "+t.Path+" is not valid UTF-8",
			map[string]any{"path": t.Path}, "save the Template as UTF-8 Markdown")
	}
	if item.ContainsMarker(t.Text) {
		return "", invalid("invalid_template",
			"the Template "+t.Path+" contains the reserved line "+item.CommentsMarker,
			map[string]any{"path": t.Path, "marker": item.CommentsMarker},
			"remove that line from the Template; otman uses it to mark where comments begin")
	}
	return t.Text, nil
}

// nameOrActor maps the value of a NAME|@me flag to the name to store: the
// name as given, or the Actor for @me, which fails when no Actor is
// configured.
func nameOrActor(s resolved, flag, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n") || !utf8.ValidString(value) {
		return "", invalid("invalid_arguments", flag+" needs a single-line UTF-8 name or @me",
			map[string]any{"flag": flag}, "")
	}
	name, err := s.ResolveUser(value)
	if errors.Is(err, config.ErrNoActor) {
		return "", invalid("no_actor", flag+" @me needs an actor, and none is configured",
			map[string]any{"flag": flag},
			"pass --actor NAME, set "+config.Actor.EnvVar+", or run 'otman config set actor NAME'")
	}
	return name, err
}

// selectedProject is the selected Project, failing when there is none.
func selectedProject(s resolved) (config.Value, error) {
	sel, err := s.project()
	if err != nil {
		return config.Value{}, err
	}
	if !sel.IsSet() {
		return config.Value{}, noProject()
	}
	return sel, nil
}

func noProject() error {
	return invalid("no_project", "no Project selected", nil,
		"pass --project, set "+config.Project.EnvVar+", run 'otman project link KEY', or 'otman config set project KEY'")
}

// requireProject fails with project_not_found unless Project key exists.
func requireProject(v *vault.Vault, key string, details map[string]any) error {
	if _, ok, _, err := v.Project(key); err != nil {
		return ioError(err)
	} else if !ok {
		return projectNotFound(key, details)
	}
	return nil
}
