// Lexer error reporting helpers.

package lexer

import (
	"unicode/utf8"

	"zyx/internal/diag"
)

// report records a diagnostic spanning the source range [start, end).
func (l *lexer) report(start, end int, code diag.Code, format string, args ...any) {
	l.diags = append(l.diags, diag.New(code, l.span(start, end), format, args...))
}

// reportHelp records a diagnostic carrying an actionable fix suggestion.
func (l *lexer) reportHelp(start, end int, code diag.Code, help, format string, args ...any) {
	l.diags = append(l.diags, diag.New(code, l.span(start, end), format, args...).WithHelp(help))
}

// badUTF8 returns the offset of the first invalid UTF-8 sequence in src.
func badUTF8(src []byte) (int, bool) {
	for i := 0; i < len(src); {
		if src[i] < utf8.RuneSelf {
			i++
			continue
		}
		r, size := utf8.DecodeRune(src[i:])
		if r == utf8.RuneError && size == 1 {
			return i, false
		}
		i += size
	}
	return 0, true
}
