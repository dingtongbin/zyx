package lexer

import (
	"slices"
	"strings"
	"testing"

	"zyx/internal/diag"
	"zyx/internal/source"
	"zyx/internal/token"
)

// lexSrc tokenizes src as main.zyx.
func lexSrc(t *testing.T, src string) ([]token.Token, []diag.Diagnostic) {
	t.Helper()
	return Lex(source.NewFile("main.zyx", []byte(src)))
}

// kindsOf returns every kind in toks except the trailing EOF.
func kindsOf(toks []token.Token) []token.Kind {
	kinds := make([]token.Kind, 0, len(toks))
	for _, tk := range toks {
		if tk.Kind == token.EOF {
			break
		}
		kinds = append(kinds, tk.Kind)
	}
	return kinds
}

// sigKinds returns the kinds of a single-line source with the statement
// terminator that source generates removed.
func sigKinds(toks []token.Token) []token.Kind {
	kinds := kindsOf(toks)
	if n := len(kinds); n > 0 && kinds[n-1] == token.Newline {
		kinds = kinds[:n-1]
	}
	return kinds
}

func assertKinds(t *testing.T, got, want []token.Kind) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
}

func assertCodes(t *testing.T, diags []diag.Diagnostic, want ...diag.Code) {
	t.Helper()
	got := make([]diag.Code, len(diags))
	for i, d := range diags {
		got[i] = d.Code
	}
	if !slices.Equal(got, want) {
		t.Fatalf("diagnostic codes = %v, want %v", got, want)
	}
}

func assertClean(t *testing.T, diags []diag.Diagnostic) {
	t.Helper()
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

func TestKeywordsLexAsKeywordKinds(t *testing.T) {
	want := []token.Kind{
		token.Package, token.Import, token.Const, token.Var, token.Type,
		token.Func, token.Struct, token.Union, token.Enum, token.Interface,
		token.Impl,
		token.SelfLower, token.SelfUpper, token.New, token.If, token.Else,
		token.Match, token.For, token.In, token.Break, token.Continue,
		token.Return, token.Defer, token.Region, token.Is, token.Try,
		token.Comptime, token.Spawn, token.SpawnWatched, token.Chan,
		token.Select, token.Recv, token.Send, token.Timeout, token.Default,
		token.Extern, token.C, token.Fast, token.True, token.False,
	}
	spellings := make([]string, 0, len(want))
	for _, k := range want {
		spellings = append(spellings, k.String())
	}
	toks, diags := lexSrc(t, strings.Join(spellings, " ")+"\n")
	assertClean(t, diags)
	assertKinds(t, sigKinds(toks), want)
}

func TestIdentifierForms(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []token.Kind
	}{
		{"simple", "foo", []token.Kind{token.Ident}},
		{"underscore binding", "_", []token.Kind{token.Underscore}},
		{"joined hyphen", "a-b", []token.Kind{token.Ident}},
		{"hyphen with digits", "a-1", []token.Kind{token.Ident}},
		{"spaced minus", "a - b", []token.Kind{token.Ident, token.Minus, token.Ident}},
		{"space after hyphen", "a- b", []token.Kind{token.Ident, token.Minus, token.Ident}},
		{"space before hyphen", "a -b", []token.Kind{token.Ident, token.Minus, token.Ident}},
		{"double hyphen", "a--b", []token.Kind{token.Ident, token.Minus, token.Minus, token.Ident}},
		{"number then minus", "1-2", []token.Kind{token.Int, token.Minus, token.Int}},
		{"removed keywords stay identifiers", "while loop export",
			[]token.Kind{token.Ident, token.Ident, token.Ident}},
		{"snake case", "my_var", []token.Kind{token.Ident}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			toks, diags := lexSrc(t, c.src+"\n")
			assertClean(t, diags)
			assertKinds(t, sigKinds(toks), c.want)
		})
	}
}

func TestIdentifierText(t *testing.T) {
	toks, diags := lexSrc(t, "a-b\n")
	assertClean(t, diags)
	if got, want := toks[0].Text(), "a-b"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got, want := toks[0].Kind, token.Ident; got != want {
		t.Fatalf("kind = %v, want %v", got, want)
	}
}

