package mono

import (
	"strings"
	"testing"

	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// A generic function, a generic struct, and the call sites that use them.
func testProgram() *Program {
	file := &zyxc.File{Target: "amd64-unknown-linux-gnu", Module: "example.com/app"}
	file.Decls = []*zyxc.Node{
		zyxc.New("func", "twice", "plain", "example.com/app.twice").
			Child(
				zyxc.New("type-param", "T", "example.com/app.twice$T"),
				zyxc.New("param", "v", "T"),
				zyxc.New("result", "T"),
				zyxc.New("block").Child(
					zyxc.New("return").Child(
						zyxc.New("name", "v", "example.com/app.twice#2").Child(zyxc.New("type", "T")),
					),
				),
			),
		zyxc.New("struct", "Box", "example.com/app.Box").
			Child(
				zyxc.New("type-param", "T", "example.com/app.Box$T"),
				zyxc.New("field", "value", "T"),
				zyxc.New("func", "Get", "plain", "example.com/app.Box.Get").
					Child(
						zyxc.New("result", "T"),
						zyxc.New("block").Child(
							zyxc.New("return").Child(
								zyxc.New("field", "value", "example.com/app.Box.value").
									Child(zyxc.New("recv").Child(
										zyxc.New("name", "self", "example.com/app.Box.Get#1").Child(zyxc.New("type", "Box<T>")),
									)).
									Child(zyxc.New("type", "T")),
							),
						),
					),
			),
		zyxc.New("func", "main", "plain", "example.com/app.main").Mark("entry", "").
			Child(zyxc.New("block").
				Child(
					zyxc.New("bind", "n", "example.com/app.main#1").
						Child(zyxc.New("type", "i64")).
						Child(zyxc.New("call").
							Child(zyxc.New("callee").Child(
								zyxc.New("name", "twice", "example.com/app.twice").Child(zyxc.New("type", "func(i64) -> i64")),
							)).
							Child(zyxc.New("arg").Child(zyxc.New("lit", "2").Child(zyxc.New("type", "i64")))).
							Child(zyxc.New("type", "i64"))),
					zyxc.New("bind", "b", "example.com/app.main#2").
						Child(zyxc.New("type", "Box<i64>")).
						Child(zyxc.New("struct-lit", "Box<i64>").
							Child(zyxc.New("field-init", "value").Child(zyxc.New("lit", "3").Child(zyxc.New("type", "i64"))))),
					zyxc.New("expr-stmt").Child(
						zyxc.New("mcall", "Get", "example.com/app.Box.Get").
							Child(zyxc.New("recv").Child(
								zyxc.New("name", "b", "example.com/app.main#2").Child(zyxc.New("type", "Box<i64>")),
							)).
							Child(zyxc.New("callee-type", "func(Box<i64>) -> i64")).
							Child(zyxc.New("type", "i64")),
					),
				),
			),
	}
	return &Program{
		Target: "amd64-unknown-linux-gnu",
		Entry:  "example.com/app",
		Order:  []string{"example.com/app"},
		Files:  map[string]*zyxc.File{"example.com/app": file},
	}
}

// text renders a program's trees, so a test can assert what it holds.
func text(p *Program) string {
	var b strings.Builder
	for _, path := range p.Order {
		file := p.Files[path]
		if file == nil {
			continue
		}
		for _, decl := range file.Decls {
			decl.Sorted()
			writeNode(&b, decl, 0)
		}
	}
	return b.String()
}

// writeNode renders one node and its children.
func writeNode(b *strings.Builder, n *zyxc.Node, depth int) {
	if n == nil {
		return
	}
	b.WriteString(strings.Repeat("  ", depth))
	b.WriteString(n.String())
	b.WriteByte('\n')
	for _, child := range n.Children {
		writeNode(b, child, depth+1)
	}
}

func TestInstantiateBuildsOneInstancePerUse(t *testing.T) {
	p := testProgram()
	count, err := Instantiate(p)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("instances = %d, want 2: one for twice and one for Box", count)
	}
	out := text(p)
	for _, want := range []string{
		"twice$$",
		"struct Box example.com/app.Box$$",
		"func Get plain example.com/app.Box$$",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the program is missing %q:\n%s", want, out)
		}
	}
	// The generic declarations are gone and the call sites name instances.
	for _, unwanted := range []string{
		"func twice plain example.com/app.twice\n",
		"struct Box example.com/app.Box\n",
		"Box<i64>",
		"example.com/app.twice)",
		"func twice#",
		// An instance is a concrete declaration, so it declares no type
		// parameter and every type text in it is settled.
		"type-param",
		"type T\n",
		"Pair<T>",
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("the program still holds %q:\n%s", unwanted, out)
		}
	}
}

