package fmt

import (
	"strings"
	"testing"

	"zyx/internal/source"
)

// format returns the canonical text of a source.
func format(src string) string {
	return string(Source(source.NewFile("test.zyx", []byte(src))))
}

// sources are the inputs the formatter is checked against.
var sources = []string{
	"package main\n",
	"package main\nimport \"std/fmt\"\nfunc main() {\nfmt.Println(\"a\")\n}\n",
	"package main\n\nfunc f(a: i64, b: i64) -> i64 {\nvar t = a + b\nreturn t\n}\n",
	"package main\n\nstruct P {\nx: i64\nfunc Sum() -> i64 {\nreturn self.x\n}\n}\n",
	"package main\n\nfunc g() {\nvar m: Map<str, i64> = Map { \"a\": 1 }\nvar i = 0\nfor i < 3 {\ni += 1\n}\n}\n",
	"package main\n\nfunc h() {\nvar x = 0 - 1\nvar y = a - b\n}\n",
	"package main\n\nfunc i() {\n// a comment\nvar x = 1 // trailing\n}\n",
	"package main\nimport c \"test.h\" { fast read, write }\nfast extern func getpid() -> i32\nfunc main() {\n}\n",
}

func TestFormatIsIdempotent(t *testing.T) {
	for _, src := range sources {
		once := format(src)
		twice := format(once)
		if once != twice {
			t.Errorf("format is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", once, twice)
		}
	}
}

func TestFormatKeepsEveryToken(t *testing.T) {
	for _, src := range sources {
		got := format(src)
		for _, want := range []string{"package", "main"} {
			if !strings.Contains(got, want) {
				t.Errorf("format(%q) lost %q:\n%s", src, want, got)
			}
		}
		if strings.HasSuffix(got, "\n\n") {
			t.Errorf("format(%q) ends with a blank line", src)
		}
	}
}

func TestFormatIndentsBlocks(t *testing.T) {
	src := "package main\nfunc main() {\nfmt.Println(\"a\")\n}\n"
	got := format(src)
	want := "package main\n\nfunc main() {\n    fmt.Println(\"a\")\n}\n"
	if got != want {
		t.Errorf("format =\n%q\nwant\n%q", got, want)
	}
}

// A result type that is a user-declared type is a type name like any other,
// so the brace after it opens a body rather than a construction. The source
// below is already canonical, which makes formatting it a no-op.
func TestFormatKeepsABodyAfterAUserResultType(t *testing.T) {
	src := "package main\n\nstruct Tally {\n    total: i64\n    func init(n: i64) -> Tally {\n" +
		"        return Tally { total: n }\n    }\n}\n"
	if got := format(src); got != src {
		t.Errorf("format =\n%q\nwant\n%q", got, src)
	}
}

// A call on a type argument list attaches to the list it closes, while a
// comparison keeps the space before its right operand. The source is already
// canonical, which makes formatting it a no-op.
func TestFormatAttachesACallToATypeArgumentList(t *testing.T) {
	src := "package main\n\nfunc f(n: i64) -> i64 {\n    return n\n}\n\nfunc main() {\n" +
		"    var xs = new Array<i64>(4)\n    var b = 1 > (2)\n    var d = f(1)\n}\n"
	if got := format(src); got != src {
		t.Errorf("format =\n%q\nwant\n%q", got, src)
	}
}

func TestFormatKeepsAnEmptyBodyOnOneLine(t *testing.T) {
	src := "package main\nfunc main() {\n}\nfunc other() {\n}\n"
	got := format(src)
	want := "package main\n\nfunc main() { }\n\nfunc other() { }\n"
	if got != want {
		t.Errorf("format =\n%q\nwant\n%q", got, want)
	}
	if again := format(got); again != got {
		t.Errorf("an empty body is not idempotent:\n%q", again)
	}
}

func TestFormatPutsPlainImportsBeforeCImports(t *testing.T) {
	src := "package main\nimport c \"zyxrt.h\"\nimport \"std/fmt\"\nimport \"std/str\"\n\nfunc main() {\n}\n"
	got := format(src)
	fmtAt := strings.Index(got, "std/fmt")
	strAt := strings.Index(got, "std/str")
	cAt := strings.Index(got, "zyxrt.h")
	if fmtAt < 0 || strAt < 0 || cAt < 0 {
		t.Fatalf("an import was lost:\n%s", got)
	}
	if !(fmtAt < strAt && strAt < cAt) {
		t.Errorf("imports are not grouped:\n%s", got)
	}
}

func TestFormatLeavesAFailedFileAlone(t *testing.T) {
	src := "package main\n\nfunc f() {\n\tvar s = \"unterminated\n}\n"
	if got := format(src); got != src {
		t.Errorf("a file that does not lex must come back unchanged:\n%q", got)
	}
}

func TestFormatSortsImports(t *testing.T) {
	src := "package main\nimport \"std/str\"\nimport \"std/fmt\"\n\nfunc main() {\n}\n"
	got := format(src)
	fmtAt := strings.Index(got, "std/fmt")
	strAt := strings.Index(got, "std/str")
	if fmtAt < 0 || strAt < 0 || fmtAt > strAt {
		t.Errorf("imports are not in byte order:\n%s", got)
	}
}

func TestFormatKeepsATypeArgumentListTight(t *testing.T) {
	src := "package main\n\nfunc f() {\nvar m: Map<str, i64> = Map { \"a\": 1 }\n}\n"
	got := format(src)
	if !strings.Contains(got, "Map<str, i64>") {
		t.Errorf("a type argument list must stay tight:\n%s", got)
	}
}

