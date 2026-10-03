package cli

import (
	"strings"

	"github.com/talvor/otman/internal/output"
)

// Exit codes are part of otman's stable contract.
const (
	ExitOK       = 0
	ExitFailure  = 1 // I/O or unexpected failure
	ExitInvalid  = 2 // invalid arguments, config or input
	ExitNotFound = 3
	ExitConflict = 4 // conflict, ambiguity, invalid graph, stale or unsafe write
)

// Error is a failure with a stable code and exit status. Commands return it
// for every failure they anticipate; anything else from cobra is a usage
// error, and anything else from otman's own code is wrapped as io_error.
type Error struct {
	Exit    int
	Code    string
	Message string
	Details map[string]any
	Hint    string
}

func (e *Error) Error() string { return e.Message }

func (e *Error) problem() output.Problem {
	p := output.Problem{Code: e.Code, Message: e.Message, Details: e.Details}
	if e.Hint != "" {
		h := e.Hint
		p.Hint = &h
	}
	return p
}

// invalid is a failure with exit 2: invalid arguments, config or input.
func invalid(code, msg string, details map[string]any, hint string) *Error {
	return &Error{Exit: ExitInvalid, Code: code, Message: msg, Details: details, Hint: hint}
}

func invalidArgs(msg, hint string) *Error { return invalid("invalid_arguments", msg, nil, hint) }

// conflictingFlags reports that flag and other were passed together but
// exclude each other.
func conflictingFlags(flag, other, hint string) *Error {
	return invalid("conflicting_flags", flag+" conflicts with "+other,
		map[string]any{"flags": []string{flag, other}}, hint)
}

// exclusive fails with conflicting_flags when more than one of the flags
// names was given.
func exclusive(changed func(string) bool, names ...string) error {
	var given []string
	for _, n := range names {
		if changed(n) {
			given = append(given, "--"+n)
		}
	}
	if len(given) < 2 {
		return nil
	}
	return conflictingFlags(given[0], given[1], "pass only one of --"+strings.Join(names, ", --"))
}

// unsafeWrite refuses to rewrite the Item file at path for reason.
func unsafeWrite(path, reason, hint string) *Error {
	return &Error{Exit: ExitConflict, Code: "unsafe_write",
		Message: "refusing to rewrite " + path + ": " + reason,
		Details: map[string]any{"path": path, "reason": reason}, Hint: hint}
}

func ioError(err error) *Error {
	return &Error{Exit: ExitFailure, Code: "io_error", Message: err.Error()}
}
