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
		g.storeInto(slot, ptypes[i], v, true)
	}

	thunk := g.spawnThunk(sym, ptypes, blockLL, result)
	g.declareRuntime("zyxrt_coro_spawn", "ptr", "ptr", "ptr")
	handle := g.newReg()
	g.emit("  %s = call ptr @zyxrt_coro_spawn(ptr @%s, ptr %s)", handle, thunk, base)
	return &value{text: handle, ll: "ptr", typ: types.JoinHandle.Instantiate(result)}
}

// join lowers Join and TryJoin on a coroutine handle. A coroutine reports
// either the value it produced or the exception it raised, and the handle
// turns that into a Result.
func (g *generator) join(module string, n *zyxc.Node) *value {
	name := ""
	if len(n.Payload) > 0 {
		name = n.Payload[0]
	}
	if name != "Join" {
		g.fail(fmt.Errorf("llvmgen: %s on a coroutine handle is not lowered", name))
		return nil
	}
	handle := g.load(g.expr(module, g.wrapped(n, "recv"), nil))
	if handle == nil {
		return nil
	}
	typ := g.typeOfNode(n.First("type"), module)
	if typ == nil || typ.Kind != types.KindEnum || len(typ.Args) == 0 {
		g.fail(fmt.Errorf("llvmgen: the result of Join is not settled"))
		return nil
	}
	produced := typ.Args[0]
	if !g.crossesAsRegister(produced) {
		g.fail(fmt.Errorf("llvmgen: a coroutine result of %s needs a boundary copy, which is not lowered", produced))
		return nil
	}
	panicSlot := g.alloca(types.Raw)
	g.declareRuntime("zyxrt_coro_join", "ptr", "ptr", "ptr")
	raw := g.newReg()
	g.emit("  %s = call ptr @zyxrt_coro_join(ptr %s, ptr %s)", raw, handle.text, panicSlot)
	raised := g.newReg()
	g.emit("  %s = load ptr, ptr %s", raised, panicSlot)

	index, _, ok := g.variantOf(typ, "Err")
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no variant Err", typ))
		return nil
	}
	obj := g.allocate(typ)
	tagType := g.tagType(g.tagSize(typ))
	isErr := g.newReg()
	g.emit("  %s = icmp ne ptr %s, null", isErr, raised)
	tag := g.newReg()
	g.emit("  %s = select i1 %s, %s %d, %s 0", tag, isErr, tagType, index, tagType)
	g.emit("  store %s %s, ptr %s", tagType, tag, obj)

	// The union holds one payload, so the two outcomes are written on their
	// own paths.
	errLabel := g.newLabel("join.err")
	okLabel := g.newLabel("join.ok")
	endLabel := g.newLabel("join.end")
	g.terminator("  br i1 %s, label %%%s, label %%%s", isErr, errLabel, okLabel)

	g.label(okLabel)
	g.emit("  store %s %s, ptr %s", g.llPass(produced), g.fromRaw(raw, produced),
		g.payloadElement(obj, typ, []*types.Type{produced}, 0))
	g.terminator("  br label %%%s", endLabel)

	g.label(errLabel)
	g.emit("  store ptr %s, ptr %s", raised,
		g.payloadElement(obj, typ, []*types.Type{types.PanicError}, 0))
	g.terminator("  br label %%%s", endLabel)

	g.label(endLabel)
	return &value{text: obj, ll: "ptr", typ: typ}
}

// payloadElement returns the address of one element of a variant payload
// inside an enum object. Every variant shares the union that follows the tag.
func (g *generator) payloadElement(obj string, typ *types.Type, payload []*types.Type, elem int) string {
	union := g.newReg()
	g.emit("  %s = getelementptr inbounds %s, ptr %s, i32 0, i32 1", union, g.llType(typ), obj)
	slot := g.newReg()
	g.emit("  %s = getelementptr inbounds %s, ptr %s, i32 0, i32 %d",
		slot, g.llType(types.TupleOf(payload...)), union, elem)
	return slot
}

// fromRaw turns the pointer a coroutine reported back into a value of its
// type.
func (g *generator) fromRaw(raw string, typ *types.Type) string {
	reg := g.newReg()
	g.emit("  %s = ptrtoint ptr %s to %s", reg, raw, g.llPass(typ))
	return reg
}

// crossesAsRegister reports whether a value crosses a coroutine boundary as
// itself. An object would have to be copied out of the coroutine's region,
// which the boundary marks settle, and a float would have to be reinterpreted
// rather than cast.
func (g *generator) crossesAsRegister(t *types.Type) bool {
	return strings.HasPrefix(g.llPass(t), "i")
}

// spawnThunk emits the entry point a coroutine starts in: it reads the
// argument block back, calls the function, and records the value the function
// produced so that the handle's Join can report it.
func (g *generator) spawnThunk(sym string, ptypes []*types.Type, blockLL string, result *types.Type) string {
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
	if result == nil {
		g.emit("  call void @%s(%s)", llvmName(sym), strings.Join(parts, ", "))
	} else {
		reg := g.newReg()
		g.emit("  %s = call %s @%s(%s)", reg, g.llPass(result), llvmName(sym), strings.Join(parts, ", "))
		cast := g.newReg()
		g.emit("  %s = inttoptr %s %s to ptr", cast, g.llPass(result), reg)
		g.declareRuntime("zyxrt_coro_set_result", "void", "ptr")
		g.emit("  call void @zyxrt_coro_set_result(ptr %s)", cast)
	}
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
