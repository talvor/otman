package cli

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/config"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

func (a *app) newCommentCmd() *cobra.Command {
	var body, bodyFile string
	cmd := &cobra.Command{
		Use:   "comment REF (--body TEXT | --body-file PATH|-)",
		Short: "Append a comment to an Item",
		Args:  cobra.ExactArgs(1),
	}
	fl := cmd.Flags()
	fl.StringVar(&body, "body", "", "the comment, as Markdown")
	fl.StringVar(&bodyFile, "body-file", "", "read the comment from PATH, or from stdin with -")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return a.comment(cmd, args[0], body, bodyFile)
	}
	return cmd
}

// comment appends a comment by the actor to the Item ref names. It is not
// retry-idempotent: every run appends another comment.
func (a *app) comment(cmd *cobra.Command, ref, body, bodyFile string) error {
	if !cmd.Flags().Changed("body") && !cmd.Flags().Changed("body-file") {
		return invalid("invalid_arguments", "comment needs --body or --body-file", nil,
			"pass the comment with --body TEXT, or --body-file PATH (- for stdin)")
	}
	text, err := a.readComment(cmd, "body", body, bodyFile)
	if err != nil {
		return err
	}
	s, err := a.settings()
	if err != nil {
		return err
	}
	c, err := a.newComment(s, "comment", text)
	if err != nil {
		return err
	}
	return a.withVault(s, func(v *vault.Vault, warnings []output.Problem) error {
		f, err := resolveRef(s, v, ref)
		if err != nil {
			return err
		}
		data, err := v.ReadItemFile(f)
		if err != nil {
			return ioError(err)
		}
		if data, err = item.AppendComment(data, c, a.opts.Now()); err != nil {
			return writeError(f, err)
		}
		if err := v.WriteItemFile(f, data); err != nil {
			return ioError(err)
		}
		files, err := v.ItemFiles(f.Key)
		if err != nil {
			return ioError(err)
		}
		summary := newItemSummary(f, item.Parse(data), data, newItemLinks(files))
		human := fmt.Sprintf("Commented on %s · %s\n", summary.ID, summary.Title)
		return a.emit(mutationResult{summary, true, human}, warnings)
	})
}

// readComment returns the text of --NAME or --NAME-file, as readText
// does, failing with empty_comment when it has nothing but whitespace.
func (a *app) readComment(cmd *cobra.Command, name, text, file string) (string, error) {
	text, err := a.readText(cmd, name, text, file)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", invalid("empty_comment", "the comment is empty", nil,
			"pass the comment's text with --"+name+" or --"+name+"-file")
	}
	return text, nil
}

// newComment is a comment with text by the actor, created now. command
// names what needs the actor, for the failure when none is configured.
func (a *app) newComment(s resolved, command, text string) (item.Comment, error) {
	if !s.Actor.IsSet() {
		return item.Comment{}, invalid("no_actor", command+" needs an actor, and none is configured", nil,
			"pass --actor NAME, set "+config.Actor.EnvVar+", or run 'otman config set actor NAME'")
	}
	actor := s.Actor.Value
	if !utf8.ValidString(actor) || strings.IndexFunc(actor, func(r rune) bool { return unicode.IsControl(r) && r != '\t' }) >= 0 {
		return item.Comment{}, invalid("invalid_arguments", "the actor must be a single line of UTF-8 text",
			map[string]any{"source": string(s.Actor.Source)}, "")
	}
	return item.Comment{Author: actor, Created: a.opts.Now().UTC().Format(time.RFC3339), Body: text}, nil
}
