package lexer

import (
	"testing"

	"zyx/internal/token"
)

func TestLineTermination(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []token.Kind
	}{
		{
			name: "simple statements break at the newline",
			src:  "a\nb\n",
			want: []token.Kind{token.Ident, token.Newline, token.Ident, token.Newline},
		},
		{
			name: "operator at line end carries over",
			src:  "a = 1 +\nb\n",
			want: []token.Kind{
				token.Ident, token.Assign, token.Int, token.Plus,
				token.Ident, token.Newline,
			},
		},
		{
			name: "value at line end terminates",
			src:  "a = 1\n+ b\n",
			want: []token.Kind{
				token.Ident, token.Assign, token.Int, token.Newline,
				token.Plus, token.Ident, token.Newline,
			},
		},
		{
			name: "comma at line end carries over",
			src:  "a(1,\n2)\n",
			want: []token.Kind{
				token.Ident, token.LParen, token.Int, token.Comma,
				token.Int, token.RParen, token.Newline,
			},
		},
		{
			name: "brackets suppress newlines",
			src:  "f(\n1,\n2)\n",
			want: []token.Kind{
				token.Ident, token.LParen, token.Int, token.Comma,
				token.Int, token.RParen, token.Newline,
			},
		},
		{
			name: "blank lines collapse",
			src:  "a\n\n\nb\n",
			want: []token.Kind{token.Ident, token.Newline, token.Ident, token.Newline},
		},
		{
			name: "semicolon separates statements",
			src:  "a; b\n",
			want: []token.Kind{token.Ident, token.Newline, token.Ident, token.Newline},
		},
		{
			name: "repeated semicolons collapse",
			src:  "a;;b\n",
			want: []token.Kind{token.Ident, token.Newline, token.Ident, token.Newline},
		},
		{
			name: "semicolon plus newline does not double",
			src:  "a;\nb\n",
			want: []token.Kind{token.Ident, token.Newline, token.Ident, token.Newline},
		},
		{
			name: "comment before the break",
			src:  "a = 1 + // carry\nb\n",
			want: []token.Kind{
				token.Ident, token.Assign, token.Int, token.Plus, token.Comment,
				token.Ident, token.Newline,
			},
		},
		{
			name: "comment does not extend the statement",
			src:  "a // note\nb\n",
			want: []token.Kind{
				token.Ident, token.Comment, token.Newline, token.Ident, token.Newline,
			},
		},
		{
			name: "range at line end carries over",
			src:  "a = b..\nc\n",
			want: []token.Kind{
				token.Ident, token.Assign, token.Ident, token.Range,
				token.C, token.Newline,
			},
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

func TestBlockBracesDoNotCountDepth(t *testing.T) {
	src := "if x > 0 {\nfoo()\n}\n"
	toks, diags := lexSrc(t, src)
	assertClean(t, diags)
	assertKinds(t, kindsOf(toks), []token.Kind{
		token.If, token.Ident, token.Gt, token.Int, token.LBrace,
		token.Newline,
		token.Ident, token.LParen, token.RParen, token.Newline,
		token.RBrace, token.Newline,
	})
}

func TestLineEndingBraceWithComment(t *testing.T) {
	src := "if x { // open\nfoo()\n}\n"
	toks, diags := lexSrc(t, src)
	assertClean(t, diags)
	assertKinds(t, kindsOf(toks), []token.Kind{
		token.If, token.Ident, token.LBrace, token.Comment,
		token.Newline,
		token.Ident, token.LParen, token.RParen, token.Newline,
		token.RBrace, token.Newline,
	})
}

func TestMidLineBraceSuppressesNewlines(t *testing.T) {
	src := "var p = Point { x: 1,\ny: 2 }\n"
	toks, diags := lexSrc(t, src)
	assertClean(t, diags)
	assertKinds(t, kindsOf(toks), []token.Kind{
		token.Var, token.Ident, token.Assign, token.Ident, token.LBrace,
		token.Ident, token.Colon, token.Int, token.Comma,
		token.Ident, token.Colon, token.Int, token.RBrace, token.Newline,
	})
}

func TestMultiLineLiteralWithLineEndingBrace(t *testing.T) {
	src := "var p = Point {\nx: 1,\n}\n"
	toks, diags := lexSrc(t, src)
	assertClean(t, diags)
	assertKinds(t, kindsOf(toks), []token.Kind{
		token.Var, token.Ident, token.Assign, token.Ident, token.LBrace,
		token.Newline,
		token.Ident, token.Colon, token.Int, token.Comma,
		token.RBrace, token.Newline,
	})
}

func TestSemicolonInsideBracketsIsInvisible(t *testing.T) {
	src := "c { for (i; j) }\n"
	toks, diags := lexSrc(t, src)
	assertClean(t, diags)
	assertKinds(t, kindsOf(toks), []token.Kind{
		token.C, token.LBrace, token.For, token.LParen, token.Ident,
		token.Ident, token.RParen, token.RBrace, token.Newline,
	})
}

func TestFileWithoutTrailingNewline(t *testing.T) {
	toks, diags := lexSrc(t, "a")
	assertClean(t, diags)
	assertKinds(t, kindsOf(toks), []token.Kind{token.Ident, token.Newline})
}

func TestContinuationAtEndOfFile(t *testing.T) {
	toks, diags := lexSrc(t, "a +")
	assertClean(t, diags)
	assertKinds(t, kindsOf(toks), []token.Kind{token.Ident, token.Plus})
}

func TestUnbalancedBracketsDoNotPanic(t *testing.T) {
	cases := []string{"(a\n", "a)\n", "}\n", "f({a)\n", "[\n1\n"}
	for _, src := range cases {
		toks, diags := lexSrc(t, src)
		if len(toks) == 0 || toks[len(toks)-1].Kind != token.EOF {
			t.Fatalf("src %q: stream does not end with EOF", src)
		}
		_ = diags
	}
}
