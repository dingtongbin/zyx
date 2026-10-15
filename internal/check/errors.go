// Checker diagnostics.

package check

import (
	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/source"
	"zyx/internal/types"
)

// errorf reports a diagnostic at span.
func (c *checker) errorf(code diag.Code, span source.Span, format string, args ...any) {
	c.diags = append(c.diags, diag.New(code, span, format, args...))
}

// errorHelp reports a diagnostic with an actionable fix.
func (c *checker) errorHelp(code diag.Code, span source.Span, help, format string, args ...any) {
	c.diags = append(c.diags, diag.New(code, span, format, args...).WithHelp(help))
}

// recordDecl records the declared type of a declaration name.
func (c *checker) recordDecl(id *ast.Ident, t *types.Type) {
	if id != nil && t != nil {
		c.out.Nodes[id] = t
	}
}
