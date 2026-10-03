package cli

import (
	"errors"
	"strings"
	"unicode/utf8"

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
		return "", invalid("no_actor", flag+" @me needs an actor, and none is configured",
			map[string]any{"flag": flag},
			"pass --actor NAME, set "+config.Actor.EnvVar+", or run 'otman config set actor NAME'")
	}
	return name, err
}
