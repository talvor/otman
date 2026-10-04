// Package docs embeds the docs that otman prints.
package docs

import _ "embed"

// TrackerTemplate is the Tracker template, with {{KEY}} standing for the
// linked Project's key.
//
//go:embed agents/issue-tracker-otman.md
var TrackerTemplate string
