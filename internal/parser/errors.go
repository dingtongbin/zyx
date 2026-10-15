// Syntax error reporting, recovery, and token description.

package parser

import (
	"strconv"

	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/source"
	"zyx/internal/token"
)

// errorAt records a syntax diagnostic spanning tok.
func (p *parser) errorAt(tok token.Token, code diag.Code, format string, args ...any) {
	p.diags = append(p.diags, diag.New(code, tok.Span, format, args...))
}

// errorHelpAt records a syntax diagnostic carrying a fix suggestion.
func (p *parser) errorHelpAt(tok token.Token, code diag.Code, help, format string, args ...any) {
	p.diags = append(p.diags, diag.New(code, tok.Span, format, args...).WithHelp(help))
}

// errorHelpSpan records a syntax diagnostic at an explicit source span.
func (p *parser) errorHelpSpan(sp source.Span, code diag.Code, help, format string, args ...any) {
	p.diags = append(p.diags, diag.New(code, sp, format, args...).WithHelp(help))
}

// expected reports that the current token should have been kind and leaves
// the position unchanged.
func (p *parser) expected(kind token.Kind) {
	p.errorHelpAt(p.tok, diag.CodeExpectedToken,
		"write "+kind.String()+" here",
		"expected %s, found %s", kind, describe(p.tok))
}

// expect consumes the current token when it has kind and reports the
// mismatch otherwise.
func (p *parser) expect(kind token.Kind) bool {
	if p.tok.Kind != kind {
		p.expected(kind)
		return false
	}
	p.advance()
	return true
}

// expectGt consumes a closing type argument `>`. A `>>` token closes two
// nested argument lists, so it is split into two closers.
func (p *parser) expectGt() {
	if p.gtCount > 0 {
		p.gtCount--
		return
	}
	switch p.tok.Kind {
	case token.Gt:
		p.advance()
	case token.Shr:
		p.gtCount = 1
		p.advance()
	default:
		p.expected(token.Gt)
	}
}

// sync skips through the end of the current statement so parsing can resume
// at the next one.
func (p *parser) sync() {
	for p.tok.Kind != token.EOF && p.tok.Kind != token.Newline &&
		p.tok.Kind != token.Semicolon && p.tok.Kind != token.RBrace {
		p.advance()
	}
	if p.tok.Kind == token.Newline || p.tok.Kind == token.Semicolon {
		p.advance()
	}
}

// describe names a token in a diagnostic.
func describe(tok token.Token) string {
	switch tok.Kind {
	case token.EOF:
		return "end of file"
	case token.Newline:
		return "end of line"
	case token.Ident:
		return "identifier " + strconv.Quote(tok.Text())
	}
	return strconv.Quote(tok.Text())
}

// name consumes an identifier token and returns it as a node. A token that
// is not an identifier is reported and consumed so parsing can continue.
func (p *parser) name() *ast.Ident {
	id := &ast.Ident{Sp: p.tok.Span, Name: p.tok.Text()}
	if p.tok.Kind != token.Ident {
		p.expected(token.Ident)
	}
	p.advance()
	return id
}

// binding consumes a binding name; `_` is accepted as a discard.
func (p *parser) binding() *ast.Ident {
	id := &ast.Ident{Sp: p.tok.Span, Name: p.tok.Text()}
	if p.tok.Kind != token.Ident && p.tok.Kind != token.Underscore {
		p.expected(token.Ident)
	}
	p.advance()
	return id
}
