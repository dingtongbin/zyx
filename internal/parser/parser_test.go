package parser

import (
	"slices"
	"testing"

	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/source"
)

// parseSrc parses src as main.zyx.
func parseSrc(t *testing.T, src string) (*ast.File, []diag.Diagnostic) {
	t.Helper()
	return Parse(source.NewFile("main.zyx", []byte(src)))
}

// assertClean fails when parsing produced any diagnostic.
func assertClean(t *testing.T, diags []diag.Diagnostic) {
	t.Helper()
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

// assertCodes compares the diagnostic codes in order.
func assertCodes(t *testing.T, diags []diag.Diagnostic, want ...diag.Code) {
	t.Helper()
	got := make([]diag.Code, len(diags))
	for i, d := range diags {
		got[i] = d.Code
	}
	if !slices.Equal(got, want) {
		t.Fatalf("codes = %v, want %v", got, want)
	}
}

// findNode returns the first node in f that match accepts.
func findNode(f *ast.File, match func(ast.Node) bool) ast.Node {
	var found ast.Node
	ast.Walk(f, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if match(n) {
			found = n
			return false
		}
		return true
	})
	return found
}

// isType reports whether n is a node of type T.
func isType[T ast.Node](n ast.Node) bool {
	_, ok := n.(T)
	return ok
}

func TestParseFullProgram(t *testing.T) {
	src := `package main
import "std/net"
import "std/fmt"
import "std/sync"

interface Show {
    func Show() -> str
}

var total_conns: sync.Atomic<i64> = sync.StaticAtomic(0)

struct ConnCtx {
    conn: net.Conn
    limit: usize
    impl SyncSafe { }
    impl Show {
        func Show() -> str => f"conn[limit={self.limit}]"
    }
    func handle(sem: chan<bool>) {
        defer sem.Send(true)
        defer self.conn.Close()
        region {
            var buf = new Array<u8>(4096)
            for {
                match self.conn.Read(buf) {
                    Ok(n) => {
                        if n == 0: return
                        self.conn.Write(buf[0..n])
                    }
                    Err(e) => {
                        fmt.Println(f"read error: {e}")
                        return
                    }
                }
            }
        }
    }
}

func on_conn_panic(err: PanicError) {
    fmt.Println(f"coroutine panic: {err.Msg}")
}

func main() {
    var listener = net.Listen("0.0.0.0:9000").Unwrap()
    var sem = new chan<bool>(4096)
    for _ in 0..4096 {
        sem.Send(true)
    }
    region {
        for {
            var _, ok = sem.Recv()
            if !ok: return
            var conn = listener.Accept().Unwrap()
            total_conns.Add(1)
            var ctx = ConnCtx { conn: conn, limit: 4096 }
            spawn_watched ctx.handle(sem), func on_conn_panic
        }
    }
}
`
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	if f.Package == nil || f.Package.Name == nil || f.Package.Name.Name != "main" {
		t.Fatalf("package = %+v, want main", f.Package)
	}
	if len(f.Imports) != 3 {
		t.Fatalf("imports = %d, want 3", len(f.Imports))
	}
	if len(f.Decls) != 5 {
		t.Fatalf("decls = %d, want 5", len(f.Decls))
	}
	checks := []struct {
		name  string
		match func(ast.Node) bool
	}{
		{"struct", isType[*ast.StructDecl]},
		{"interface", isType[*ast.InterfaceDecl]},
		{"impl block", isType[*ast.ImplBlock]},
		{"field", isType[*ast.FieldDecl]},
		{"method", isType[*ast.FuncDecl]},
		{"var", isType[*ast.VarDecl]},
		{"match", isType[*ast.MatchStmt]},
		{"region", isType[*ast.RegionStmt]},
		{"for", isType[*ast.ForStmt]},
		{"defer", isType[*ast.DeferStmt]},
		{"spawn_watched", isType[*ast.SpawnExpr]},
		{"range", isType[*ast.RangeExpr]},
		{"call", isType[*ast.CallExpr]},
		{"selector", isType[*ast.SelectorExpr]},
		{"index", isType[*ast.IndexExpr]},
		{"variant pattern", isType[*ast.VariantPattern]},
		{"if", isType[*ast.IfStmt]},
	}
	for _, c := range checks {
		if findNode(f, c.match) == nil {
			t.Errorf("no %s node in the tree", c.name)
		}
	}
}

