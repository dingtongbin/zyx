// Package llvmgen lowers .zyxc to LLVM IR text.
package llvmgen

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"zyx/internal/toolchain"
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// Generate lowers one package of a program to LLVM IR text. The package is
// defined in the module; every other package the program reaches is declared,
// so the module can call it and the linker brings the two together. An empty
// package defines everything, which is one module for the whole program.
//
// Every fact it uses comes from the trees of the program and from the layout
// algorithm; it never reads source and never resolves a name.
func Generate(p *Program, pkg string, target toolchain.Target) ([]byte, error) {
	if p.Entry == "" {
		return nil, fmt.Errorf("llvmgen: no entry package")
	}
	g := &generator{prog: p, pkg: pkg, target: target, m: newModule(llvmTriple(target))}
	g.tab = newTypeTable(p)
	g.run()
	if g.err != nil {
		return nil, g.err
	}
	return g.m.text(), nil
}

// A generator carries the state of one module's emission.
type generator struct {
	prog   *Program
	tab    *typeTable
	target toolchain.Target
	m      *module
	// pkg is the package this module holds, empty when it holds every package
	// of the program.
	pkg string
	// err holds the first failure; emission continues so that one message
	// reports the whole problem.
	err error
	// fn is the function being emitted, nil outside a function.
	fn *funcState
	// reg and label number the registers and labels of the module.
	reg    int
	labels int
	// thunks counts the coroutine entry points emitted.
	thunks int
}

// A pendingRelease is one object the analysis marked as no longer used.
type pendingRelease struct {
	typ  *types.Type
	slot string
}

// A funcState carries the state of one function's emission.
type funcState struct {
	symbol  string
	body    strings.Builder
	allocas strings.Builder
	vars    map[string]*varSlot
	result  *types.Type
	entry   string
	ret     string
	loops   []loopInfo
	// resultSlot holds the result of a function with a result, so that
	// every return path converges on one epilogue.
	resultSlot string
	// terminated marks a block that already ends in a terminator.
	terminated bool
	// regions maps the name a region carries, such as r0 or r1, onto the
	// local that holds its handle. An object is created in the region the
	// analysis settled as its home.
	regions map[string]string
	// pending holds the objects the current statement has finished with,
	// which are released when the statement ends. It belongs to the
	// function, because a release must land in the function that made the
	// object.
	pending []pendingRelease
}

// A varSlot is one local variable.
type varSlot struct {
	alloca string
	typ    *types.Type
}

// A loopInfo records the targets a break and a continue reach.
type loopInfo struct {
	label      string
	breakTo    string
	continueTo string
}

// fail records the first failure.
func (g *generator) fail(err error) {
	if g.err == nil && err != nil {
		g.err = err
	}
}

// emit writes one line of the current function.
func (g *generator) emit(format string, args ...any) {
	fmt.Fprintf(&g.fn.body, format+"\n", args...)
}

// terminator writes a line that ends a basic block. The objects the block
// finished with are released first, so no release lands after a terminator,
// where it could never run.
func (g *generator) terminator(format string, args ...any) {
	g.flushReleases(0)
	g.emit(format, args...)
	g.fn.terminated = true
}

// newReg returns a fresh register name.
func (g *generator) newReg() string {
	g.reg++
	return fmt.Sprintf("%%t%d", g.reg)
}

// newLabel returns a fresh label name.
func (g *generator) newLabel(prefix string) string {
	g.labels++
	return fmt.Sprintf("%s%d", prefix, g.labels)
}

// branch writes a line that ends a basic block inside an expression. A
// release the statement is still holding belongs to the statement, so it is
// not flushed here the way a statement-level terminator flushes it.
func (g *generator) branch(format string, args ...any) {
	g.emit(format, args...)
	g.fn.terminated = true
}

// label starts a basic block.
func (g *generator) label(name string) {
	g.emit("%s:", name)
	g.fn.terminated = false
}

// alloca reserves stack space for a local, in the entry block.
func (g *generator) alloca(t *types.Type) string {
	reg := g.newReg()
	fmt.Fprintf(&g.fn.allocas, "  %s = alloca %s\n", reg, g.llPass(t))
	return reg
}

// allocaSlots reserves an array of n slots of an LLVM type, in the entry
// block. A select builds the handles and directions it watches this way: an
// allocation inside a loop would be a dynamic one, which costs a stack probe
// on some targets and is not what a fixed count of arms needs.
func (g *generator) allocaSlots(ll string, n int) string {
	reg := g.newReg()
	fmt.Fprintf(&g.fn.allocas, "  %s = alloca [%d x %s]\n", reg, n, ll)
	return reg
}

