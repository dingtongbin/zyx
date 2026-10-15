// Coroutine switching and handoff lowering.

package llvmgen

import (
	"fmt"
	"strings"

	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// spawn lowers a spawn expression. The arguments are copied into a block
// the coroutine owns, so the spawning function may return before the
// coroutine runs, and the runtime starts the coroutine in a region of its
// own.
func (g *generator) spawn(module string, n *zyxc.Node) *value {
	if hasMark(n, "watched") {
		g.fail(fmt.Errorf("llvmgen: spawn_watched is not lowered"))
		return nil
	}
	call := g.wrapped(n, "call")
	if call == nil {
		g.fail(fmt.Errorf("llvmgen: spawn needs a call"))
		return nil
	}
	sym := g.calleeSymbol(g.wrapped(call, "callee"))
	if sym == "" {
		g.fail(fmt.Errorf("llvmgen: spawn needs a named function"))
		return nil
	}
	decl := g.tab.decl(sym)
	if decl == nil || decl.Kind != "func" {
		g.fail(fmt.Errorf("llvmgen: %s is not a function", sym))
		return nil
	}
	if decl.Payload[1] != "plain" {
		g.fail(fmt.Errorf("llvmgen: only an ordinary function may start a coroutine"))
		return nil
	}
	dm := g.tab.module(sym)
	var ptypes []*types.Type
	for _, child := range decl.Children {
		if child.Kind == "param" && !isTypeParam(child) {
			ptypes = append(ptypes, g.paramType(child, dm))
		}
	}
	var result *types.Type
	if r := decl.First("result"); r != nil {
		result = g.typeOfNode(r, dm)
	}

	var args []*zyxc.Node
	for _, child := range call.Children {
		if child.Kind == "arg" {
			args = append(args, g.sole(child))
		}
	}
	if len(args) != len(ptypes) {
		g.fail(fmt.Errorf("llvmgen: %s takes %d arguments, %d given", sym, len(ptypes), len(args)))
		return nil
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
			return nil
		}
		slot := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, i32 0, i32 %d", slot, blockLL, base, i)
		g.storeInto(slot, ptypes[i], v)
	}

	thunk := g.spawnThunk(sym, ptypes, blockLL)
	g.declareRuntime("zyxrt_coro_spawn", "ptr", "ptr", "ptr")
	handle := g.newReg()
	g.emit("  %s = call ptr @zyxrt_coro_spawn(ptr @%s, ptr %s)", handle, thunk, base)
	return &value{text: handle, ll: "ptr", typ: types.JoinHandle.Instantiate(result)}
}

// spawnThunk emits the entry point a coroutine starts in: it reads the
// argument block back and calls the function.
func (g *generator) spawnThunk(sym string, ptypes []*types.Type, blockLL string) string {
	g.thunks++
	name := fmt.Sprintf("zyx.spawn.%d", g.thunks)
	saved := g.fn
	g.fn = &funcState{symbol: name, vars: map[string]*varSlot{}}
	g.fn.entry = g.newLabel("thunk")
	g.fn.ret = g.newLabel("thunk.done")

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
	fmt.Fprintf(&out, "define internal void @%s(ptr %%arg) {\n", name)
	out.WriteString(g.fn.entry + ":\n")
	out.WriteString(g.fn.allocas.String())
	out.WriteString(g.fn.body.String())
	out.WriteString("}\n")
	g.m.funcs.WriteString(out.String())
	g.fn = saved
	return name
}