func TestTupleElementAccess(t *testing.T) {
	f, diags := parseSrc(t, "package main\nfunc f() {\nvar t = (1, \"a\")\nvar x = t.0\nvar y = t.1\n}\n")
	assertClean(t, diags)
	n := findNode(f, isType[*ast.TupleIndexExpr])
	idx, ok := n.(*ast.TupleIndexExpr)
	if !ok {
		t.Fatal("no tuple index node")
	}
	if idx.Index != 0 {
		t.Fatalf("index = %d, want 0", idx.Index)
	}
}

func TestArrayRepeatAndArrayType(t *testing.T) {
	f, diags := parseSrc(t, "package main\nvar zeros = [0] * 4096\nvar a: [u8; 4] = zeros\n")
	assertClean(t, diags)
	rep, ok := findNode(f, isType[*ast.ArrayRepeatExpr]).(*ast.ArrayRepeatExpr)
	if !ok {
		t.Fatal("no array repeat node")
	}
	if _, ok := rep.Count.(*ast.Literal); !ok {
		t.Fatalf("count = %T, want literal", rep.Count)
	}
	arr, ok := findNode(f, isType[*ast.ArrayType]).(*ast.ArrayType)
	if !ok {
		t.Fatal("no array type node")
	}
	if _, ok := arr.Len.(*ast.Literal); !ok {
		t.Fatalf("array length = %T, want literal", arr.Len)
	}
}

func TestArrayLiteralWithoutRepeat(t *testing.T) {
	f, diags := parseSrc(t, "package main\nvar xs = [1, 2, 3]\n")
	assertClean(t, diags)
	if n, ok := findNode(f, isType[*ast.ArrayLitExpr]).(*ast.ArrayLitExpr); !ok || len(n.Elems) != 3 {
		t.Fatalf("array literal = %#v, want three elements", n)
	}
}

func TestNestedTypeArgumentSplitting(t *testing.T) {
	f, diags := parseSrc(t, "package main\nvar m = new Map<str, Array<u8>>()\nvar n = new Map<Array<u8>, str>()\n")
	assertClean(t, diags)
	names := 0
	ast.Walk(f, func(n ast.Node) bool {
		if tn, ok := n.(*ast.TypeName); ok && tn.Name.Name == "Map" {
			names++
			if len(tn.Args) != 2 {
				t.Errorf("Map args = %d, want 2", len(tn.Args))
			}
		}
		return true
	})
	if names != 2 {
		t.Fatalf("Map type nodes = %d, want 2", names)
	}
}

func TestDocComments(t *testing.T) {
	src := "package main\n\n/// first line\n/// second line\nfunc F() {}\n"
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	fn, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok {
		t.Fatalf("decl = %T, want func", f.Decls[0])
	}
	if fn.Doc == nil {
		t.Fatal("doc comment not attached")
	}
	if want := []string{"first line", "second line"}; !slices.Equal(fn.Doc.Lines, want) {
		t.Fatalf("doc lines = %q, want %q", fn.Doc.Lines, want)
	}
}

func TestDetachedDocCommentIsReported(t *testing.T) {
	_, diags := parseSrc(t, "package main\n\n/// doc\n\nfunc F() {}\n")
	assertCodes(t, diags, diag.CodeDocComment)

	_, diags = parseSrc(t, "package main\nfunc F() {\n\t/// doc\n\tG()\n}\n")
	assertCodes(t, diags, diag.CodeDocComment)
}

func TestExpressionPrecedence(t *testing.T) {
	f, diags := parseSrc(t, "package main\nvar x = a + b * c\n")
	assertClean(t, diags)
	v := f.Decls[0].(*ast.VarDecl)
	add, ok := v.Value.(*ast.BinaryExpr)
	if !ok || add.Op.String() != "+" {
		t.Fatalf("top operator = %#v, want +", v.Value)
	}
	mul, ok := add.Y.(*ast.BinaryExpr)
	if !ok || mul.Op.String() != "*" {
		t.Fatalf("right operand = %#v, want a * product", add.Y)
	}
}

func TestIsBindsBelowUnary(t *testing.T) {
	f, diags := parseSrc(t, "package main\nfunc f(x: Conn) -> bool => !x is Conn\n")
	assertClean(t, diags)
	is := findNode(f, isType[*ast.IsExpr]).(*ast.IsExpr)
	if _, ok := is.X.(*ast.UnaryExpr); !ok {
		t.Fatalf("is operand = %T, want a unary expression", is.X)
	}
}

