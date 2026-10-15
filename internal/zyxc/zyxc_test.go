package zyxc

import (
	"strings"
	"testing"

	"zyx/internal/ast"
	"zyx/internal/check"
	"zyx/internal/coro"
	"zyx/internal/marks"
	"zyx/internal/parser"
	"zyx/internal/resolve"
	"zyx/internal/source"
	"zyx/internal/toolchain"
)

// buildSource runs the front end on one package and returns its tree.
func buildSource(t *testing.T, src string) *File {
	t.Helper()
	file, pdiags := parser.Parse(source.NewFile("server/server.zyx", []byte(src)))
	if len(pdiags) != 0 {
		t.Fatalf("parse: %v", pdiags)
	}
	pkg := &resolve.Package{Path: "example.com/app/server", Files: []*ast.File{file}}
	rinfo, rdiags := resolve.Resolve(pkg, nil)
	if len(rdiags) != 0 {
		t.Fatalf("resolve: %v", rdiags)
	}
	target := toolchain.HostTarget()
	cpkg, cdiags := check.Check(pkg, rinfo, nil, target)
	if len(cdiags) != 0 {
		t.Fatalf("check: %v", cdiags)
	}
	cinfo := coro.Build(pkg)
	minfo, mdiags := marks.Build(pkg, rinfo, cpkg, cinfo)
	if len(mdiags) != 0 {
		t.Fatalf("marks: %v", mdiags)
	}
	ir, err := Build(pkg, rinfo, cpkg, minfo, cinfo, target.Triple)
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func TestFormatHeader(t *testing.T) {
	f := buildSource(t, `package server

const MaxConn = 64
`)
	lines := strings.Split(string(Format(f)), "\n")
	if lines[0] != "zyx 1" {
		t.Errorf("first line = %q, want zyx 1", lines[0])
	}
	if !strings.HasPrefix(lines[1], "target ") {
		t.Errorf("second line = %q, want a target", lines[1])
	}
	if lines[2] != "module example.com/app/server" {
		t.Errorf("third line = %q", lines[2])
	}
	if !strings.HasPrefix(lines[3], "exports ") {
		t.Errorf("fourth line = %q, want exports", lines[3])
	}
}

func TestDeclarationsAndReferences(t *testing.T) {
	f := buildSource(t, `package server

const MaxConn = 64

struct Conn {
	host: str
	func Close() {
	}
}

func Listen() -> Conn {
	return Conn { host: "local" }
}

func Use(conn: Conn) {
	conn.Close()
}
`)
	text := string(Format(f))
	for _, want := range []string{
		"const MaxConn example.com/app/server.MaxConn",
		"struct Conn example.com/app/server.Conn",
		"field host str",
		"func Close plain example.com/app/server.Conn.Close",
		"func Listen plain example.com/app/server.Listen",
		"struct-lit Conn",
		"field-init host",
		"func Use plain example.com/app/server.Use",
		"mcall Close example.com/app/server.Conn.Close",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
}

func TestMarksAreWritten(t *testing.T) {
	f := buildSource(t, `package server

func Work(xs: Array<i64>) -> i64 {
	var buf = new Array<i64>(4)
	var total: i64 = 0
	for x in xs {
		total = total + x
	}
	return total + i64(buf.Len())
}
`)
	text := string(Format(f))
	if !strings.Contains(text, "iter-guard") {
		t.Errorf("missing the iteration guard in\n%s", text)
	}
	if !strings.Contains(text, "home:r0") {
		t.Errorf("missing the home region in\n%s", text)
	}
}

func TestCanonicalForm(t *testing.T) {
	f := buildSource(t, `package server

struct Conn {
	host: str
	func Close() {
	}
}
`)
	text := string(Format(f))
	if !strings.HasSuffix(text, "\n") {
		t.Error("the file must end with one newline")
	}
	if strings.HasSuffix(text, "\n\n") {
		t.Error("the file must end with exactly one newline")
	}
	for _, line := range strings.Split(text, "\n") {
		if line != strings.TrimRight(line, " \t") {
			t.Errorf("line has trailing space: %q", line)
		}
	}
	if !strings.Contains(text, "\n  field host ") {
		t.Errorf("a child must be indented two spaces:\n%s", text)
	}
}

func TestFormatIsStable(t *testing.T) {
	src := `package server

struct Conn {
	host: str
	func Close() {
	}
}

func Listen() -> Conn {
	return Conn { host: "local" }
}
`
	first := string(Format(buildSource(t, src)))
	second := string(Format(buildSource(t, src)))
	if first != second {
		t.Errorf("two runs differ:\n%s\n%s", first, second)
	}
}

func TestExportsHashCoversTheInterface(t *testing.T) {
	private := `package server

const maxConn = 1
`
	public := `package server

const MaxConn = 1
`
	a := buildSource(t, private)
	b := buildSource(t, public)
	if a.Exports == b.Exports {
		t.Error("a change to the interface must change the hash")
	}

	again := buildSource(t, private)
	if a.Exports != again.Exports {
		t.Error("the same interface must hash the same")
	}
}

func TestQuoteToken(t *testing.T) {
	cases := map[string]string{
		"plain": "plain",
		"a b":   `"a b"`,
		`a"b`:   `"a\"b"`,
		"":      `""`,
		"a\tb":  `"a\tb"`,
	}
	for in, want := range cases {
		if got := quoteToken(in); got != want {
			t.Errorf("quoteToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckKindRejectsBadKinds(t *testing.T) {
	for _, kind := range []string{"", "Upper", "with space", "semi;colon"} {
		if err := checkKind(kind); err == nil {
			t.Errorf("kind %q must be rejected", kind)
		}
	}
	if err := checkKind("extern-struct"); err != nil {
		t.Errorf("a valid kind was rejected: %v", err)
	}
}
