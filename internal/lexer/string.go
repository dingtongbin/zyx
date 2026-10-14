// Character, string, and byte string literals.

package lexer

import (
	"strings"
	"unicode/utf8"

	"zyx/internal/diag"
	"zyx/internal/token"
)

// escapeClass selects the escape set of a literal.
type escapeClass uint8

// Literal escape classes.
const (
	// escapeCharStr is the set shared by char and str literals.
	escapeCharStr escapeClass = iota
	// escapeBytes is the byte string set: no \u, but \xNN.
	escapeBytes
)

// scanChar scans a character literal.
func (l *lexer) scanChar() {
	start := l.pos
	l.pos++
	if l.atLineEnd() {
		l.reportHelp(start, l.pos, diag.CodeUnterminatedLiteral,
			"close the character with a single quote",
			"unterminated character literal")
		l.emitSig(token.Token{Kind: token.Char, Span: l.span(start, l.pos)})
		return
	}
	if l.src[l.pos] == '\'' {
		l.pos++
		l.reportHelp(start, l.pos, diag.CodeInvalidCharLiteral,
			"write a single character between the quotes",
			"empty character literal")
		l.emitSig(token.Token{Kind: token.Char, Span: l.span(start, l.pos)})
		return
	}
	value := ""
	if l.src[l.pos] == '\\' {
		value = l.decodeEscape(escapeCharStr)
	} else {
		_, size := utf8.DecodeRune(l.src[l.pos:])
		value = string(l.src[l.pos : l.pos+size])
		l.pos += size
	}
	switch {
	case l.hasByteAt(l.pos, '\''):
		l.pos++
	case l.atLineEnd():
		l.reportHelp(start, l.pos, diag.CodeUnterminatedLiteral,
			"close the character with a single quote",
			"unterminated character literal")
	default:
		for l.pos < len(l.src) && !l.hasByteAt(l.pos, '\'') && !l.atLineEnd() {
			l.pos++
		}
		if l.hasByteAt(l.pos, '\'') {
			l.pos++
		}
		// A failed escape already reported; extra characters only matter
		// when the literal really held a character before them.
		if value != "" {
			l.reportHelp(start, l.pos, diag.CodeInvalidCharLiteral,
				"use a string literal for more than one character",
				"character literal must contain exactly one character")
		}
	}
	l.emitSig(token.Token{Kind: token.Char, Span: l.span(start, l.pos), Str: value})
}

// scanString scans a string literal; Token.Str holds the decoded text.
func (l *lexer) scanString() {
	start := l.pos
	l.pos++
	var b strings.Builder
	for {
		if l.atLineEnd() {
			l.reportHelp(start, l.pos, diag.CodeUnterminatedLiteral,
				"close the string with a double quote",
				"unterminated string literal")
			l.emitSig(token.Token{Kind: token.Str, Span: l.span(start, l.pos), Str: b.String()})
			return
		}
		c := l.src[l.pos]
		switch {
		case c == '"':
			l.pos++
			l.emitSig(token.Token{Kind: token.Str, Span: l.span(start, l.pos), Str: b.String()})
			return
		case c == '\\':
			b.WriteString(l.decodeEscape(escapeCharStr))
		case c >= utf8.RuneSelf:
			_, size := utf8.DecodeRune(l.src[l.pos:])
			b.Write(l.src[l.pos : l.pos+size])
			l.pos += size
		default:
			b.WriteByte(c)
			l.pos++
		}
	}
}

// scanByteString scans a b"..." literal; Token.Str holds the raw bytes.
func (l *lexer) scanByteString() {
	start := l.pos
	l.pos += 2
	var b strings.Builder
	for {
		if l.atLineEnd() {
			l.reportHelp(start, l.pos, diag.CodeUnterminatedLiteral,
				"close the byte string with a double quote",
				"unterminated byte string literal")
			l.emitSig(token.Token{Kind: token.ByteStr, Span: l.span(start, l.pos), Str: b.String()})
			return
		}
		c := l.src[l.pos]
		switch {
		case c == '"':
			l.pos++
			l.emitSig(token.Token{Kind: token.ByteStr, Span: l.span(start, l.pos), Str: b.String()})
			return
		case c == '\\':
			b.WriteString(l.decodeEscape(escapeBytes))
		default:
			b.WriteByte(c)
			l.pos++
		}
	}
}

// atLineEnd reports whether the scan position sits at a line break or at
// the end of the file, where a literal cannot continue.
func (l *lexer) atLineEnd() bool {
	if l.pos >= len(l.src) {
		return true
	}
	return l.src[l.pos] == '\n' || l.src[l.pos] == '\r'
}