func TestAggregateLiteralAndBodyBlock(t *testing.T) {
	f, diags := parseSrc(t, "package main\nvar p = Point { x: 1, y: 2 }\nfunc f() {\nif p.x {\ng()\n}\n}\n")
	assertClean(t, diags)
	lit, ok := findNode(f, isType[*ast.AggregateLitExpr]).(*ast.AggregateLitExpr)
	if !ok {
		t.Fatal("no aggregate literal")
	}
	if len(lit.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(lit.Entries))
	}
	ifstmt := findNode(f, isType[*ast.IfStmt]).(*ast.IfStmt)
	if _, ok := ifstmt.Then.(*ast.BlockStmt); !ok {
		t.Fatalf("if body = %T, want a block", ifstmt.Then)
	}
}

func TestQualifiedAggregateLiteral(t *testing.T) {
	src := `package main

func f() -> net.Conn {
	return net.Conn { host: "local" }
}
`
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	lit, ok := findNode(f, isType[*ast.AggregateLitExpr]).(*ast.AggregateLitExpr)
	if !ok {
		t.Fatal("no aggregate literal")
	}
	if lit.Type == nil || lit.Type.Qual == nil || lit.Type.Qual.Name != "net" {
		t.Fatalf("type = %v, want a qualified name", lit.Type)
	}
	if lit.Type.Name == nil || lit.Type.Name.Name != "Conn" {
		t.Fatalf("type name = %v, want Conn", lit.Type.Name)
	}
}

func TestCAggregateLiteral(t *testing.T) {
	src := `package main

import c "cfg.h"

func f() -> c.Cfg {
	return c.Cfg { port: 1 }
}
`
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	lit, ok := findNode(f, isType[*ast.AggregateLitExpr]).(*ast.AggregateLitExpr)
	if !ok {
		t.Fatal("no aggregate literal")
	}
	if lit.Type == nil || lit.Type.Qual == nil || lit.Type.Qual.Name != "c" {
		t.Fatalf("type = %v, want c.Cfg", lit.Type)
	}
}

func TestLoopsAndLabels(t *testing.T) {
	src := `package main
func f() {
outer: for row in matrix {
    for cell in row {
        if skip(cell): continue outer
    }
}
for ready() { step() }
for { break }
}
`
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	var labeled *ast.ForStmt
	ast.Walk(f, func(n ast.Node) bool {
		if v, ok := n.(*ast.ForStmt); ok && v.Label != nil {
			labeled = v
		}
		return true
	})
	if labeled == nil || labeled.Label.Name != "outer" {
		t.Fatalf("labeled loop = %+v, want label outer", labeled)
	}
	if len(labeled.Bindings) != 1 {
		t.Fatalf("bindings = %d, want 1", len(labeled.Bindings))
	}
}

func TestSelectArms(t *testing.T) {
	src := `package main
func worker(high: chan<Job>, out: chan<i64>) {
    for {
        select {
            recv(high) -> j, ok {
                if !ok: return
                out.Send(run(j))
            }
            send(out, 1) { }
            timeout(d): return
            default: return
        }
    }
}
`
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	sel, ok := findNode(f, isType[*ast.SelectStmt]).(*ast.SelectStmt)
	if !ok {
		t.Fatal("no select statement")
	}
	if len(sel.Arms) != 4 {
		t.Fatalf("arms = %d, want 4", len(sel.Arms))
	}
	want := []ast.SelectKind{ast.SelectRecv, ast.SelectSend, ast.SelectTimeout, ast.SelectDefault}
	for i, arm := range sel.Arms {
		if arm.Kind != want[i] {
			t.Errorf("arm %d kind = %d, want %d", i, arm.Kind, want[i])
		}
	}
	if sel.Arms[0].RecvValue.Name != "j" || sel.Arms[0].RecvOk.Name != "ok" {
		t.Fatalf("recv bindings = %q, %q", sel.Arms[0].RecvValue.Name, sel.Arms[0].RecvOk.Name)
	}
}

