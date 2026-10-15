// Interpolated string literals.

package lexer

import (
	"strings"
	"unicode/utf8"

	"zyx/internal/diag"
	"zyx/internal/source"
	"zyx/internal/token"
)

// scanFString scans an f-string literal whose prefix starts at l.pos. The
// literal becomes decoded text segments plus interpolation spans that the
// parser re-lexes as expressions.
func (l *lexer) scanFString() {
	start := l.pos
	l.pos += 2
	parts := make([]token.FStrPart, 0, 2)
	var text strings.Builder
	for {
		if l.atLineEnd() {
			l.reportHelp(start, l.pos, diag.CodeUnterminatedLiteral,
				"close the f-string with a double quote",
				"unterminated f-string literal")
			l.emitFString(start, parts, text)
			return
		}
		c := l.src[l.pos]
		switch {
		case c == '"':
			l.pos++
			l.emitFString(start, parts, text)
			return
		case c == '{':
			if l.hasByteAt(l.pos+1, '{') {
				text.WriteByte('{')
				l.pos += 2
				continue
			}
			if text.Len() > 0 {
				parts = append(parts, token.FStrPart{Kind: token.FStrText, Text: text.String()})
				text.Reset()
			}
			expr, closed := l.scanInterpolation()
			parts = append(parts, token.FStrPart{Kind: token.FStrExpr, Span: expr})
			if !closed {
				l.emitFString(start, parts, text)
				return
			}
		case c == '}':
			if l.hasByteAt(l.pos+1, '}') {
				text.WriteByte('}')
				l.pos += 2
				continue
			}
			l.reportHelp(l.pos, l.pos+1, diag.CodeInvalidEscape,
				"write '}}' for a literal brace", "unmatched '}' in f-string literal")
			l.pos++
		case c == '\\':
			text.WriteString(l.decodeEscape(escapeCharStr))
		case c >= utf8.RuneSelf:
			_, size := utf8.DecodeRune(l.src[l.pos:])
			text.Write(l.src[l.pos : l.pos+size])
			l.pos += size
		default:
			text.WriteByte(c)
			l.pos++
		}
	}
}

// emitFString appends the f-string literal built so far, with any trailing
// text segment. A literal left open still yields one token, so the parser
// sees a complete token stream after the lexer reports the error.
func (l *lexer) emitFString(start int, parts []token.FStrPart, text strings.Builder) {
	if text.Len() > 0 {
		parts = append(parts, token.FStrPart{Kind: token.FStrText, Text: text.String()})
	}
	l.emitSig(token.Token{Kind: token.FString, Span: l.span(start, l.pos), Parts: parts})
}

// scanInterpolation scans the "{expr}" that starts at the opening brace in
// l.pos and returns the span of the expression source. closed reports
// whether the matching brace was found before the line or file ended.
func (l *lexer) scanInterpolation() (source.Span, bool) {
	open := l.pos
	l.pos++
	exprStart := l.pos
	depth := 0
	for {
		if l.atLineEnd() {
			l.report(open, l.pos, diag.CodeUnterminatedLiteral,
				"unterminated interpolation in f-string literal")
			return l.span(exprStart, l.pos), false
		}
		c := l.src[l.pos]
		switch {
		case c == '"':
			l.skipStringRaw()
		case c == '\'':
			l.skipCharRaw()
		case c == '/' && l.hasByteAt(l.pos+1, '/'):
			// A comment runs to the line end, so the closing brace can
			// never follow it; the line-end check reports the literal.
			for l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
				l.pos++
			}
		case c == 'f' && l.hasByteAt(l.pos+1, '"'):
			l.reportHelp(l.pos, l.pos+2, diag.CodeNestedFString,
				"move the nested f-string outside the interpolation",
				"f-strings cannot be nested")
			l.pos += 2
		case c == '{':
			depth++
			l.pos++
		case c == '}':
			if depth > 0 {
				depth--
				l.pos++
				continue
			}
			end := l.pos
			l.pos++
			span := l.span(exprStart, end)
			if l.isBlankInterp(span) {
				l.reportHelp(open, l.pos, diag.CodeEmptyInterpolation,
					"write an expression between the braces",
					"f-string interpolation is empty")
			}
			return span, true
		case c >= utf8.RuneSelf:
			r, size := utf8.DecodeRune(l.src[l.pos:])
			l.reportHelp(l.pos, l.pos+size, diag.CodeNonASCIISource,
				"write non-ASCII text as a \\u{...} escape inside a string literal",
				"non-ASCII character %q outside a comment or literal", r)
			l.pos += size
		default:
			l.pos++
		}
	}
}

// isBlankInterp reports whether the interpolation source holds no
// expression, ignoring horizontal space.
func (l *lexer) isBlankInterp(span source.Span) bool {
	for _, c := range l.src[span.Start:span.End] {
		if c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}
