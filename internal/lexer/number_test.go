package lexer

import (
	"testing"

	"zyx/internal/diag"
	"zyx/internal/token"
)

func TestNumericLiterals(t *testing.T) {
	cases := []struct {
		name       string
		src        string
		wantKind   token.Kind
		wantSuffix token.NumSuffix
		wantCodes  []diag.Code
	}{
		{"decimal", "42", token.Int, token.SuffixNone, nil},
		{"zero", "0", token.Int, token.SuffixNone, nil},
		{"leading zeros", "007", token.Int, token.SuffixNone, nil},
		{"hex", "0x2A", token.Int, token.SuffixNone, nil},
		{"hex uppercase digits", "0xABCDEF", token.Int, token.SuffixNone, nil},
		{"uppercase hex prefix", "0X2A", token.Int, token.SuffixNone, nil},
		{"octal", "0o52", token.Int, token.SuffixNone, nil},
		{"binary", "0b101010", token.Int, token.SuffixNone, nil},
		{"grouped digits", "1_000_000", token.Int, token.SuffixNone, nil},
		{"int suffix i8", "42i8", token.Int, token.SuffixI8, nil},
		{"int suffix u64", "42u64", token.Int, token.SuffixU64, nil},
		{"int suffix usize", "42usize", token.Int, token.SuffixUsize, nil},
		{"float", "3.14", token.Float, token.SuffixNone, nil},
		{"float suffix f32", "2.0f32", token.Float, token.SuffixF32, nil},
		{"exponent", "1e10", token.Float, token.SuffixNone, nil},
		{"exponent with suffix", "1e10f64", token.Float, token.SuffixF64, nil},
		{"uppercase exponent", "1E5", token.Float, token.SuffixNone, nil},
		{"signed exponent", "1e-10", token.Float, token.SuffixNone, nil},
		{"trailing underscore", "1_", token.Int, token.SuffixNone, []diag.Code{diag.CodeInvalidNumber}},
		{"double underscore", "1__0", token.Int, token.SuffixNone, []diag.Code{diag.CodeInvalidNumber}},
		{"leading underscore after prefix", "0x_2A", token.Int, token.SuffixNone, []diag.Code{diag.CodeInvalidNumber}},
		{"underscore before suffix", "1000_i8", token.Int, token.SuffixI8, []diag.Code{diag.CodeInvalidNumber}},
		{"hex without digits", "0x", token.Int, token.SuffixNone, []diag.Code{diag.CodeInvalidNumber}},
		{"binary digit out of range", "0b2", token.Int, token.SuffixNone, []diag.Code{diag.CodeInvalidNumber}},
		{"octal digit out of range", "0o8", token.Int, token.SuffixNone, []diag.Code{diag.CodeInvalidNumber}},
		{"float suffix on integer", "42f32", token.Int, token.SuffixNone, []diag.Code{diag.CodeInvalidNumber}},
		{"int suffix on float", "3.14i8", token.Float, token.SuffixNone, []diag.Code{diag.CodeInvalidNumber}},
		{"unknown suffix", "42abc", token.Int, token.SuffixNone, []diag.Code{diag.CodeInvalidNumber}},
		{"bare exponent letter", "1e", token.Int, token.SuffixNone, []diag.Code{diag.CodeInvalidNumber}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			toks, diags := lexSrc(t, c.src+"\n")
			assertCodes(t, diags, c.wantCodes...)
			if len(toks) == 0 || toks[0].Kind != c.wantKind {
				t.Fatalf("first token = %v, want %v", toks, c.wantKind)
			}
			if toks[0].Suffix != c.wantSuffix {
				t.Fatalf("suffix = %v, want %v", toks[0].Suffix, c.wantSuffix)
			}
		})
	}
}

func TestRangeVersusFloatDot(t *testing.T) {
	toks, diags := lexSrc(t, "1..2\n")
	assertClean(t, diags)
	assertKinds(t, sigKinds(toks), []token.Kind{token.Int, token.Range, token.Int})
}

func TestTupleIndexDot(t *testing.T) {
	toks, diags := lexSrc(t, "t.0\n")
	assertClean(t, diags)
	assertKinds(t, sigKinds(toks), []token.Kind{token.Ident, token.Dot, token.Int})
}
