package cli

import (
	"errors"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/config"
	"github.com/talvor/otman/internal/item"
)

// parseKind reads the value of --kind.
func parseKind(s string) (item.Kind, error) {
	kind, ok := item.ParseKind(s)
	if !ok {
		return "", invalid("invalid_kind", "unknown Kind "+quoteArg(s),
			map[string]any{"kind": s, "allowed": item.Kinds}, "use --kind issue, prd or spec")
	}
	return kind, nil
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
		return "", noActor(flag+" @me", map[string]any{"flag": flag})
	}
	return name, err
}

// requireActor is the Actor a command that records an identity acts as,
// failing with no_actor when none is configured.
func requireActor(s resolved, command string) (string, error) {
	actor, err := s.ResolveUser("@me")
	if errors.Is(err, config.ErrNoActor) {
		return "", noActor(command, nil)
	}
	if err := checkActor(s); err != nil {
		return "", err
	}
	return actor, err
}

// checkActor rejects a configured Actor that is not valid UTF-8.
func checkActor(s resolved) error {
	if s.Actor.IsSet() && !utf8.ValidString(s.Actor.Value) {
		return invalid("invalid_arguments", "the actor is not valid UTF-8",
			map[string]any{"source": string(s.Actor.Source)}, "")
	}
	return nil
}

// noActor reports that what needs an Actor, and none is configured.
func noActor(what string, details map[string]any) *Error {
	return invalid("no_actor", what+" needs an actor, and none is configured", details,
		"pass --actor NAME, set "+config.Actor.EnvVar+", or run 'otman config set actor NAME'")
}

// singleLine reports whether s is one line of UTF-8 text: no control
// characters but tabs.
func singleLine(s string) bool {
	return utf8.ValidString(s) && strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\t' }) < 0
}

// readText returns the text of --NAME or --NAME-file (- is stdin), such
// as --body and --body-file, which conflict. It must be UTF-8 and must not
// contain the comments marker. It fails with unreadable_body_file or
// unreadable_comment_file, invalid_body or invalid_comment, and
// reserved_marker.
func (a *app) readText(cmd *cobra.Command, name, text, file string) (string, error) {
	fl := cmd.Flags()
	textFlag, fileFlag := "--"+name, "--"+name+"-file"
	if fl.Changed(name) && fl.Changed(name+"-file") {
		return "", conflictingFlags(textFlag, fileFlag,
			"pass the "+name+" with either "+textFlag+" or "+fileFlag+", not both")
	}
	source := textFlag
	if fl.Changed(name + "-file") {
		var b []byte
		var err error
		if file == "-" {
			source = "stdin"
			if a.opts.Stdin != nil {
				b, err = io.ReadAll(a.opts.Stdin)
			}
		} else {
			source = file
			b, err = os.ReadFile(a.abs(file))
		}
		if err != nil {
			msg := err.Error()
			if errors.Is(err, os.ErrNotExist) {
				msg = "no such file " + file
			}
			return "", invalid("unreadable_"+name+"_file", "cannot read "+fileFlag+": "+msg,
				map[string]any{"path": file}, "pass a readable file, or - for stdin")
		}
		text = string(b)
	}
	if !utf8.ValidString(text) {
		return "", invalid("invalid_"+name, "the "+name+" from "+source+" is not valid UTF-8",
			map[string]any{"source": source}, "pass the "+name+" as UTF-8 Markdown")
	}
	if item.ContainsMarker(text) {
		return "", invalid("reserved_marker",
			"the "+name+" from "+source+" contains the reserved line "+item.CommentsMarker,
			map[string]any{"source": source, "marker": item.CommentsMarker},
			"remove that line; otman uses it to mark where comments begin")
	}
	return text, nil
}
