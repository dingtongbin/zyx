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

// regionStmt lowers a region block. The block owns a region of its own, so
// every object created inside it is released when it leaves. The order is the
// one the language fixes: wait for the coroutines the block started, run its
// deferred calls, then release the memory.
func (g *generator) regionStmt(module string, n *zyxc.Node) {
	body := g.wrapped(n, "body")
	region := g.newReg()
	g.declareRuntime("zyxrt_region_new", "ptr")
	g.emit("  %s = call ptr @zyxrt_region_new()", region)
	slot := g.alloca(types.Raw)
	g.emit("  store ptr %s, ptr %s", region, slot)
	g.declareRuntime("zyxrt_region_enter", "void", "ptr")
	g.emit("  call void @zyxrt_region_enter(ptr %s)", region)
	if name := regionName(n); name != "" {
		g.nameRegion(name, slot)
	}

	deferSlot := ""
	if hasMark(n, "defers") {
		deferSlot = g.openRegion()
	}
	g.stmt(module, body)

	g.declareRuntime("zyxrt_region_wait", "void", "ptr")
	g.emit("  call void @zyxrt_region_wait(ptr %s)", region)
	if deferSlot != "" {
		g.closeRegion(deferSlot)
	}
	g.declareRuntime("zyxrt_region_leave", "void")
	g.emit("  call void @zyxrt_region_leave()")
	g.declareRuntime("zyxrt_region_free", "void", "ptr")
	g.emit("  call void @zyxrt_region_free(ptr %s)", region)
}

// regionName returns the name a region block carries, such as r1, which its
// enter mark writes.
func regionName(n *zyxc.Node) string {
	for _, m := range n.Marks {
		if m.Name == "region:enter" {
			return m.Arg
		}
	}
	return ""
}

// nameRegion records the local that holds a region's handle.
func (g *generator) nameRegion(name, slot string) {
	if g.fn.regions == nil {
		g.fn.regions = map[string]string{}
	}
	g.fn.regions[name] = slot
}

// regionSlot returns the local that holds the handle of a named region, or
// the empty string when the name is not one this function entered.
func (g *generator) regionSlot(name string) string {
	return g.fn.regions[name]
}

// openHome records the region the function itself runs in, which is the home
// of an object that outlives a nested block.
func (g *generator) openHome() {
	g.declareRuntime("zyxrt_region_current", "ptr")
	reg := g.newReg()
	g.emit("  %s = call ptr @zyxrt_region_current()", reg)
	slot := g.alloca(types.Raw)
	g.emit("  store ptr %s, ptr %s", reg, slot)
	g.nameRegion("r0", slot)
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
func (g *generator) release(typ *types.Type, slot string) {
	if slot == "" || !releasable(typ) {
		return
	}
	size, err := layout.Size(typ, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("llvmgen: the size of %s: %w", typ, err))
		return
	}
	// The address is read where the release happens, because the block that
	// computed it need not dominate this one.
	addr := g.newReg()
	g.emit("  %s = load ptr, ptr %s", addr, slot)
	g.declareRuntime("zyxrt_region_current", "ptr")
	g.declareRuntime("zyxrt_region_release", "void", "ptr", "ptr", g.ptrInt())
	region := g.newReg()
	g.emit("  %s = call ptr @zyxrt_region_current()", region)
	g.emit("  call void @zyxrt_region_release(ptr %s, ptr %s, %s %d)",
		region, addr, g.ptrInt(), size)
}

// allocateIn creates an object of a type in the region the analysis settled
// as its home and returns its address. An object whose home is the function's
// own region outlives a nested block, so it must not be created in the block.
func (g *generator) allocateIn(n *zyxc.Node, typ *types.Type) string {
	name := ""
	for _, m := range n.Marks {
		switch m.Name {
		case "home":
			name = m.Arg
		case "shared-bind":
			// An object more than one region uses binds to the region that
			// contains them all, which outlives every use.
			name = m.Arg
		}
	}
	if slot := g.regionSlot(name); slot != "" {
		size, err := layout.Size(typ, g.tab, g.target.Layout())
		if err != nil {
			g.fail(fmt.Errorf("llvmgen: the size of %s: %w", typ, err))
			return ""
		}
		align, err := layout.Align(typ, g.tab, g.target.Layout())
		if err != nil {
			g.fail(fmt.Errorf("llvmgen: the alignment of %s: %w", typ, err))
			return ""
		}
		g.declareRuntime("zyxrt_region_alloc", "ptr", "ptr", g.ptrInt(), g.ptrInt())
		region := g.newReg()
		g.emit("  %s = load ptr, ptr %s", region, slot)
		reg := g.newReg()
		g.emit("  %s = call ptr @zyxrt_region_alloc(ptr %s, %s %d, %s %d)",
			reg, region, g.ptrInt(), size, g.ptrInt(), align)
		return reg
	}
	return g.allocate(typ)
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
