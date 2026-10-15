// Region enter and exit lowering.

package llvmgen

import (
	"fmt"
	"strings"

	"zyx/internal/layout"
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// openRegion records the defer mark of a region and returns the local that
// holds it.
func (g *generator) openRegion() string {
	g.declareRuntime("zyxrt_defer_mark", "ptr")
	reg := g.newReg()
	g.emit("  %s = call ptr @zyxrt_defer_mark()", reg)
	slot := g.alloca(types.Raw)
	g.emit("  store ptr %s, ptr %s", reg, slot)
	return slot
}

// closeRegion runs the deferred calls a region recorded, in reverse order.
func (g *generator) closeRegion(slot string) {
	g.declareRuntime("zyxrt_defer_run", "void", "ptr")
	reg := g.newReg()
	g.emit("  %s = load ptr, ptr %s", reg, slot)
	g.emit("  call void @zyxrt_defer_run(ptr %s)", reg)
}

// regionStmt lowers a region block. The block is a memory region of its own,
// and the deferred calls it holds run when it leaves.
func (g *generator) regionStmt(module string, n *zyxc.Node) {
	body := g.wrapped(n, "body")
	if !hasMark(n, "defers") {
		g.stmt(module, body)
		return
	}
	slot := g.openRegion()
	g.stmt(module, body)
	g.closeRegion(slot)
}

// deferStmt lowers a deferred call. The arguments are evaluated where the
// statement stands, which is what the language requires, and the call itself
// waits for the region to leave.
func (g *generator) deferStmt(module string, n *zyxc.Node) {
	call := g.wrapped(n, "call")
	if call == nil {
		g.fail(fmt.Errorf("llvmgen: defer needs a call"))
		return
	}
	sym := g.calleeSymbol(g.wrapped(call, "callee"))
	if sym == "" {
		g.fail(fmt.Errorf("llvmgen: defer needs a named function"))
		return
	}
	decl := g.tab.decl(sym)
	if decl == nil || decl.Kind != "func" {
		g.fail(fmt.Errorf("llvmgen: %s is not a function", sym))
		return
	}
	if decl.Payload[1] != "plain" {
		g.fail(fmt.Errorf("llvmgen: only an ordinary function may be deferred"))
		return
	}
	dm := g.tab.module(sym)
	var ptypes []*types.Type
	if recv := g.receiverType(decl); recv != nil {
		ptypes = append(ptypes, recv)
	}
	for _, child := range decl.Children {
		if child.Kind == "param" && !isTypeParam(child) {
			ptypes = append(ptypes, g.paramType(child, dm))
		}
	}
	var args []*zyxc.Node
	for _, child := range call.Children {
		if child.Kind == "arg" {
			args = append(args, g.sole(child))
		}
	}
	if len(args) != len(ptypes) {
		g.fail(fmt.Errorf("llvmgen: %s takes %d arguments, %d given", sym, len(ptypes), len(args)))
		return
	}

	// The argument block is laid out as the tuple of the parameter types,
	// which gives the same offsets the thunk reads back.
	block := types.TupleOf(ptypes...)
	blockLL := g.llType(block)
	if len(ptypes) == 0 {
		blockLL = "{}"
	}
	base := g.allocate(block)
	for i, arg := range args {
		v := g.expr(module, arg, ptypes[i])
		if v == nil {
			return
		}
		slot := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, i32 0, i32 %d", slot, blockLL, base, i)
		g.storeInto(slot, ptypes[i], v, true)
	}
	thunk := g.deferThunk(sym, ptypes, blockLL)
	g.declareRuntime("zyxrt_defer_push", "void", "ptr", "ptr")
	g.emit("  call void @zyxrt_defer_push(ptr @%s, ptr %s)", thunk, base)
}

// deferThunk emits the entry a deferred call runs through: it reads the
// argument block back and calls the function.
func (g *generator) deferThunk(sym string, ptypes []*types.Type, blockLL string) string {
	g.thunks++
	name := fmt.Sprintf("zyx.defer.%d", g.thunks)
	saved := g.fn
	g.fn = &funcState{symbol: name, vars: map[string]*varSlot{}}
	g.fn.entry = g.newLabel("defer")
	g.fn.ret = g.newLabel("defer.done")

	var parts []string
	for i, typ := range ptypes {
		slot := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %%arg, i32 0, i32 %d", slot, blockLL, i)
		reg := g.newReg()
		g.emit("  %s = load %s, ptr %s", reg, g.llPass(typ), slot)
		parts = append(parts, g.llPass(typ)+" "+reg)
	}
	g.emit("  call void @%s(%s)", llvmName(sym), strings.Join(parts, ", "))
	g.terminator("  ret void")

	var out strings.Builder
	fmt.Fprintf(&out, "define internal void @%s(ptr %%arg) uwtable {\n", name)
	out.WriteString(g.fn.entry + ":\n")
	out.WriteString(g.fn.allocas.String())
	out.WriteString(g.fn.body.String())
	out.WriteString("}\n")
	g.m.funcs.WriteString(out.String())
	g.fn = saved
	return name
}

// release returns an object to its region at the point the analysis marked as
// its last use, so the space is reused within the region rather than at its
// exit. The object must be one this backend allocated: a handle the runtime
// owns is released by the runtime, which the container runtime does.
func (g *generator) release(typ *types.Type, addr string) {
	if addr == "" {
		return
	}
	if !releasable(typ) {
		return
	}
	size, err := layout.Size(typ, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("llvmgen: the size of %s: %w", typ, err))
		return
	}
	g.declareRuntime("zyxrt_region_current", "ptr")
	g.declareRuntime("zyxrt_region_release", "void", "ptr", "ptr", g.ptrInt())
	region := g.newReg()
	g.emit("  %s = call ptr @zyxrt_region_current()", region)
	g.emit("  call void @zyxrt_region_release(ptr %s, ptr %s, %s %d)",
		region, addr, g.ptrInt(), size)
}

// allocate creates an object of a type in the region of the running
// coroutine and returns its address.
func (g *generator) allocate(typ *types.Type) string {
	size, err := layout.Size(typ, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("llvmgen: the size of %s: %w", typ, err))
		return "null"
	}
	align, err := layout.Align(typ, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("llvmgen: the alignment of %s: %w", typ, err))
		return "null"
	}
	g.declareRuntime("zyxrt_region_alloc_current", "ptr", "i64", "i64")
	reg := g.newReg()
	g.emit("  %s = call ptr @zyxrt_region_alloc_current(i64 %d, i64 %d)", reg, size, align)
	return reg
}
