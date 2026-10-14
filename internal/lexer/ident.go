// Identifiers, keywords, and non-ASCII characters in code position.

package lexer

import (
	"unicode/utf8"

	"zyx/internal/diag"
	"zyx/internal/token"
)

// isIdentStart reports whether c can start an identifier.
func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isIdentChar reports whether c can appear inside an identifier.
func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// scanIdent scans an identifier, a keyword, or the f-string and byte
// string literals whose single-letter prefix is recognized here.
func (l *lexer) scanIdent() {
	start := l.pos
	if c := l.src[start]; (c == 'f' || c == 'b') && l.hasByteAt(start+1, '"') {
		if c == 'f' {
			l.scanFString()
		} else {
			l.scanByteString()
		}
		return
	}
	l.pos++
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if isIdentChar(c) {
			l.pos++
			continue
		}
		// A hyphen joins only between identifier characters with no space
		// around it: a-b names one identifier, a - b and a- b subtract.
		if c == '-' && l.pos+1 < len(l.src) && isIdentChar(l.src[l.pos+1]) {
			l.pos += 2
			continue
		}
		break
	}
	kind := token.Ident
	if text := string(l.src[start:l.pos]); text == "_" {
		kind = token.Underscore
	} else if kw, ok := token.LookupKeyword(text); ok {
		kind = kw
	}
	if kind == token.C && l.startsCBlock(l.pos) {
		l.scanCBlock(start)
		return
	}
	l.emitSig(token.Token{Kind: kind, Span: l.span(start, l.pos)})
}

// scanNonASCII skips and reports a non-ASCII character in code position.
// Comments and literals divert to their own scanners before this point.
func (l *lexer) scanNonASCII() {
	start := l.pos
	r, size := utf8.DecodeRune(l.src[l.pos:])
	l.pos += size
	l.reportHelp(start, l.pos, diag.CodeNonASCIISource,
		"write non-ASCII text as a \\u{...} escape inside a string literal",
		"non-ASCII character %q outside a comment or literal", r)
}
