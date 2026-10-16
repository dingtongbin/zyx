// Package llvmgen lowers .zyxc to LLVM IR text.
package llvmgen

import (
	"fmt"
	"strings"

	"zyx/internal/toolchain"
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// Generate lowers a program to LLVM IR text. Every fact it uses comes from
// the trees of the program and from the layout algorithm; it never reads
// source and never resolves a name.
func Generate(p *Program, target toolchain.Target) ([]byte, error) {
	if p.Entry == "" {
		return nil, fmt.Errorf("llvmgen: no entry package")
	}
	g := &generator{prog: p, target: target, m: newModule(llvmTriple(target))}
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

// run emits every package of the program in dependency order.
func (g *generator) run() {
	for _, path := range g.prog.Order {
		file := g.prog.Files[path]
		if file == nil {
			continue
		}
		for _, decl := range file.Decls {
			g.declaration(path, decl)
		}
	}
	g.entryPoint()
}

// declaration emits one top-level declaration.
func (g *generator) declaration(module string, n *zyxc.Node) {
	switch n.Kind {
	case "func":
		g.function(module, n)
	case "const", "var":
		g.global(module, n)
	case "struct", "enum":
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