func TestOperatorSpellingsLex(t *testing.T) {
	src := "+ - * / % << >> & ^ | ~ ! == != < <= > >= && || .. => -> ... " +
		", . : = += -= *= /= %= &= |= ^= <<= >>= ( ) [ ] { }"
	want := []token.Kind{
		token.Plus, token.Minus, token.Star, token.Slash, token.Percent,
		token.Shl, token.Shr, token.Amp, token.Caret, token.Pipe, token.Tilde,
		token.Bang, token.Eq, token.Ne, token.Lt, token.Le, token.Gt, token.Ge,
		token.AndAnd, token.OrOr, token.Range, token.FatArrow, token.Arrow,
		token.Ellipsis, token.Comma, token.Dot, token.Colon, token.Assign,
		token.AddAssign, token.SubAssign, token.MulAssign, token.DivAssign,
		token.ModAssign, token.AndAssign, token.OrAssign, token.XorAssign,
		token.ShlAssign, token.ShrAssign, token.LParen, token.RParen,
		token.LBrack, token.RBrack, token.LBrace, token.RBrace,
	}
	toks, diags := lexSrc(t, src+"\n")
	assertClean(t, diags)
	assertKinds(t, sigKinds(toks), want)
}

func TestCommentTokens(t *testing.T) {
	toks, diags := lexSrc(t, "f() // tail\n")
	assertClean(t, diags)
	assertKinds(t, kindsOf(toks), []token.Kind{
		token.Ident, token.LParen, token.RParen, token.Comment, token.Newline,
	})
	if toks[3].Doc {
		t.Fatal("line comment marked as doc comment")
	}

	toks, diags = lexSrc(t, "/// doc\n")
	assertClean(t, diags)
	if !toks[0].Doc {
		t.Fatal("/// comment not marked as doc comment")
	}
	if got, want := toks[0].Text(), "/// doc"; got != want {
		t.Fatalf("doc comment text = %q, want %q", got, want)
	}

	// UTF-8 handling test data written as Go escapes to keep this file ASCII.
	toks, diags = lexSrc(t, "// \u4E2D comment\n")
	assertClean(t, diags)
	if toks[0].Kind != token.Comment {
		t.Fatalf("kind = %v, want comment", toks[0].Kind)
	}
}

func TestCommentOnlyFileHasNoNewline(t *testing.T) {
	toks, diags := lexSrc(t, "// nothing here\n")
	assertClean(t, diags)
	assertKinds(t, kindsOf(toks), []token.Kind{token.Comment})
}

func TestIllegalCharacters(t *testing.T) {
	toks, diags := lexSrc(t, "@\n")
	assertCodes(t, diags, diag.CodeUnexpectedChar)
	assertKinds(t, kindsOf(toks), nil)
	if diags[0].Help == "" {
		t.Fatal("unexpected character diagnostic lacks a fix suggestion")
	}

	_, diags = lexSrc(t, "a::b\n")
	assertCodes(t, diags, diag.CodeDoubleColon)
	if diags[0].Help == "" {
		t.Fatal(":: diagnostic lacks a fix suggestion")
	}

	toks, diags = lexSrc(t, "var \u4E2D = 1\n")
	assertCodes(t, diags, diag.CodeNonASCIISource)
	assertKinds(t, kindsOf(toks), []token.Kind{
		token.Var, token.Assign, token.Int, token.Newline,
	})

	_, diags = lexSrc(t, "s = \"\u4E2D\"\n")
	assertClean(t, diags)
}

func TestInvalidUTF8IsFatal(t *testing.T) {
	toks, diags := Lex(source.NewFile("main.zyx", []byte{'a', 0xFF, 'b'}))
	assertCodes(t, diags, diag.CodeInvalidUTF8)
	if len(toks) != 0 {
		t.Fatalf("got %d tokens, want none", len(toks))
	}
}

func TestMultipleErrorsAreCollected(t *testing.T) {
	_, diags := lexSrc(t, "@\n@\n")
	assertCodes(t, diags, diag.CodeUnexpectedChar, diag.CodeUnexpectedChar)
}

func TestByteOrderMarkIsIgnored(t *testing.T) {
	toks, diags := Lex(source.NewFile("main.zyx", []byte("\xEF\xBB\xBFa\n")))
	assertClean(t, diags)
	assertKinds(t, kindsOf(toks), []token.Kind{token.Ident, token.Newline})
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
