// Renders diagnostics as file:line:column with code and message.

package diag

import (
	"fmt"
	"strings"
)

// RenderString renders the diagnostic as
// "<file>:<line>:<col>: <code> <message>". A help line follows when the
// diagnostic carries a fix suggestion, then one line per note. The
// diagnostic must carry a valid span.
func (d Diagnostic) RenderString() string {
	var b strings.Builder
	d.renderHead(&b, string(d.Code), d.Message)
	if d.Help != "" {
		b.WriteString("    help: ")
		b.WriteString(d.Help)
		b.WriteByte('\n')
	}
	for _, n := range d.Notes {
		d.renderNote(&b, n)
	}
	return b.String()
}

// renderHead writes "<file>:<line>:<col>: <code> <message>".
func (d Diagnostic) renderHead(b *strings.Builder, code, message string) {
	line, col := d.Span.StartPos()
	fmt.Fprintf(b, "%s:%d:%d: %s %s\n", d.Span.File.Name(), line, col, code, message)
}

// renderNote writes "<file>:<line>:<col>: note: <message>".
func (d Diagnostic) renderNote(b *strings.Builder, n Note) {
	line, col := n.Span.StartPos()
	fmt.Fprintf(b, "%s:%d:%d: note: %s\n", n.Span.File.Name(), line, col, n.Message)
}
