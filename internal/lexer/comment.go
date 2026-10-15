// Line comments.

package lexer

import "zyx/internal/token"

// scanComment scans a line comment starting at "//". Three or more slashes
// open a doc comment, which attaches to the following declaration.
func (l *lexer) scanComment() {
	start := l.pos
	l.pos += 2
	doc := false
	if l.pos < len(l.src) && l.src[l.pos] == '/' {
		doc = true
		l.pos++
	}
	for l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
		l.pos++
	}
	l.emit(token.Token{Kind: token.Comment, Span: l.span(start, l.pos), Doc: doc})
}
