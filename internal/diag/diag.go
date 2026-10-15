// Package diag defines diagnostics, stable error codes, and rendering.

package diag

import (
	"fmt"

	"zyx/internal/source"
)

// Note is an additional labeled position attached to a diagnostic.
type Note struct {
	// Span locates the note in source.
	Span source.Span
	// Message is the note text.
	Message string
}

// Diagnostic is one compiler message at a source position.
type Diagnostic struct {
	// Code is the stable identifier of the error class.
	Code Code
	// Message is the human-readable message.
	Message string
	// Span locates the message in source.
	Span source.Span
	// Help is an optional actionable fix suggestion.
	Help string
	// Notes carry related positions rendered after the main line.
	Notes []Note
}

// New builds a diagnostic for span; the message is formatted like
// fmt.Sprintf.
func New(code Code, span source.Span, format string, args ...any) Diagnostic {
	return Diagnostic{Code: code, Span: span, Message: fmt.Sprintf(format, args...)}
}

// WithHelp attaches an actionable fix suggestion and returns the
// diagnostic.
func (d Diagnostic) WithHelp(help string) Diagnostic {
	d.Help = help
	return d
}

// WithNote attaches a related source position and returns the diagnostic.
func (d Diagnostic) WithNote(span source.Span, format string, args ...any) Diagnostic {
	d.Notes = append(d.Notes, Note{Span: span, Message: fmt.Sprintf(format, args...)})
	return d
}