// TestFormatKeepsFastDeclarations checks the two places a C function's
// blocking behavior is declared: the fast list of an import of a header, and
// the fast qualifier of an extern declaration.
func TestFormatKeepsFastDeclarations(t *testing.T) {
	src := "package main\nimport c \"test.h\" { fast read, write }\nfast extern func getpid() -> i32\nfunc main() {\n}\n"
	got := format(src)
	for _, want := range []string{"fast read, write", "fast extern func getpid() -> i32"} {
		if !strings.Contains(got, want) {
			t.Errorf("the formatted source lost %q:\n%s", want, got)
		}
	}
	if again := format(got); again != got {
		t.Errorf("format is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", got, again)
	}
}

// TestFormatKeepsATrailingComment checks that a comment the program wrote at
// the end of a line stays on that line. The format neither wraps nor splits
// lines, and it keeps a line comment where it was written.
func TestFormatKeepsATrailingComment(t *testing.T) {
	src := "package main\nfunc f() {\nvar x = 1 // after a binding\n} // after a block\n"
	got := format(src)
	for _, want := range []string{"var x = 1 // after a binding", "} // after a block"} {
		if !strings.Contains(got, want) {
			t.Errorf("the formatted source moved a trailing comment:\n%s", got)
		}
	}
	if again := format(got); again != got {
		t.Errorf("format is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", got, again)
	}
}

// TestFormatKeepsABlockComment checks that a block comment is kept where it
// was written: a multi-line one stays multi-line, an inline one stays on its
// line, and a nested one is one comment rather than two.
func TestFormatKeepsABlockComment(t *testing.T) {
	src := "package main\n/* one\n   /* nested */\n   two */\nfunc f() {\nvar x = 1 /* inline */ + 2\n/* own line */\nvar y = 3\n}\n"
	got := format(src)
	for _, want := range []string{"/* one", "/* nested */", "var x = 1 /* inline */ + 2", "/* own line */\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("the formatted source lost %q:\n%s", want, got)
		}
	}
	if again := format(got); again != got {
		t.Errorf("format is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", got, again)
	}
}

// checkFormat reports whether src formats to want and whether formatting the
// result again leaves it alone. The format is idempotent, so both runs must
// equal the expected text.
func checkFormat(t *testing.T, name, src, want string) {
	t.Helper()
	once := format(src)
	if once != want {
		t.Errorf("%s: format =\n%s\nwant\n%s", name, once, want)
	}
	if twice := format(once); twice != want {
		t.Errorf("%s: second format =\n%s\nwant\n%s", name, twice, want)
	}
}

// TestFormatKeepsStructFieldsOnTheirLines checks that every field of a struct
// declaration stays on the line the program wrote it on. A field whose type
// ends in a type argument list once joined the field after it, because the
// lexer folds the line break into the `>` that closes the list.
func TestFormatKeepsStructFieldsOnTheirLines(t *testing.T) {
	src := "package main\n\nstruct Node {\n    counts: Array<u32>\n    symbols: Array<u32>\n" +
		"    left: i32\n    right: i32\n}\n"
	checkFormat(t, "struct fields", src, src)
}

// TestFormatKeepsAMultiLineLiteral checks that a struct literal the program
// wrote across lines stays across lines, with its members indented under the
// statement that opened it and the closing brace back at that level.
func TestFormatKeepsAMultiLineLiteral(t *testing.T) {
	src := "package main\n\nstruct Node {\n    counts: Array<u32>\n    left: i32\n}\n\n" +
		"func make_node() -> Node {\n    return Node {\n        counts: new Array<u32>(0),\n" +
		"        left: 0,\n    }\n}\n"
	checkFormat(t, "multi-line literal", src, src)
}

// TestFormatKeepsACommentAboveItsDeclaration checks that a comment directly
// above a declaration stays attached to it: the format does not put a blank
// line between the two.
func TestFormatKeepsACommentAboveItsDeclaration(t *testing.T) {
	src := "package main\n\n// f does nothing.\nfunc f() {}\n"
	want := "package main\n\n// f does nothing.\nfunc f() { }\n"
	checkFormat(t, "documented declaration", src, want)
}

// TestFormatKeepsAnElseBesideItsBrace checks that `else` is written beside the
// closing brace, with one space between them and none doubled.
func TestFormatKeepsAnElseBesideItsBrace(t *testing.T) {
	src := "package main\n\nfunc f(n: i64) -> i64 {\n    if n > 0 {\n        return 1\n" +
		"    } else {\n        return -1\n    }\n}\n"
	checkFormat(t, "else", src, src)
}

// TestFormatAttachesAUnaryMinus checks that a unary minus attaches to its
// operand while the space before it follows the token on its left, so a
// literal and a returned value read the same as they were written.
func TestFormatAttachesAUnaryMinus(t *testing.T) {
	src := "package main\n\nfunc f() -> i64 {\n    var low: i64 = -9223372036854775808\n" +
		"    var pair = (-1, -2)\n    return -low\n}\n"
	checkFormat(t, "unary minus", src, src)
}

// TestFormatClosesNestedTypeArguments checks that `>>` closes two type
// argument lists as tightly as one `>` closes one, and that a call attaches to
// the list it follows.
func TestFormatClosesNestedTypeArguments(t *testing.T) {
	src := "package main\n\nvar rows: Array<Node<u32>>\nvar made = new Array<Node<u32>>(0)\n"
	checkFormat(t, "nested type arguments", src, src)
}

// TestFormatKeepsABlankLineInsideABlock checks that a single blank line the
// program wrote inside a block stays, while a second one in a row is dropped.
func TestFormatKeepsABlankLineInsideABlock(t *testing.T) {
	src := "package main\n\nfunc f() {\n    var x = 1\n\n\n    var y = 2\n}\n"
	want := "package main\n\nfunc f() {\n    var x = 1\n\n    var y = 2\n}\n"
	checkFormat(t, "blank line in a block", src, want)
}
