// Package lexer tokenizes zyx source files.
package lexer

import (
	"zyx/internal/diag"
	"zyx/internal/source"
	"zyx/internal/token"
)

// Lex scans file and returns its token stream and diagnostics in source
// order. The stream always ends with an EOF token. Invalid UTF-8 is fatal:
// Lex then reports one diagnostic and returns no tokens.
func Lex(file *source.File) ([]token.Token, []diag.Diagnostic) {
	l := &lexer{file: file, src: file.Data()}
	if off, ok := badUTF8(l.src); !ok {
		l.report(off, off+1, diag.CodeInvalidUTF8, "source file is not valid UTF-8")
		return nil, l.diags
	}
	l.run()
	return l.tokens, l.diags
}

// LexExpr lexes the byte range [start, end) of file as the token stream of
// an embedded expression, such as an f-string interpolation. Spans stay
// absolute, so diagnostics keep their original positions. The caller is
// responsible for having validated the whole file as UTF-8.
func LexExpr(file *source.File, start, end int) ([]token.Token, []diag.Diagnostic) {
	l := &lexer{file: file, src: file.Data()[:end], pos: start}
	l.run()
	return l.tokens, l.diags
}

// lexer holds the scanner state for one file.
type lexer struct {
	file   *source.File
	src    []byte
	pos    int
	tokens []token.Token
	diags  []diag.Diagnostic

	depth       int        // unclosed (, [, and counted {
	braces      []bool     // one entry per open {; true when it counts
	hasSig      bool       // a significant token was seen on this line
	lastSig     token.Kind // kind of that token
	lastEmitted token.Kind // kind of the last appended token
}

// run scans the file from front to back.
func (l *lexer) run() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t':
			l.pos++
		case c == '\n':
			l.lineEnd(l.pos, l.pos+1)
			l.pos++
		case c == '\r':
			end := l.pos + 1
			if end < len(l.src) && l.src[end] == '\n' {
				end++
			}
			l.lineEnd(l.pos, end)
			l.pos = end
		case c == ';':
			l.semicolon(l.pos, l.pos+1)
			l.pos++
		case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/':
			l.scanComment()
		case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*':
			l.scanBlockComment()
		case c == '\'':
			l.scanChar()
		case c == '"':
			l.scanString()
		case c >= '0' && c <= '9':
			l.scanNumber()
		case isIdentStart(c):
			l.scanIdent()
		case c >= 0x80:
			l.scanNonASCII()
		default:
			l.scanOperator()
		}
	}
	l.finish()
}

// finish terminates a final line that lacks a break and appends EOF.
func (l *lexer) finish() {
	if l.hasSig && l.depth == 0 && !isContinuation(l.lastSig) {
		l.emitNewline(len(l.src), len(l.src))
	}
	l.emit(token.Token{Kind: token.EOF, Span: l.span(len(l.src), len(l.src))})
}

// span builds a source span for the current file.
func (l *lexer) span(start, end int) source.Span {
	return source.Span{File: l.file, Start: start, End: end}
}

// emit appends one token to the stream.
func (l *lexer) emit(t token.Token) {
	l.tokens = append(l.tokens, t)
	l.lastEmitted = t.Kind
}

// emitSig appends a statement-significant token and records it as the last
// token of the current line. A line comment is not significant, because it
// ends its own line; a block comment is, because the line goes on past it.
func (l *lexer) emitSig(t token.Token) {
	l.hasSig = true
	l.lastSig = t.Kind
	l.emit(t)
}

// hasByteAt reports whether the byte at offset i equals c.
func (l *lexer) hasByteAt(i int, c byte) bool {
	return i < len(l.src) && l.src[i] == c
}