func TestFStringInterpolation(t *testing.T) {
	f, diags := parseSrc(t, "package main\nfunc f() {\ng(f\"count = {n + 1}, name = {user.name}\")\n}\n")
	assertClean(t, diags)
	lit, ok := findNode(f, isType[*ast.Literal]).(*ast.Literal)
	if !ok {
		t.Fatal("no literal")
	}
	var exprs int
	for _, part := range lit.Parts {
		if part.X != nil {
			exprs++
		}
	}
	if exprs != 2 {
		t.Fatalf("interpolations = %d, want 2", exprs)
	}
}

func TestFunctionValueAndLambda(t *testing.T) {
	src := `package main
var g = func IsPositive
var h = func net.Listen
var keep = func(x: i64) -> bool => x > 0
`
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	ref, ok := findNode(f, isType[*ast.FuncRefExpr]).(*ast.FuncRefExpr)
	if !ok || ref.Name.Name != "IsPositive" {
		t.Fatalf("func ref = %+v, want IsPositive", ref)
	}
	lambda, ok := findNode(f, isType[*ast.LambdaExpr]).(*ast.LambdaExpr)
	if !ok || len(lambda.Params) != 1 || lambda.BodyExpr == nil {
		t.Fatalf("lambda = %+v", lambda)
	}
}

func TestSyntaxErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []diag.Code
	}{
		{
			name: "missing package",
			src:  "func F() {}\n",
			want: []diag.Code{diag.CodeMisplacedDecl},
		},
		{
			name: "duplicate package",
			src:  "package main\npackage other\n",
			want: []diag.Code{diag.CodeMisplacedDecl},
		},
		{
			name: "import after declaration",
			src:  "package main\nfunc F() {}\nimport \"std/os\"\n",
			want: []diag.Code{diag.CodeMisplacedDecl},
		},
		{
			name: "expression statement",
			src:  "package main\nfunc F() {\n\ta + b\n}\n",
			want: []diag.Code{diag.CodeInvalidStatement},
		},
		{
			name: "invalid assignment target",
			src:  "package main\nfunc F() {\n\tf() = 1\n}\n",
			want: []diag.Code{diag.CodeInvalidAssignTarget},
		},
		{
			name: "chained comparison",
			src:  "package main\nfunc F() -> bool => a == b == c\n",
			want: []diag.Code{diag.CodeChainedOperator},
		},
		{
			name: "float pattern",
			src:  "package main\nfunc F() {\n\tmatch x {\n\t\t1.5 => G()\n\t}\n}\n",
			want: []diag.Code{diag.CodeFloatPattern},
		},
		{
			name: "one-element tuple",
			src:  "package main\nvar t = (a,)\n",
			want: []diag.Code{diag.CodeUnitTuple},
		},
		{
			name: "declaration in single-line body",
			src:  "package main\nfunc F() {\n\tif x: var y = 1\n}\n",
			want: []diag.Code{diag.CodeDeclarationInSingleLineBody},
		},
		{
			name: "spawn of a non-call",
			src:  "package main\nfunc F() {\n\tregion {\n\t\tspawn x\n\t}\n}\n",
			want: []diag.Code{diag.CodeUnexpectedToken},
		},
		{
			name: "unexpected top-level token",
			src:  "package main\n== 1\n",
			want: []diag.Code{diag.CodeUnexpectedToken, diag.CodeUnexpectedToken},
		},
		{
			name: "repeated function qualifier",
			src:  "package main\ncomptime extern func F() {}\n",
			want: []diag.Code{diag.CodeUnexpectedToken},
		},
		{
			name: "address of outside a C call",
			src:  "package main\nfunc F() {\n\t&x\n}\n",
			want: []diag.Code{diag.CodeInvalidStatement},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, diags := parseSrc(t, c.src)
			assertCodes(t, diags, c.want...)
		})
	}
}

func TestInvalidUTF8HasNoTree(t *testing.T) {
	f, diags := Parse(source.NewFile("main.zyx", []byte{0xFF}))
	if f != nil {
		t.Fatal("tree built for an invalid UTF-8 file")
	}
	assertCodes(t, diags, diag.CodeInvalidUTF8)
}

