// Line and block comments.

package lexer

import (
	"zyx/internal/diag"
	"zyx/internal/token"
)

// blockCommentEnd returns the offset just past the block comment that opens
// at i, and whether it closes. A block comment nests, so one that holds
// another ends where the outer one closes.
func blockCommentEnd(src []byte, i int) (int, bool) {
	depth := 0
	for i < len(src) {
		switch {
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			depth++
			i += 2
		case src[i] == '*' && i+1 < len(src) && src[i+1] == '/':
			depth--
			i += 2
			if depth == 0 {
				return i, true
			}
		default:
			i++
		}
	}
	return i, false
}

// scanBlockComment scans a block comment starting at "/*".
func (l *lexer) scanBlockComment() {
	start := l.pos
	end, closed := blockCommentEnd(l.src, l.pos)
	l.pos = end
	if !closed {
		l.reportHelp(start, l.pos, diag.CodeUnexpectedToken,
			"close the comment with */",
			"a block comment is not closed")
	}
	l.emitSig(token.Token{Kind: token.Comment, Span: l.span(start, l.pos), Block: true})
}

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
