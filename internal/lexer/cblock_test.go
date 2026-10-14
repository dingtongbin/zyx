package lexer

import (
	"testing"

	"zyx/internal/diag"
	"zyx/internal/token"
)

func TestCBlockIsOneToken(t *testing.T) {
	src := "c { puts(\"x\"); }\n"
	toks, diags := lexSrc(t, src)
	assertClean(t, diags)
	assertKinds(t, kindsOf(toks), []token.Kind{token.CBlock, token.Newline})
	if got, want := toks[0].Text(), "c { puts(\"x\"); }"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestCBlockHoldsHostileC(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"unsigned suffix", "var h = c { return 2166136261u; }\n"},
		{"ternary", "var x = c { return a ? b : c; }\n"},
		{"nested braces", "c { if (a) { b(); } }\n"},
		{"string holding a brace", "c { f(\"}\"); }\n"},
		{"char holding a brace", "c { f('}'); }\n"},
		{"escaped quote", "c { f(\"a\\\"}\"); }\n"},
		{"line comment holding a brace", "c { f(); // }\n}\n"},
		{"block comment holding a brace", "c { /* } */ f(); }\n"},
		{"preprocessor line", "c { #define X 1\nreturn X; }\n"},
		{"empty block", "c { }\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			toks, diags := lexSrc(t, c.src)
			assertClean(t, diags)
			if !containsKind(toks, token.CBlock) {
				t.Fatalf("token stream has no cblock token: %v", toks)
			}
		})
	}
}

// containsKind reports whether toks holds a token of kind k.
func containsKind(toks []token.Token, k token.Kind) bool {
	for _, tk := range toks {
		if tk.Kind == k {
			return true
		}
	}
	return false
}

func TestCKeywordOutsideBlocks(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []token.Kind
	}{
		{
			name: "C call",
			src:  "c.printf(\"x\")\n",
			want: []token.Kind{
				token.C, token.Dot, token.Ident, token.LParen,
				token.Str, token.RParen, token.Newline,
			},
		},
		{
			name: "import c header",
			src:  "import c \"stdio.h\"\n",
			want: []token.Kind{token.Import, token.C, token.Str, token.Newline},
		},
		{
			name: "C ABI definition",
			src:  "c func OnAccept(fd: i32) {}\n",
			want: []token.Kind{
				token.C, token.Func, token.Ident, token.LParen, token.Ident,
				token.Colon, token.Ident, token.RParen, token.LBrace,
				token.RBrace, token.Newline,
			},
		},
		{
			name: "identifier starting with c",
			src:  "count\n",
			want: []token.Kind{token.Ident, token.Newline},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			toks, diags := lexSrc(t, c.src)
			assertClean(t, diags)
			assertKinds(t, kindsOf(toks), c.want)
		})
	}
}

func TestUnterminatedCBlock(t *testing.T) {
	_, diags := lexSrc(t, "c { f();\n")
	assertCodes(t, diags, diag.CodeUnterminatedCBlock)
	if diags[0].Help == "" {
		t.Fatal("unterminated C block diagnostic lacks a fix suggestion")
	}
}

func TestCBlockSpansMultipleLines(t *testing.T) {
	src := "func checksum(data: [u8]) -> u32 {\nreturn c {\nunsigned int h = 1u;\nreturn h;\n}\n}\n"
	toks, diags := lexSrc(t, src)
	assertClean(t, diags)
	assertKinds(t, kindsOf(toks), []token.Kind{
		token.Func, token.Ident, token.LParen, token.Ident, token.Colon,
		token.LBrack, token.Ident, token.RBrack, token.RParen, token.Arrow,
		token.Ident, token.LBrace,
		token.Newline,
		token.Return, token.CBlock,
		token.Newline,
		token.RBrace,
		token.Newline,
	})
}
