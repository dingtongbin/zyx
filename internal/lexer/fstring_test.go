package lexer

import (
	"slices"
	"testing"

	"zyx/internal/diag"
	"zyx/internal/token"
)

func TestFStringParts(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		wantKinds []token.FStrPartKind
		wantTexts []string // literal segment contents in order
		wantExprs []string // interpolation sources in order
	}{
		{
			name:      "single interpolation",
			src:       `f"count = {n}"`,
			wantKinds: []token.FStrPartKind{token.FStrText, token.FStrExpr},
			wantTexts: []string{"count = "},
			wantExprs: []string{"n"},
		},
		{
			name:      "brace escapes",
			src:       `f"{{}}x"`,
			wantKinds: []token.FStrPartKind{token.FStrText},
			wantTexts: []string{"{}x"},
		},
		{
			name:      "alternating segments",
			src:       `f"a{b}c{d}e"`,
			wantKinds: []token.FStrPartKind{token.FStrText, token.FStrExpr, token.FStrText, token.FStrExpr, token.FStrText},
			wantTexts: []string{"a", "c", "e"},
			wantExprs: []string{"b", "d"},
		},
		{
			name:      "escape in literal segment",
			src:       `f"a\nb{c}"`,
			wantKinds: []token.FStrPartKind{token.FStrText, token.FStrExpr},
			wantTexts: []string{"a\nb"},
			wantExprs: []string{"c"},
		},
		{
			name:      "string braces stay in the expression",
			src:       `f"{x + "{"}"}`,
			wantKinds: []token.FStrPartKind{token.FStrExpr},
			wantExprs: []string{`x + "{"`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			toks, diags := lexSrc(t, c.src+"\n")
			assertClean(t, diags)
			if toks[0].Kind != token.FString {
				t.Fatalf("first token = %v, want fstring", toks[0].Kind)
			}
			gotKinds := make([]token.FStrPartKind, 0, len(toks[0].Parts))
			for _, p := range toks[0].Parts {
				gotKinds = append(gotKinds, p.Kind)
			}
			if !slices.Equal(gotKinds, c.wantKinds) {
				t.Fatalf("part kinds = %v, want %v", gotKinds, c.wantKinds)
			}
			var texts, exprs []string
			for _, p := range toks[0].Parts {
				if p.Kind == token.FStrText {
					texts = append(texts, p.Text)
				} else {
					exprs = append(exprs, p.Span.Text())
				}
			}
			if !slices.Equal(texts, c.wantTexts) {
				t.Errorf("literal segments = %q, want %q", texts, c.wantTexts)
			}
			if !slices.Equal(exprs, c.wantExprs) {
				t.Errorf("interpolations = %q, want %q", exprs, c.wantExprs)
			}
		})
	}
}

func TestFStringErrors(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		wantCodes []diag.Code
	}{
		{"empty interpolation", `f"{}"`, []diag.Code{diag.CodeEmptyInterpolation}},
		{"blank interpolation", `f"{ }"`, []diag.Code{diag.CodeEmptyInterpolation}},
		{"unterminated literal", `f"abc`, []diag.Code{diag.CodeUnterminatedLiteral}},
		{"unterminated interpolation", `f"a{b"`, []diag.Code{diag.CodeUnterminatedLiteral}},
		{"lone closing brace", `f"a}b"`, []diag.Code{diag.CodeInvalidEscape}},
		{"comment swallows the brace", `f"{x // c}"`, []diag.Code{diag.CodeUnterminatedLiteral}},
		// UTF-8 handling test data written as a Go escape to keep this file ASCII.
		{"non-ascii in interpolation", "f\"{ \u4E2D }\"", []diag.Code{diag.CodeNonASCIISource}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, diags := lexSrc(t, c.src+"\n")
			assertCodes(t, diags, c.wantCodes...)
		})
	}
}

func TestNestedFStringIsRejected(t *testing.T) {
	_, diags := lexSrc(t, `f"{f"a"}"`+"\n")
	if !slices.ContainsFunc(diags, func(d diag.Diagnostic) bool {
		return d.Code == diag.CodeNestedFString
	}) {
		t.Fatalf("missing nested f-string diagnostic: %v", diags)
	}
}

func TestNonASCIIInsideInterpolationStringIsAllowed(t *testing.T) {
	// UTF-8 handling test data written as a Go escape to keep this file ASCII.
	_, diags := lexSrc(t, "f\"{m[\"\u4E2D\"]}\""+"\n")
	assertClean(t, diags)
}