func TestInstantiateSharesOneInstancePerSignature(t *testing.T) {
	p := testProgram()
	if _, err := Instantiate(p); err != nil {
		t.Fatal(err)
	}
	out := text(p)
	if got := strings.Count(out, "plain example.com/app.twice$$"); got != 1 {
		t.Errorf("twice has %d instances, want 1:\n%s", got, out)
	}
	if got := strings.Count(out, "struct Box example.com/app.Box$$"); got != 1 {
		t.Errorf("Box has %d instances, want 1:\n%s", got, out)
	}
}

func TestInstanceNamesCarryTheirSignature(t *testing.T) {
	p := testProgram()
	if _, err := Instantiate(p); err != nil {
		t.Fatal(err)
	}
	first := instanceName("Box", []*types.Type{types.Int64}, p.Target)
	second := instanceName("Box", []*types.Type{types.Str}, p.Target)
	if first == second {
		t.Error("two instantiations of one generic must not share a name")
	}
	if !strings.HasPrefix(first, "Box$$") || len(first) != len("Box$$")+16 {
		t.Errorf("instance name %q is not the generic name and sixteen hexadecimal digits", first)
	}
	if again := instanceName("Box", []*types.Type{types.Int64}, p.Target); again != first {
		t.Errorf("the same signature must give the same name: %q and %q", first, again)
	}
}

// TestInstantiateRejectsAUseWithoutAnInstantiatedSignature checks that the
// type arguments of a use come from the callee's own annotation, and that a
// use which carries none is an error rather than a dangling name. Reading the
// arguments of the call instead would put a second, weaker copy of the front
// end's rule in this stage, and rewriting past the use would hand the backend
// a name that no declaration answers to.
func TestInstantiateRejectsAUseWithoutAnInstantiatedSignature(t *testing.T) {
	file := &zyxc.File{Target: "amd64-unknown-linux-gnu", Module: "example.com/app"}
	file.Decls = []*zyxc.Node{
		zyxc.New("func", "twice", "plain", "example.com/app.twice").
			Child(
				zyxc.New("type-param", "T", "example.com/app.twice$T"),
				zyxc.New("param", "v", "T"),
				zyxc.New("result", "T"),
				zyxc.New("block"),
			),
		zyxc.New("func", "main", "plain", "example.com/app.main").Mark("entry", "").
			Child(zyxc.New("block").Child(
				zyxc.New("expr-stmt").Child(
					zyxc.New("call").
						Child(zyxc.New("callee").Child(
							zyxc.New("name", "twice", "example.com/app.twice").Child(zyxc.New("type", "func(T) -> T")),
						)).
						Child(zyxc.New("arg").Child(zyxc.New("lit", "2").Child(zyxc.New("type", "i64")))).
						Child(zyxc.New("type", "i64")),
				),
			)),
	}
	p := &Program{
		Target: "amd64-unknown-linux-gnu",
		Entry:  "example.com/app",
		Order:  []string{"example.com/app"},
		Files:  map[string]*zyxc.File{"example.com/app": file},
	}
	_, err := Instantiate(p)
	if err == nil {
		t.Fatal("a use with no instantiated signature must be an error")
	}
	if !strings.Contains(err.Error(), "example.com/app.twice") {
		t.Errorf("the error does not name the declaration: %v", err)
	}
	if strings.Contains(text(p), "twice$$") {
		t.Error("a callee with the generic signature produced an instance")
	}
}

func TestInstantiateLeavesAConcreteProgramAlone(t *testing.T) {
	file := &zyxc.File{Target: "amd64-unknown-linux-gnu", Module: "example.com/app"}
	file.Decls = []*zyxc.Node{
		zyxc.New("func", "main", "plain", "example.com/app.main").Mark("entry", "").
			Child(zyxc.New("block")),
	}
	p := &Program{
		Target: "amd64-unknown-linux-gnu",
		Entry:  "example.com/app",
		Order:  []string{"example.com/app"},
		Files:  map[string]*zyxc.File{"example.com/app": file},
	}
	count, err := Instantiate(p)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("instances = %d, want none", count)
	}
	if len(p.Files["example.com/app"].Decls) != 1 {
		t.Error("a concrete program keeps its declarations")
	}
}
