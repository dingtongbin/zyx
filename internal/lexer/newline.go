// Line termination, continuation tokens, and bracket depth.

package lexer

import "zyx/internal/token"

// isContinuation reports whether a statement continues past a line whose
// last token is k.
func isContinuation(k token.Kind) bool {
	switch k {
	case token.Plus, token.Minus, token.Star, token.Slash, token.Percent,
		token.Shl, token.Shr, token.Amp, token.Caret, token.Pipe,
		token.Eq, token.Ne, token.Lt, token.Le, token.Gt, token.Ge,
		token.AndAnd, token.OrOr, token.Range,
		token.Assign, token.AddAssign, token.SubAssign, token.MulAssign,
		token.DivAssign, token.ModAssign, token.AndAssign, token.OrAssign,
		token.XorAssign, token.ShlAssign, token.ShrAssign,
		token.FatArrow, token.Comma, token.Dot, token.Colon:
		return true
	}
	return false
}

// lineEnd ends the line spanning [start, end): inside brackets the break
// has no effect, a continuation token carries the statement over, and any
// other line-ending token terminates it.
func (l *lexer) lineEnd(start, end int) {
	last, hasSig := l.lastSig, l.hasSig
	l.hasSig = false
	if l.depth > 0 || !hasSig || isContinuation(last) {
		return
	}
	l.emitNewline(start, end)
}

// semicolon emits a statement separator. A `;` is a token everywhere,
// because the array type `[T; N]` needs to see it; the parser reads it as
// equivalent to a line break. The separator ends the line, so no line break
// token follows it.
func (l *lexer) semicolon(start, end int) {
	l.emit(token.Token{Kind: token.Semicolon, Span: l.span(start, end)})
	l.hasSig = false
}

// emitNewline appends a Newline token unless the stream already ends in
// one, so blank lines and repeated semicolons collapse.
func (l *lexer) emitNewline(start, end int) {
	if l.lastEmitted == token.Newline {
		return
	}
	l.emit(token.Token{Kind: token.Newline, Span: l.span(start, end)})
}

// openBrace records an opening brace at pos. A brace at the end of its line
// opens a block, and blocks do not count toward bracket depth.
func (l *lexer) openBrace(pos int) {
	counts := !l.lineEndsAfter(pos)
	l.braces = append(l.braces, counts)
	if counts {
		l.depth++
	}
}

// closeBrace pops the innermost open brace and updates the depth. An
// unmatched close leaves the depth alone; the parser reports it.
func (l *lexer) closeBrace() {
	if n := len(l.braces); n > 0 {
		if l.braces[n-1] {
			l.depth--
		}
		l.braces = l.braces[:n-1]
	}
}

// lineEndsAfter reports whether only horizontal space, an optional line
// comment, and the line terminator follow the position pos.
func (l *lexer) lineEndsAfter(pos int) bool {
	i := pos + 1
	for i < len(l.src) && (l.src[i] == ' ' || l.src[i] == '\t') {
		i++
	}
	if i+1 < len(l.src) && l.src[i] == '/' && l.src[i+1] == '/' {
		for i < len(l.src) && l.src[i] != '\n' && l.src[i] != '\r' {
			i++
		}
	}
	if i >= len(l.src) {
		return true
	}
	return l.src[i] == '\n' || l.src[i] == '\r'
}
