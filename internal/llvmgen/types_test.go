package llvmgen

import (
	"strings"
	"testing"

	"zyx/internal/layout"
	"zyx/internal/toolchain"
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// testProgram builds the trees of a small module.
func testProgram() *Program {
	file := &zyxc.File{Target: "amd64-unknown-linux-gnu", Module: "example.com/app"}
	file.Decls = []*zyxc.Node{
		zyxc.New("import", "std/fmt"),
		zyxc.New("struct", "Point", "example.com/app.Point").
			Child(zyxc.New("field-decl", "x", "i64", "0"), zyxc.New("field-decl", "y", "f64", "0")),
		zyxc.New("struct", "Line", "example.com/app.Line").
			Child(
				zyxc.New("field-decl", "from", "Point", "0"),
				zyxc.New("field-decl", "to", "Point", "16"),
				zyxc.New("field-decl", "tag", "str", "32"),
			),
		zyxc.New("func", "main", "plain", "example.com/app.main").
			Mark("entry", "").
			Child(zyxc.New("block")),
	}
	return &Program{
		Entry: "example.com/app",
		Order: []string{"example.com/app"},
		Files: map[string]*zyxc.File{"example.com/app": file},
	}
}

// linuxAMD64 is the target the tests lay out for.
func linuxAMD64(t *testing.T) toolchain.Target {
	t.Helper()
	target, err := toolchain.ParseTarget("amd64-unknown-linux-gnu")
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestTypeTextRoundTrips(t *testing.T) {
	tab := newTypeTable(testProgram())
	cases := []string{
		"i8", "u32", "i64", "usize", "f32", "f64", "bool", "char", "str", "raw",
		"[i64;4]", "[u8]", "(i64,str)", "func(i64)->bool", "func()",
		"chan<i32>", "Array<u8>", "Map<str,i64>", "Point",
	}
	for _, text := range cases {
		ty, err := tab.parse("example.com/app", text)
		if err != nil {
			t.Errorf("parse(%q): %v", text, err)
			continue
		}
		if got := ty.Canonical(); got != text {
			t.Errorf("parse(%q).Canonical() = %q", text, got)
		}
	}
}

func TestTypeTextRejectsGarbage(t *testing.T) {
	tab := newTypeTable(testProgram())
	for _, text := range []string{"", "i64 i64", "[i64", "func(i64", "chan<i32", "Unknown"} {
		if _, err := tab.parse("example.com/app", text); err == nil {
			t.Errorf("parse(%q) must fail", text)
		}
	}
}

func TestNamedTypeResolvesItsDeclaration(t *testing.T) {
	tab := newTypeTable(testProgram())
	ty, err := tab.parse("example.com/app", "Line")
	if err != nil {
		t.Fatal(err)
	}
	if ty.Kind != types.KindStruct {
		t.Fatalf("kind = %v, want struct", ty.Kind)
	}
	fields, ok := tab.Fields(ty)
	if !ok {
		t.Fatal("the fields of Line are not settled")
	}
	if len(fields) != 3 || fields[0].Name != "from" || fields[2].Type.Kind != types.KindStr {
		t.Fatalf("fields = %v", fields)
	}
}

func TestStructLayoutPlacesInlineFields(t *testing.T) {
	tab := newTypeTable(testProgram())
	target := linuxAMD64(t)
	ty, err := tab.parse("example.com/app", "Line")
	if err != nil {
		t.Fatal(err)
	}
	size, err := layout.Size(ty, tab, target.Layout())
	if err != nil {
		t.Fatal(err)
	}
	// A field of a struct type is a data block inside its holder, so two
	// Point blocks and one fat pointer come to forty-eight bytes.
	if size != 48 {
		t.Errorf("size = %d, want 48", size)
	}
	placed, err := layout.Fields(ty, tab, target.Layout())
	if err != nil {
		t.Fatal(err)
	}
	if len(placed) != 3 || placed[0].Offset != 0 || placed[1].Offset != 16 || placed[2].Offset != 32 {
		t.Errorf("offsets = %v", placed)
	}
}

func TestPassTypeOfAnAggregateIsAReference(t *testing.T) {
	p := testProgram()
	target := linuxAMD64(t)
	g := &generator{prog: p, target: target, m: newModule("x")}
	g.tab = newTypeTable(p)
	point, err := g.tab.parse("example.com/app", "Point")
	if err != nil {
		t.Fatal(err)
	}
	if got := g.llPass(point); got != "ptr" {
		t.Errorf("a reference struct passes as %q, want ptr", got)
	}
	if got := g.llType(point); !strings.HasPrefix(got, "%s.") {
		t.Errorf("the data block of a struct is named, got %q", got)
	}
	if got := g.llPass(types.Str); got != "{ ptr, i64 }" {
		t.Errorf("a string passes as %q", got)
	}
}

func TestGenerateEmitsTheEntryPoint(t *testing.T) {
	p := testProgram()
	target := linuxAMD64(t)
	ir, err := Generate(p, "", target)
	if err != nil {
		t.Fatal(err)
	}
	text := string(ir)
	for _, want := range []string{
		"target triple = \"x86_64-unknown-linux-gnu\"",
		"define i32 @main()",
		"@zyxrt_run",
		"example.com/app.main",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the module is missing %q:\n%s", want, text)
		}
	}
}

func TestGenerateNeedsAnEntryPackage(t *testing.T) {
	if _, err := Generate(&Program{}, "", linuxAMD64(t)); err == nil {
		t.Fatal("a program without an entry package must fail")
	}
}

func TestLlvMTripleCoversEveryPlatform(t *testing.T) {
	cases := map[string]string{
		"amd64-unknown-linux-gnu":   "x86_64-unknown-linux-gnu",
		"arm64-apple-darwin":        "aarch64-apple-darwin",
		"amd64-pc-windows-msvc":     "x86_64-pc-windows-msvc",
		"arm64-unknown-linux-gnu":   "aarch64-unknown-linux-gnu",
		"wasm32-unknown-unknown":    "wasm32-unknown-wasi",
		"i386-unknown-linux-gnu":    "i686-unknown-linux-gnu",
		"arm-unknown-linux-gnu":     "armv7-unknown-linux-gnu",
		"arm64-unknown-linux-andro": "aarch64-unknown-linux-gnu",
	}
	for triple, want := range cases {
		target, err := toolchain.ParseTarget(triple)
		if err != nil {
			t.Errorf("ParseTarget(%q): %v", triple, err)
			continue
		}
		if got := llvmTriple(target); got != want {
			t.Errorf("llvmTriple(%q) = %q, want %q", triple, got, want)
		}
	}
}