// run emits the package this module holds, and declares what the others hold.
func (g *generator) run() {
	for _, path := range g.prog.Order {
		file := g.prog.Files[path]
		if file == nil {
			continue
		}
		if g.pkg == "" || path == g.pkg {
			for _, decl := range file.Decls {
				g.declaration(path, decl)
			}
			continue
		}
		for _, decl := range file.Decls {
			g.foreignDeclaration(path, decl)
		}
	}
	// Every module carries the initializers of the bindings it holds, which
	// the entry module then calls.
	g.emitPackageInit()
	if g.pkg == "" || g.pkg == g.prog.Entry {
		g.entryPoint()
	}
}

// foreignDeclaration declares one declaration of another package, so this
// module can call it. A declaration the module never uses costs a line.
func (g *generator) foreignDeclaration(module string, n *zyxc.Node) {
	switch n.Kind {
	case "func":
		g.foreignFunc(module, n)
	case "var":
		for i := 0; i+1 < len(n.Payload); i += 2 {
			g.foreignGlobal(module, n, n.Payload[i+1], n.Payload[i])
		}
	case "struct", "enum", "extern-struct", "extern-union":
		for _, child := range n.Children {
			switch child.Kind {
			case "func":
				g.foreignFunc(module, child)
			case "impl":
				for _, f := range child.Children {
					if f.Kind == "func" {
						g.foreignFunc(module, f)
					}
				}
			}
		}
	}
}

// foreignFunc declares a function of another package. The declaration spells
// its parameters the way the definition does, which is what lets the two
// modules meet at the link.
func (g *generator) foreignFunc(module string, n *zyxc.Node) {
	if len(n.Payload) < 3 {
		return
	}
	qual, sym := n.Payload[1], n.Payload[2]
	if qual == "comptime" || qual == "extern" {
		return
	}
	params, _, retLL := g.signature(module, n)
	g.declareZyx(g.linkName(sym), params, retLL)
}

// foreignGlobal declares a top-level binding of another package, and takes
// its initializer when this module is the one that runs the initializers.
// A binding the package keeps to itself is not declared at all: the front end
// does not let another package name it.
func (g *generator) foreignGlobal(module string, n *zyxc.Node, id, name string) {
	if !exported(name) {
		return
	}
	typ := g.typeOfNode(n.First("type"), module)
	if typ == nil {
		return
	}
	if g.m.declared[id] {
		return
	}
	g.m.declared[id] = true
	fmt.Fprintf(&g.m.funcs, "@%s = external global %s\n", llvmName(id), g.llType(typ))
}

// declareZyx declares a function of this program, which the module calls
// without defining it. Its parameters are spelled the way the definition
// spells them, which is the shape the two sides have to agree on.
func (g *generator) declareZyx(symbol string, params []paramInfo, retLL string) {
	if g.m.declared[symbol] {
		return
	}
	g.m.declared[symbol] = true
	parts := make([]string, 0, len(params))
	for _, p := range params {
		parts = append(parts, p.lls...)
	}
	fmt.Fprintf(&g.m.funcs, "declare %s @%s(%s)\n", retLL, llvmName(symbol), strings.Join(parts, ", "))
}

// exported reports whether a name is visible outside its package, which the
// language spells with a leading capital.
func exported(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// globalLinkage returns the linkage a top-level binding takes. A public name
// can be reached by another module, so the linker names it; a private one is
// only ever read by its own module, so the object file keeps it to itself.
func globalLinkage(name string) string {
	if exported(name) {
		return ""
	}
	return "internal "
}

// declaration emits one top-level declaration.
func (g *generator) declaration(module string, n *zyxc.Node) {
	switch n.Kind {
	case "func":
		g.function(module, n)
	case "const", "var":
		g.global(module, n)
	case "struct", "enum", "extern-struct", "extern-union":
		// The type is emitted on first use; the members may hold methods.
		for _, child := range n.Children {
			switch child.Kind {
			case "func":
				g.function(module, child)
			case "impl":
				for _, f := range child.Children {
					if f.Kind == "func" {
						g.function(module, f)
					}
				}
			}
		}
	}
}

// storeFat stores a parameter that the C ABI split into a pointer and a
// length into its local.
func (g *generator) storeFat(slot string, p *paramInfo) {
	reg := g.newReg()
	g.emit("  %s = insertvalue %s undef, ptr %s, 0", reg, g.llPass(p.typ), p.regs[0])
	reg2 := g.newReg()
	g.emit("  %s = insertvalue %s %s, %s %s, 1", reg2, g.llPass(p.typ), reg, g.ptrInt(), p.regs[1])
	g.emit("  store %s %s, ptr %s", g.llPass(p.typ), reg2, slot)
}