// scanOperator scans a punctuator or reports an illegal byte.
func (l *lexer) scanOperator() {
	start := l.pos
	kind, size := token.Invalid, 1
	c := l.src[l.pos]
	switch c {
	case '@':
		l.reportHelp(start, start+1, diag.CodeUnexpectedChar,
			"zyx has no annotations or attributes", "unexpected character '@'")
		l.pos++
		return
	case ':':
		if l.hasByteAt(l.pos+1, ':') {
			l.reportHelp(start, start+2, diag.CodeDoubleColon,
				"use '.' for member access", "'::' is not valid in zyx")
			l.pos += 2
			return
		}
		kind = token.Colon
	case '+':
		kind = token.Plus
		if l.hasByteAt(l.pos+1, '=') {
			kind, size = token.AddAssign, 2
		}
	case '-':
		switch {
		case l.hasByteAt(l.pos+1, '>'):
			kind, size = token.Arrow, 2
		case l.hasByteAt(l.pos+1, '='):
			kind, size = token.SubAssign, 2
		default:
			kind = token.Minus
		}
	case '*':
		kind = token.Star
		if l.hasByteAt(l.pos+1, '=') {
			kind, size = token.MulAssign, 2
		}
	case '/':
		kind = token.Slash
		if l.hasByteAt(l.pos+1, '=') {
			kind, size = token.DivAssign, 2
		}
	case '%':
		kind = token.Percent
		if l.hasByteAt(l.pos+1, '=') {
			kind, size = token.ModAssign, 2
		}
	case '=':
		switch {
		case l.hasByteAt(l.pos+1, '>'):
			kind, size = token.FatArrow, 2
		case l.hasByteAt(l.pos+1, '='):
			kind, size = token.Eq, 2
		default:
			kind = token.Assign
		}
	case '!':
		if l.hasByteAt(l.pos+1, '=') {
			kind, size = token.Ne, 2
		} else {
			kind = token.Bang
		}
	case '<':
		switch {
		case l.hasByteAt(l.pos+1, '<') && l.hasByteAt(l.pos+2, '='):
			kind, size = token.ShlAssign, 3
		case l.hasByteAt(l.pos+1, '<'):
			kind, size = token.Shl, 2
		case l.hasByteAt(l.pos+1, '='):
			kind, size = token.Le, 2
		default:
			kind = token.Lt
		}
	case '>':
		switch {
		case l.hasByteAt(l.pos+1, '>') && l.hasByteAt(l.pos+2, '='):
			kind, size = token.ShrAssign, 3
		case l.hasByteAt(l.pos+1, '>'):
			kind, size = token.Shr, 2
		case l.hasByteAt(l.pos+1, '='):
			kind, size = token.Ge, 2
		default:
			kind = token.Gt
		}
	case '&':
		switch {
		case l.hasByteAt(l.pos+1, '&'):
			kind, size = token.AndAnd, 2
		case l.hasByteAt(l.pos+1, '='):
			kind, size = token.AndAssign, 2
		default:
			kind = token.Amp
		}
	case '|':
		switch {
		case l.hasByteAt(l.pos+1, '|'):
			kind, size = token.OrOr, 2
		case l.hasByteAt(l.pos+1, '='):
			kind, size = token.OrAssign, 2
		default:
			kind = token.Pipe
		}
	case '^':
		kind = token.Caret
		if l.hasByteAt(l.pos+1, '=') {
			kind, size = token.XorAssign, 2
		}
	case '~':
		kind = token.Tilde
	case '.':
		switch {
		case l.hasByteAt(l.pos+1, '.') && l.hasByteAt(l.pos+2, '.'):
			kind, size = token.Ellipsis, 3
		case l.hasByteAt(l.pos+1, '.'):
			kind, size = token.Range, 2
		default:
			kind = token.Dot
		}
	case ',':
		kind = token.Comma
	case '(':
		kind = token.LParen
		l.depth++
	case ')':
		kind = token.RParen
		if l.depth > 0 {
			l.depth--
		}
	case '[':
		kind = token.LBrack
		l.depth++
	case ']':
		kind = token.RBrack
		if l.depth > 0 {
			l.depth--
		}
	case '{':
		kind = token.LBrace
		l.openBrace(start)
	case '}':
		kind = token.RBrace
		l.closeBrace()
	default:
		l.reportHelp(start, start+1, diag.CodeUnexpectedChar,
			"remove the character", "unexpected character %q", rune(c))
		l.pos++
		return
	}
	l.pos = start + size
	l.emitSig(token.Token{Kind: kind, Span: l.span(start, l.pos)})
}
