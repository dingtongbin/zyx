package lexer

import (
	"testing"

	"zyx/internal/diag"
	"zyx/internal/token"
)

func TestStringAndCharValues(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		kind  token.Kind
		value string
	}{
		{"string", `"hello"`, token.Str, "hello"},
		{"string newline escape", `"a\nb"`, token.Str, "a\nb"},
		// UTF-8 handling test data written as Go escapes to keep this file ASCII.
		{"string unicode text", "\"\u4E2D\"", token.Str, "\u4E2D"},
		{"string unicode escape", `"\u{4E2D}"`, token.Str, "\u4E2D"},
		{"string null escape", `"\0"`, token.Str, "\x00"},
		{"empty string", `""`, token.Str, ""},
		{"char", `'a'`, token.Char, "a"},
		{"char escape", `'\n'`, token.Char, "\n"},
		{"char quote escape", `'\''`, token.Char, "'"},
		{"char unicode text", "'\u4E2D'", token.Char, "\u4E2D"},
		{"char unicode escape", `'\u{4E2D}'`, token.Char, "\u4E2D"},
		{"byte string", `b"raw bytes"`, token.ByteStr, "raw bytes"},
		{"byte hex escape", `b"a\x41"`, token.ByteStr, "aA"},
		{"byte string unicode text", "b\"\u4E2D\"", token.ByteStr, "\u4E2D"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			toks, diags := lexSrc(t, c.src+"\n")
			assertClean(t, diags)
			if len(toks) == 0 || toks[0].Kind != c.kind {
				t.Fatalf("first token = %v, want %v", toks, c.kind)
			}
			if got := toks[0].Str; got != c.value {
				t.Fatalf("value = %q, want %q", got, c.value)
			}
		})
	}
}

func TestLiteralErrors(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		wantCodes []diag.Code
	}{
		{"empty char", `''`, []diag.Code{diag.CodeInvalidCharLiteral}},
		{"two chars", `'ab'`, []diag.Code{diag.CodeInvalidCharLiteral}},
		{"unknown escape", `"\q"`, []diag.Code{diag.CodeInvalidEscape}},
		{"hex escape outside bytes", `"\x41"`, []diag.Code{diag.CodeInvalidEscape}},
		{"unicode escape in bytes", `b"\u{41}"`, []diag.Code{diag.CodeInvalidEscape}},
		{"short hex escape", `b"\x4"`, []diag.Code{diag.CodeInvalidEscape}},
		{"surrogate escape", `"\u{D800}"`, []diag.Code{diag.CodeInvalidEscape}},
		{"escape past max", `"\u{110000}"`, []diag.Code{diag.CodeInvalidEscape}},
		{"empty escape braces", `"\u{}"`, []diag.Code{diag.CodeInvalidEscape}},
		{"missing brace", `"\u41"`, []diag.Code{diag.CodeInvalidEscape}},
		{"unterminated string", `"abc`, []diag.Code{diag.CodeUnterminatedLiteral}},
		{"unterminated char", `'a`, []diag.Code{diag.CodeUnterminatedLiteral}},
		{"unterminated byte string", `b"abc`, []diag.Code{diag.CodeUnterminatedLiteral}},
		{"char literal runs past line end", `'\n`, []diag.Code{diag.CodeUnterminatedLiteral}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, diags := lexSrc(t, c.src+"\n")
			assertCodes(t, diags, c.wantCodes...)
		})
	}
}

func TestUnicodeEscapeBounds(t *testing.T) {
	cases := []struct {
		name string
		src  string
		ok   bool
	}{
		{"single hex digit", `'\u{7}'`, true},
		{"six hex digits", `'\u{10FFFF}'`, true},
		{"seven digits", `'\u{1234567}'`, false},
		{"no digits", `'\u{}'`, false},
		{"lower surrogate", `'\u{DC00}'`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, diags := lexSrc(t, c.src+"\n")
			if c.ok {
				assertClean(t, diags)
			} else {
				assertCodes(t, diags, diag.CodeInvalidEscape)
			}
		})
	}
}
