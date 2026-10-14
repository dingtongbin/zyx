// Inline C blocks: `c { ... }` is captured as one token, never tokenized as
// zyx, because its contents are C and may hold constructs the zyx lexer
// rejects, such as `?`, `#`, `2166136261u`, or `/* ... */`.

package lexer

import (
	"zyx/internal/diag"
	"zyx/internal/token"
)

// startsCBlock reports whether the `c` keyword ending at from opens an
// inline C block. Horizontal space may separate the keyword from the brace,
// but the brace must sit on the same line.
func (l *lexer) startsCBlock(from int) bool {
	i := from
	for i < len(l.src) && (l.src[i] == ' ' || l.src[i] == '\t') {
		i++
	}
	return i < len(l.src) && l.src[i] == '{'
}

// scanCBlock scans `c { ... }` as a single token whose span covers the whole
// construct. Braces are balanced, and C strings, character literals, and
// comments are skipped so their contents cannot close the block early.
// l.pos starts just past the `c` keyword.
func (l *lexer) scanCBlock(start int) {
	for l.pos < len(l.src) && (l.src[l.pos] == ' ' || l.src[l.pos] == '\t') {
		l.pos++
	}
	l.pos++ // consume the opening brace
	depth := 1
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '"':
			l.skipCLiteral('"')
		case c == '\'':
			l.skipCLiteral('\'')
		case c == '/' && l.hasByteAt(l.pos+1, '/'):
			for l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
				l.pos++
			}
		case c == '/' && l.hasByteAt(l.pos+1, '*'):
			l.skipCComment()
		case c == '{':
			depth++
			l.pos++
		case c == '}':
			depth--
			l.pos++
			if depth == 0 {
				l.emitSig(token.Token{Kind: token.CBlock, Span: l.span(start, l.pos)})
				return
			}
		default:
			l.pos++
		}
	}
	l.reportHelp(start, l.pos, diag.CodeUnterminatedCBlock,
		"close the inline C block with '}'",
		"unterminated inline C block")
}

// skipCLiteral advances past a C string or character literal whose opening
// quote sits at l.pos, honoring backslash escapes.
func (l *lexer) skipCLiteral(quote byte) {
	l.pos++
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\\' {
			l.pos += 2
			continue
		}
		if c == quote {
			l.pos++
			return
		}
		if c == '\n' || c == '\r' {
			return
		}
		l.pos++
	}
}

// skipCComment advances past a C block comment that opens with "/*" at
// l.pos. An unterminated comment consumes the rest of the file; the
// enclosing scan then reports the missing closing brace.
func (l *lexer) skipCComment() {
	l.pos += 2
	for l.pos < len(l.src) {
		if l.src[l.pos] == '*' && l.hasByteAt(l.pos+1, '/') {
			l.pos += 2
			return
		}
		l.pos++
	}
}
