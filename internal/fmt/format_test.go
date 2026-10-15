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
		"        return Tally { total: n}\n    }\n}\n"
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