// decodeEscape decodes the escape sequence whose backslash sits at l.pos
// and advances past it. Invalid sequences report a diagnostic and decode
// to nothing so scanning continues at the closing quote.
func (l *lexer) decodeEscape(class escapeClass) string {
	back := l.pos
	l.pos++
	if l.pos >= len(l.src) {
		l.report(back, l.pos, diag.CodeInvalidEscape, "unterminated escape sequence")
		return ""
	}
	c := l.src[l.pos]
	l.pos++
	switch c {
	case 'n':
		return "\n"
	case 'r':
		return "\r"
	case 't':
		return "\t"
	case '\\':
		return "\\"
	case '"':
		return "\""
	case '0':
		return "\x00"
	case '\'':
		if class == escapeBytes {
			l.report(back, l.pos, diag.CodeInvalidEscape,
				"byte string literals cannot use the \\' escape")
			return ""
		}
		return "'"
	case 'x':
		if class == escapeCharStr {
			l.reportHelp(back, l.pos, diag.CodeInvalidEscape,
				"\\xNN is spelled with two hex digits inside b\"...\"",
				"\\x escapes are only valid in byte string literals")
			return ""
		}
		return l.decodeHexByte(back)
	case 'u':
		if class == escapeBytes {
			l.reportHelp(back, l.pos, diag.CodeInvalidEscape,
				"write non-ASCII bytes as \\xNN",
				"byte string literals cannot use \\u escapes")
			return ""
		}
		return l.decodeUnicodeEscape(back)
	}
	l.report(back, l.pos, diag.CodeInvalidEscape,
		"unknown escape sequence %q", "\\"+string(rune(c)))
	return ""
}

// decodeHexByte decodes the two digits of a \xNN escape.
func (l *lexer) decodeHexByte(back int) string {
	if l.pos+1 >= len(l.src) || !isHexDigit(l.src[l.pos]) || !isHexDigit(l.src[l.pos+1]) {
		l.report(back, l.pos, diag.CodeInvalidEscape,
			"\\x escape needs exactly two hexadecimal digits")
		return ""
	}
	v := hexVal(l.src[l.pos])<<4 | hexVal(l.src[l.pos+1])
	l.pos += 2
	return string([]byte{byte(v)})
}

// decodeUnicodeEscape decodes the scalar value of a \u{...} escape.
func (l *lexer) decodeUnicodeEscape(back int) string {
	if !l.hasByteAt(l.pos, '{') {
		l.report(back, l.pos, diag.CodeInvalidEscape, "expected '{' after \\u")
		return ""
	}
	l.pos++
	v, digits := 0, 0
	for l.pos < len(l.src) && isHexDigit(l.src[l.pos]) {
		v = v*16 + hexVal(l.src[l.pos])
		digits++
		l.pos++
	}
	valid := digits >= 1 && digits <= 6
	if l.hasByteAt(l.pos, '}') {
		l.pos++
	} else {
		valid = false
	}
	if !valid {
		l.report(back, l.pos, diag.CodeInvalidEscape,
			"\\u escape needs one to six hexadecimal digits followed by '}'")
		return ""
	}
	if v > 0x10FFFF || v >= 0xD800 && v <= 0xDFFF {
		l.report(back, l.pos, diag.CodeInvalidEscape,
			"\\u escape is not a Unicode scalar value")
		return ""
	}
	return string(rune(v))
}

// skipStringRaw advances past a string literal without decoding it. It
// stops at the closing quote, the end of the line, or the end of the file.
func (l *lexer) skipStringRaw() {
	l.pos++
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '"':
			l.pos++
			return
		case c == '\\':
			if l.pos+1 >= len(l.src) {
				l.pos = len(l.src)
				return
			}
			if l.src[l.pos+1] == '\n' || l.src[l.pos+1] == '\r' {
				l.pos++
				return
			}
			l.pos += 2
		case c == '\n' || c == '\r':
			return
		default:
			l.pos++
		}
	}
}

// skipCharRaw advances past a character literal without decoding it.
func (l *lexer) skipCharRaw() {
	l.pos++
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\'':
			l.pos++
			return
		case c == '\\':
			if l.pos+1 >= len(l.src) {
				l.pos = len(l.src)
				return
			}
			if l.src[l.pos+1] == '\n' || l.src[l.pos+1] == '\r' {
				l.pos++
				return
			}
			l.pos += 2
		case c == '\n' || c == '\r':
			return
		default:
			l.pos++
		}
	}
}

// isHexDigit reports whether c is a hexadecimal digit.
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c|0x20 >= 'a' && c|0x20 <= 'f')
}

// hexVal returns the value of the hexadecimal digit c.
func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	default:
		return int((c|0x20)-'a') + 10
	}
}
