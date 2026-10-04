// Package docs embeds the docs that otman prints.
package docs

import (
	_ "embed"
	"strings"
)

// trackerTemplate is the Tracker template, with {{KEY}} standing for the
// linked Project's key.
//
//go:embed agents/issue-tracker-otman.md
var trackerTemplate string

// TrackerTemplate returns the Tracker template for the Project key.
func TrackerTemplate(key string) string {
	return strings.ReplaceAll(trackerTemplate, "{{KEY}}", key)
}