func TestEveryNodeHasASpan(t *testing.T) {
	src := `package main
import "std/fmt"
const N: usize = 4
type Alias = i64
enum E { A  B(i64) }
struct S {
    x: i64
    const tag = "s"
    func M() -> i64 => self.x
}
func F(a: i64, b: i64 = 2) -> i64 {
    defer fmt.Println("done")
    var xs = [1, 2]
    for i, v in xs {
        if v > a { break }
    }
    return a + b
}
`
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	ast.Walk(f, func(n ast.Node) bool {
		if n.Span().File == nil {
			t.Errorf("%T has no file span", n)
		}
		if n.Span().End < n.Span().Start {
			t.Errorf("%T has an inverted span %v", n, n.Span())
		}
		return true
	})
}

func TestSemicolonSeparatesStatements(t *testing.T) {
	src := "package main\nfunc f() {\n\tg(); h();\n\tvar a: [u8; 4] = [0] * 4;\n\tg()\n}\n"
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	block, ok := findNode(f, isType[*ast.BlockStmt]).(*ast.BlockStmt)
	if !ok {
		t.Fatal("no block")
	}
	if len(block.Stmts) != 4 {
		t.Fatalf("statements = %d, want 4", len(block.Stmts))
	}
}

func TestSemicolonInArgumentListIsRejected(t *testing.T) {
	_, diags := parseSrc(t, "package main\nfunc f() {\n\tg(a; b)\n}\n")
	if len(diags) == 0 {
		t.Fatal("semicolon inside an argument list was accepted")
	}
}

func TestSemicolonBetweenTopLevelDecls(t *testing.T) {
	src := "package main;\nconst A = 1;\nvar B = 2;\nfunc f() {}\n"
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	if len(f.Decls) != 3 {
		t.Fatalf("decls = %d, want 3", len(f.Decls))
	}
}

func TestMidLineBodiesAndDelimiters(t *testing.T) {
	src := "package main\nfunc f() {\nif x { return }\nfor { break }\ng(Point { x: 1 })\nvar p = Point { q: Point { x: 1 } }\n}\n"
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	var ifstmt *ast.IfStmt
	ast.Walk(f, func(n ast.Node) bool {
		if v, ok := n.(*ast.IfStmt); ok && ifstmt == nil {
			ifstmt = v
		}
		return true
	})
	if ifstmt == nil {
		t.Fatal("no if statement")
	}
	if _, ok := ifstmt.Then.(*ast.BlockStmt); !ok {
		t.Fatalf("mid-line if body = %T, want a block", ifstmt.Then)
	}
	var aggregates int
	ast.Walk(f, func(n ast.Node) bool {
		if _, ok := n.(*ast.AggregateLitExpr); ok {
			aggregates++
		}
		return true
	})
	if aggregates != 3 {
		t.Fatalf("aggregate literals = %d, want 3", aggregates)
	}
}

func TestTypeForms(t *testing.T) {
	src := `package main
struct S {
    func Clone() -> Self => self
}
var a: [u8] = b"x"
var b: [u8; 4] = [0] * 4
var t: (i64, str) = (1, "a")
var d: func(i64) -> i64 = func IsPositive
var ch: chan<bool> = new chan<bool>(1)
var conn: net.Conn = new Conn()
var h: Array<i64> = [1]
`
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	var slice, array, tuple, fn, chanType, named bool
	ast.Walk(f, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.SliceType:
			slice = true
		case *ast.ArrayType:
			array = true
		case *ast.TupleType:
			tuple = true
		case *ast.FuncType:
			fn = true
		case *ast.ChanType:
			chanType = true
		case *ast.TypeName:
			named = true
		}
		return true
	})
	for name, ok := range map[string]bool{
		"slice": slice, "array": array, "tuple": tuple,
		"func type": fn, "chan type": chanType, "named": named,
	} {
		if !ok {
			t.Errorf("no %s type in the tree", name)
		}
	}
}

func TestExternUnion(t *testing.T) {
	src := "package main\nimport c \"sys/epoll.h\"\nextern union epoll_data_t {\n\tptr: raw\n\tfd: i32\n\tu64v: u64\n}\n"
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	u, ok := findNode(f, isType[*ast.UnionDecl]).(*ast.UnionDecl)
	if !ok {
		t.Fatal("no union declaration")
	}
	if u.Name.Name != "epoll_data_t" || len(u.Fields) != 3 {
		t.Fatalf("union %q has %d fields, want 3", u.Name.Name, len(u.Fields))
	}
}

