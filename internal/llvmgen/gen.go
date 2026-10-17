// Package llvmgen lowers .zyxc to LLVM IR text.
package llvmgen

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"zyx/internal/layout"
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
	// bodyIndex holds every function the program defines, by symbol, and deepC
	// answers, per symbol, whether the function can reach C of unknown depth.
	// Both are what decides the stack a coroutine starts on.
	bodyIndex map[string]*zyxc.Node
	deepC     map[string]bool
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
	// frameBytes is how much stack the function's locals take, which is what
	// the prologue check asks the runtime to commit when the frame reaches
	// below what is committed.
	frameBytes uint64
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
	g.countFrame(t)
	return reg
}

// allocaSlots reserves an array of n slots of an LLVM type, in the entry
// block. A select builds the handles and directions it watches this way: an
// allocation inside a loop would be a dynamic one, which costs a stack probe
// on some targets and is not what a fixed count of arms needs.
func (g *generator) allocaSlots(ll string, n int) string {
	reg := g.newReg()
	fmt.Fprintf(&g.fn.allocas, "  %s = alloca [%d x %s]\n", reg, n, ll)
	g.fn.frameBytes += uint64(n) * llSlotBytes(ll)
	return reg
}

// countFrame adds the room a local of the type takes to the function's frame.
// The language size is used for every type, which over-counts a value that is
// stored as a reference: the check may commit more than the frame needs, which
// costs memory a coroutine that descends is using anyway.
func (g *generator) countFrame(t *types.Type) {
	size, err := layout.Size(t, g.tab, g.target.Layout())
	if err != nil {
		g.fn.frameBytes += 8
		return
	}
	g.fn.frameBytes += size
}

// llSlotBytes answers how many bytes one slot of an LLVM type takes, which is
// what the frame of an array of them holds.
func llSlotBytes(ll string) uint64 {
	switch ll {
	case "i8", "i1":
		return 1
	case "i16":
		return 2
	case "i32":
		return 4
	case "i64", "ptr", "double":
		return 8
	case "float":
		return 4
	}
	return 8
}

// stackCheck emits the prologue check a coroutine's stack needs on a target
// whose kernel does not grow one by itself. The low end of the frame the
// function is about to use is compared with the low end of the committed stack,
// which the runtime keeps in the thread record, and the runtime commits more of
// the reservation when the frame would fall past it; a frame that would pass
// the reservation is a panic. On a target that commits a page as it is touched
// there is nothing to check, and the compiler emits no code at all.
func (g *generator) stackCheck() string {
	if g.target.OS != "windows" || g.target.Arch != "amd64" {
		return ""
	}
	probe := g.newReg()
	fmt.Fprintf(&g.fn.allocas, "  %s = alloca i8\n", probe)
	addr := g.newReg()
	/* The frame the check stands for is what the language allocates plus the
	   room a frame needs beyond it: registers the compiler spills, the outgoing
	   argument area, and the alignment it keeps. What matters is that it is not
	   smaller than the frame, because the frame's low end is what is compared
	   and committed for. */
	frame := g.fn.frameBytes + 4096
	lowest := g.newReg()
	lowestPtr := g.newReg()
	limit := g.newReg()
	low := g.newReg()
	grow := g.newLabel("stack")
	body := g.newLabel("body")
	var b strings.Builder
	fmt.Fprintf(&b, "  %s = ptrtoint ptr %s to i64\n", addr, probe)
	fmt.Fprintf(&b, "  %s = sub i64 %s, %d\n", lowest, addr, frame)
	fmt.Fprintf(&b, "  %s = inttoptr i64 %s to ptr\n", lowestPtr, lowest)
	fmt.Fprintf(&b, "  %s = call i64 asm sideeffect \"movq %%gs:16, $0\", \"=r,~{dirflag},~{fpsr},~{flags}\"()\n", limit)
	fmt.Fprintf(&b, "  %s = icmp ult i64 %s, %s\n", low, lowest, limit)
	fmt.Fprintf(&b, "  br i1 %s, label %%%s, label %%%s\n", low, grow, body)
	fmt.Fprintf(&b, "%s:\n", grow)
	g.declareRuntime("zyxrt_stack_grow", "void", "ptr")
	fmt.Fprintf(&b, "  call void @zyxrt_stack_grow(ptr %s)\n", lowestPtr)
	fmt.Fprintf(&b, "  br label %%%s\n", body)
	fmt.Fprintf(&b, "%s:\n", body)
	return b.String()
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
	g.emitFrames()
}

// emitFrames emits the table a fault report reads to name the functions a stack
// passed through: one entry per function this module defined, holding its
// address, its name, and where it was declared. The names are the symbols,
// which is what the report shows, and the positions come from the front end,
// which records where every node came from. A module that carries its own names
// and positions needs no debug information to be installed and no tool to be
// found. The table is ordered by name, so that a module built twice is byte for
// byte the same.
func (g *generator) emitFrames() {
	if g.err != nil || len(g.m.defined) == 0 {
		return
	}
	names := make([]string, 0, len(g.m.defined))
	for symbol := range g.m.defined {
		names = append(names, symbol)
	}
	sort.Strings(names)
	g.declareRuntime("zyxrt_frames_register", "void", "ptr", "i64")
	var body strings.Builder
	fmt.Fprintf(&body, "@zyx.frames = internal constant [%d x { ptr, ptr, ptr }] [\n", len(names))
	for i, symbol := range names {
		comma := ","
		if i == len(names)-1 {
			comma = ""
		}
		fmt.Fprintf(&body, "  { ptr, ptr, ptr } { ptr @%s, ptr %s, ptr %s }%s\n",
			llvmName(symbol), g.m.stringGlobal(symbol), g.m.stringGlobal(g.m.defined[symbol]), comma)
	}
	body.WriteString("]\n")
	body.WriteString("define internal void @zyx.frames.init() {\n")
	fmt.Fprintf(&body, "  call void @zyxrt_frames_register(ptr @zyx.frames, i64 %d)\n", len(names))
	body.WriteString("  ret void\n}\n")
	body.WriteString("@llvm.global_ctors = appending global [1 x { i32, ptr, ptr }] " +
		"[{ i32, ptr, ptr } { i32 65535, ptr @zyx.frames.init, ptr null }]\n")
	g.m.funcs.WriteString(body.String())
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
