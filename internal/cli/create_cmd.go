package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
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
	labels     []string
	parent     string
	blockedBy  []string
}

func (a *app) newCreateCmd() *cobra.Command {
	var f createFlags
	cmd := &cobra.Command{
		Use:   "create --title TEXT [--kind issue|prd|spec] [--body TEXT | --body-file PATH|- | --no-template] [--label L]... [--assignee NAME|@me] [--parent REF] [--blocked-by REF]...",
		Short: "Create an Item in the selected Project",
		Args:  cobra.NoArgs,
	}
	fl := cmd.Flags()
	fl.StringVar(&f.title, "title", "", "the Item's title (required)")
	fl.StringVar(&f.kind, "kind", string(item.Issue), "issue, prd or spec")
	fl.StringVar(&f.body, "body", "", "the Item's body, as Markdown")
	fl.StringVar(&f.bodyFile, "body-file", "", "read the body from PATH, or from stdin with -")
	fl.BoolVar(&f.noTemplate, "no-template", false, "start with an empty body instead of the Kind's Template")
	fl.StringArrayVar(&f.labels, "label", nil, "add the Label L (repeatable)")
	fl.StringVar(&f.assignee, "assignee", "", "assign the Item to NAME, or to the actor with @me")
	fl.StringVar(&f.parent, "parent", "", "make REF, an Item of the same Project, the parent")
	fl.StringArrayVar(&f.blockedBy, "blocked-by", nil, "record that REF, an Item of the same Project, blocks the new Item (repeatable)")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return a.create(cmd, f) }
	return cmd
}

// mutationResult is the result of every mutation: the Item's summary and
// whether anything changed. human is what a terminal shows.
type mutationResult struct {
	Item    itemSummary `json:"item"`
	Changed bool        `json:"changed"`

	human string
}

func (r mutationResult) RenderHuman(w io.Writer) error {
	_, err := io.WriteString(w, r.human)
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
	if !singleLine(title) {
		return invalid("invalid_arguments", "--title must be a single line of UTF-8 text",
			map[string]any{"flag": "--title"}, "")
	}
	kind, err := parseKind(f.kind)
	if err != nil {
		return err
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
	body, err := a.readText(cmd, "body", f.body, f.bodyFile)
	if err != nil {
		return err
	}
	labels, err := parseLabels("--label", f.labels)
	if err != nil {
		return err
	}
	if cmd.Flags().Changed("parent") && f.parent == "" {
		return invalid("invalid_arguments", "--parent cannot be empty",
			map[string]any{"flag": "--parent"}, "pass the parent Item, or omit --parent")
	}
	if slices.Contains(f.blockedBy, "") {
		return invalid("invalid_arguments", "--blocked-by cannot be empty",
			map[string]any{"flag": "--blocked-by"}, "pass the blocking Item, or omit --blocked-by")
	}
	s, err := a.settings()
	if err != nil {
		return err
	}
	if err := checkActor(s); err != nil {
		return err
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
		parent, blockers, err := createRelations(v, key.Value, cmd.Flags().Changed("parent"), f.parent, f.blockedBy)
		if err != nil {
			return err
		}
		inUse, err := labelsInUse(v, key.Value, "")
		if err != nil {
			return ioError(err)
		}
		file, data, err := v.CreateItem(key.Value, vault.NewItem{
			Title: title, Kind: kind, Body: body,
			Author: author, Assignee: assignee, Labels: labels,
			Parent: parent, BlockedBy: blockers, Now: a.opts.Now(),
		})
		if err != nil {
			return ioError(err)
		}
		warnings = append(warnings, newLabelWarnings(key.Value, labels, inUse)...)
		files, err := v.ItemFiles(key.Value)
		if err != nil {
			return ioError(err)
		}
		summary := newItemSummary(file, item.Parse(data), data, newItemLinks(files))
		human := fmt.Sprintf("Created %s · %s\n%s\n", summary.ID, summary.Title, summary.Path)
		return a.emit(mutationResult{summary, true, human}, warnings)
	})
}

// createRelations resolves the --parent and --blocked-by targets of a new
// Item in Project key to the wikilinks it stores, without repeats. A new
// Item has no children and blocks nothing, so no edge can close a cycle.
func createRelations(v *vault.Vault, key string, hasParent bool, parentRef string, blockerRefs []string) (*string, []string, error) {
	var parent *string
	if hasParent {
		t, err := resolveTarget(v, key, parentRef, "parent")
		if err != nil {
			return nil, nil, err
		}
		link := item.Link(t.Name())
		parent = &link
	}
	var blockers []string
	for _, ref := range blockerRefs {
		t, err := resolveTarget(v, key, ref, "blocker")
		if err != nil {
			return nil, nil, err
		}
		if link := item.Link(t.Name()); !slices.Contains(blockers, link) {
			blockers = append(blockers, link)
		}
	}
	return parent, blockers, nil
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

// selectedProject is the selected Project, failing when there is none.
// altFlags name other flags that would do instead of --project, such as
// "--all-projects", for the failure's hint.
func selectedProject(s resolved, altFlags ...string) (config.Value, error) {
	sel, err := s.project()
	if err != nil {
		return config.Value{}, err
	}
	if !sel.IsSet() {
		return config.Value{}, noProject(altFlags...)
	}
	return sel, nil
}

// noProject reports that no Project is selected. altFlags name other flags
// that would do instead of --project.
func noProject(altFlags ...string) error {
	flags := strings.Join(append([]string{"--project"}, altFlags...), " or ")
	return invalid("no_project", "no Project selected", nil,
		"pass "+flags+", set "+config.Project.EnvVar+", run 'otman project link KEY', or 'otman config set project KEY'")
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
