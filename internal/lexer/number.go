// Numeric literals: integers, radix prefixes, floats, and type suffixes.

package lexer

import (
	"zyx/internal/diag"
	"zyx/internal/token"
)

// numericSuffixes maps the valid literal suffixes to their kinds.
var numericSuffixes = map[string]token.NumSuffix{
	"i8":    token.SuffixI8,
	"i16":   token.SuffixI16,
	"i32":   token.SuffixI32,
	"i64":   token.SuffixI64,
	"i128":  token.SuffixI128,
	"u8":    token.SuffixU8,
	"u16":   token.SuffixU16,
	"u32":   token.SuffixU32,
	"u64":   token.SuffixU64,
	"u128":  token.SuffixU128,
	"usize": token.SuffixUsize,
	"f32":   token.SuffixF32,
	"f64":   token.SuffixF64,
}

// scanNumber scans a decimal, hexadecimal, octal, or binary literal.
func (l *lexer) scanNumber() {
	start := l.pos
	if l.src[start] == '0' && start+1 < len(l.src) {
		switch l.src[start+1] | 0x20 {
		case 'x', 'o', 'b':
			l.scanRadix(start)
			return
		}
	}
	l.scanDecimal(start)
}

// scanRadix scans a 0x, 0o, or 0b prefixed integer literal.
func (l *lexer) scanRadix(start int) {
	radix, name := 16, "hexadecimal"
	switch l.src[start+1] | 0x20 {
	case 'o':
		radix, name = 8, "octal"
	case 'b':
		radix, name = 2, "binary"
	}
	l.pos = start + 2
	l.scanDigitRun(l.pos, radix)
	if l.pos == start+2 {
		l.reportHelp(start, l.pos, diag.CodeInvalidNumber,
			"write at least one digit after the prefix",
			"%s literal has no digits", name)
		l.emitSig(token.Token{Kind: token.Int, Span: l.span(start, l.pos)})
		return
	}
	l.finishNumber(start, token.Int, false)
}

// scanDecimal scans an integer or floating-point literal.
func (l *lexer) scanDecimal(start int) {
	isFloat := false
	l.scanDigitRun(l.pos, 10)
	if l.hasByteAt(l.pos, '.') && l.pos+1 < len(l.src) && isDecDigit(l.src[l.pos+1]) {
		isFloat = true
		l.pos++
		l.scanDigitRun(l.pos, 10)
	}
	if l.pos < len(l.src) && l.src[l.pos]|0x20 == 'e' {
		j := l.pos + 1
		if j < len(l.src) && (l.src[j] == '+' || l.src[j] == '-') {
			j++
		}
		if j < len(l.src) && isDecDigit(l.src[j]) {
			isFloat = true
			l.pos++
			if l.src[l.pos] == '+' || l.src[l.pos] == '-' {
				l.pos++
			}
			l.scanDigitRun(l.pos, 10)
		}
	}
	kind := token.Int
	if isFloat {
		kind = token.Float
	}
	l.finishNumber(start, kind, isFloat)
}

// scanDigitRun consumes digits of the radix. Underscores must sit between
// digits: leading, trailing, and repeated separators are errors.
func (l *lexer) scanDigitRun(start, radix int) {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if isRadixDigit(c, radix) {
			l.pos++
			continue
		}
		if c != '_' {
			return
		}
		uStart := l.pos
		bad := l.pos == start ||
			!isRadixDigit(l.src[l.pos-1], radix) ||
			l.pos+1 >= len(l.src) ||
			!isRadixDigit(l.src[l.pos+1], radix)
		for l.pos < len(l.src) && l.src[l.pos] == '_' {
			l.pos++
		}
		if bad {
			l.report(uStart, l.pos, diag.CodeInvalidNumber,
				"underscores in a numeric literal must be between digits")
		}
	}
}

// finishNumber scans the optional type suffix, validates it against the
// literal class, and emits the literal token.
func (l *lexer) finishNumber(start int, kind token.Kind, isFloat bool) {
	suffix := token.SuffixNone
	if sufStart := l.pos; l.pos < len(l.src) && isIdentChar(l.src[l.pos]) {
		for l.pos < len(l.src) && isIdentChar(l.src[l.pos]) {
			l.pos++
		}
		suf := string(l.src[sufStart:l.pos])
		s, known := numericSuffixes[suf]
		switch {
		case !known:
			l.report(sufStart, l.pos, diag.CodeInvalidNumber,
				"invalid literal suffix %q", suf)
		case isFloat && !s.IsFloat():
			l.report(sufStart, l.pos, diag.CodeInvalidNumber,
				"floating-point literal cannot use suffix %q", suf)
		case !isFloat && !s.IsInt():
			l.report(sufStart, l.pos, diag.CodeInvalidNumber,
				"integer literal cannot use suffix %q", suf)
		default:
			suffix = s
		}
	}
	l.emitSig(token.Token{Kind: kind, Span: l.span(start, l.pos), Suffix: suffix})
}

// isRadixDigit reports whether c is a digit of the given radix.
func isRadixDigit(c byte, radix int) bool {
	switch radix {
	case 2:
		return c == '0' || c == '1'
	case 8:
		return c >= '0' && c <= '7'
	case 16:
		return (c >= '0' && c <= '9') || (c|0x20 >= 'a' && c|0x20 <= 'f')
	default:
		return isDecDigit(c)
	}
}

// isDecDigit reports whether c is a decimal digit.
func isDecDigit(c byte) bool { return c >= '0' && c <= '9' }