func TestAddressOfForCCalls(t *testing.T) {
	src := "package main\nimport c \"sys/socket.h\"\nfunc f(fd: i32, buf: Array<u8>) {\n\tvar on: i32 = 1\n\tc.setsockopt(fd, c.SOL_SOCKET, c.SO_REUSEADDR, &on, 4)\n\tc.read(fd, &buf[0], 64)\n}\n"
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	if n, ok := findNode(f, isType[*ast.AddrOfExpr]).(*ast.AddrOfExpr); !ok {
		t.Fatal("no address-of node")
	} else if _, ok := n.X.(*ast.Ident); !ok {
		t.Fatalf("addressed operand = %T, want an identifier", n.X)
	}
	var indexed bool
	ast.Walk(f, func(n ast.Node) bool {
		if a, ok := n.(*ast.AddrOfExpr); ok {
			if _, ok := a.X.(*ast.IndexExpr); ok {
				indexed = true
			}
		}
		return true
	})
	if !indexed {
		t.Fatal("no address-of applied to an index expression")
	}
}

func TestTryCallAsStatement(t *testing.T) {
	src := "package main\nfunc F() -> Result<i64, Error> {\n\ttry G()\n\treturn Result.Ok(1)\n}\n"
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	if findNode(f, isType[*ast.TryExpr]) == nil {
		t.Fatal("no try expression")
	}
}

func TestExternAndCAbiFunctions(t *testing.T) {
	src := `package main
import c "stdio.h" {
	fast printf
}
extern func strlen(s: str) -> usize
fast extern func fwrite(p: raw, n: usize) -> usize
extern func printf(fmt: str, ...)
c func OnAccept(fd: i32) { }
func use() {
	c.printf("hello\n")
	fwrite(nil, 0)
}
`
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	var variadic, cAbi, extern, fast, fastList bool
	ast.Walk(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.ImportDecl:
			for _, id := range v.Fast {
				if id.Name == "printf" {
					fastList = true
				}
			}
		case *ast.Param:
			if v.Variadic {
				variadic = true
			}
		case *ast.FuncDecl:
			if v.Quals.CAbi {
				cAbi = true
			}
			if v.Quals.Extern {
				extern = true
			}
			if v.Quals.Fast {
				fast = true
			}
		}
		return true
	})
	for name, ok := range map[string]bool{
		"variadic param": variadic, "c ABI func": cAbi, "extern func": extern,
		"fast extern func": fast, "fast name in an import": fastList,
	} {
		if !ok {
			t.Errorf("no %s in the tree", name)
		}
	}
}

func TestTruncatedInputTerminates(t *testing.T) {
	src := "package main\nimport \"std/fmt\"\nstruct S {\n\tx: i64\n\tfunc M() -> i64 => self.x\n}\nfunc f(a: i64) -> i64 {\n\tmatch a {\n\t\t0 => return 1\n\t\t_ => return 2\n\t}\n\tfor i in 0..a {\n\t\tfmt.Println(f\"{i}\")\n\t}\n\tvar t = (a, \"s\")\n\treturn t.0\n}\n"
	for n := 0; n <= len(src); n++ {
		f, _ := Parse(source.NewFile("main.zyx", []byte(src[:n])))
		if f != nil && f.Package != nil && f.Package.Sp.File == nil {
			t.Fatalf("prefix %d: package has no file span", n)
		}
	}
}

func TestAggregateInsideHeaderDelimiters(t *testing.T) {
	src := "package main\nfunc f() {\nif g(Point { x: 1 }) {\n\th()\n}\nfor h(Point { x: 1 }) { break }\n}\n"
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	var aggregates int
	ast.Walk(f, func(n ast.Node) bool {
		if _, ok := n.(*ast.AggregateLitExpr); ok {
			aggregates++
		}
		return true
	})
	if aggregates != 2 {
		t.Fatalf("aggregate literals = %d, want 2", aggregates)
	}
}

func TestCommentsBetweenTokensAreIgnored(t *testing.T) {
	src := "package main\nfunc f( // note\n\ta: i64) -> i64 { // body\n\treturn a // tail\n}\n"
	f, diags := parseSrc(t, src)
	assertClean(t, diags)
	if len(f.Decls) != 1 {
		t.Fatalf("decls = %d, want 1", len(f.Decls))
	}
}

func TestBlockCommentsAreRejected(t *testing.T) {
	_, diags := parseSrc(t, "package main\nfunc f(/* nope */) {}\n")
	if len(diags) == 0 {
		t.Fatal("block comment accepted")
	}
}
