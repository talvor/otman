// Package output renders otman's typed results and failures in the three
// output formats: JSON (the stable, versioned contract), AXI (compact TOON for
// agents and pipes) and human (tabwriter tables for terminals).
package output

import (
	"encoding/json"
	"fmt"
	"io"
)

// SchemaVersion is the version of the JSON contract.
const SchemaVersion = 1

// Format is an output format.
type Format string

const (
	Human Format = "human"
	AXI   Format = "axi"
	JSON  Format = "json"
)

// ParseFormat validates a --format value.
func ParseFormat(s string) (Format, bool) {
	switch f := Format(s); f {
	case Human, AXI, JSON:
		return f, true
	}
	return "", false
}

// Default is the format used when none is requested: human on a terminal,
// AXI otherwise.
func Default(isTTY bool) Format {
	if isTTY {
		return Human
	}
	return AXI
}

// Problem is the shape shared by errors and warnings.
type Problem struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
	Hint    *string        `json:"hint"`
}

// HumanRenderer is implemented by every result type; it writes the
// terminal-friendly form of the result.
type HumanRenderer interface {
	RenderHuman(w io.Writer) error
}

type successEnvelope struct {
	SchemaVersion int       `json:"schema_version"`
	Data          any       `json:"data"`
	Warnings      []Problem `json:"warnings"`
}

type errorEnvelope struct {
	SchemaVersion int     `json:"schema_version"`
	Error         Problem `json:"error"`
}

// Success writes a result. JSON wraps data and warnings in the envelope on
// stdout; human and AXI write data to stdout and warnings to stderr.
func Success(f Format, stdout, stderr io.Writer, data HumanRenderer, warnings []Problem) error {
	if warnings == nil {
		warnings = []Problem{}
	}
	switch f {
	case JSON:
		return writeJSON(stdout, successEnvelope{SchemaVersion, data, warnings})
	case AXI:
		if err := EncodeTOON(stdout, data); err != nil {
			return err
		}
	default:
		if err := data.RenderHuman(stdout); err != nil {
			return err
		}
	}
	for _, w := range warnings {
		if err := writeProblem(f, stderr, "warning", w); err != nil {
			return err
		}
	}
	return nil
}

// Failure writes an error diagnostic to stderr in the selected format.
func Failure(f Format, stderr io.Writer, p Problem) error {
	if f == JSON {
		return writeJSON(stderr, errorEnvelope{SchemaVersion, p})
	}
	return writeProblem(f, stderr, "error", p)
}

func writeProblem(f Format, w io.Writer, label string, p Problem) error {
	if f == AXI {
		return EncodeTOON(w, map[string]any{label: p})
	}
	// Human output names the stable code but leaves details to AXI and JSON.
	if _, err := fmt.Fprintf(w, "%s[%s]: %s\n", label, p.Code, p.Message); err != nil {
		return err
	}
	if p.Hint != nil {
		if _, err := fmt.Fprintf(w, "hint: %s\n", *p.Hint); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
