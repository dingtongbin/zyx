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
	watched := hasMark(n, "watched")
	call := g.wrapped(n, "call")
	if call == nil {
		g.fail(fmt.Errorf("llvmgen: spawn needs a call"))
		return nil
	}
	if sym, ok := g.startedFunction(call); ok {
		return g.spawnNamed(module, n, call, sym, watched)
	}
	return g.spawnValue(module, n, call, watched)
}

// startedFunction answers the symbol of the ordinary function a call names
// directly, and whether it names one. A call the program reaches through a
// value — a function held in a variable or held as a parameter, or a bound
// method value — answers false, because what is started is that value.
func (g *generator) startedFunction(call *zyxc.Node) (string, bool) {
	sym := g.calleeSymbol(g.wrapped(call, "callee"))
	if sym == "" {
		return "", false
	}
	decl := g.tab.decl(sym)
	if decl == nil || decl.Kind != "func" {
		return "", false
	}
	return sym, true
}

// spawnNamed starts a coroutine that runs a call to a function this program
// declares.
func (g *generator) spawnNamed(module string, n *zyxc.Node, call *zyxc.Node, sym string, watched bool) *value {
	decl := g.tab.decl(sym)
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
	block := g.blockType(ptypes)
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
		if hasMark(arg, "boundary-clone") && byAddress(v.typ) {
			// The object crosses into another coroutine, so the copy is
			// what the receiving coroutine owns.
			if v = g.cloneInto(v); v == nil {
				return nil
			}
		}
		g.storeArgument(base, blockLL, i, ptypes[i], v)
	}

	thunk := g.spawnThunk(sym, ptypes, blockLL, result)
	return g.spawnHandle(n, module, watched, thunk, base, result, g.stackTier(sym))
}

// spawnValue starts a coroutine that runs a call the program reaches through a
// function value: a variable or parameter of function type, or a bound method
// value. The value is what the block carries beside the arguments, and the
// thunk calls it with the receiver it holds.
func (g *generator) spawnValue(module string, n *zyxc.Node, call *zyxc.Node, watched bool) *value {
	callee := g.wrapped(call, "callee")
	if call.Kind == "mcall" {
		// A method call starts the bound method value the call expresses:
		// the value carries the method's code and the receiver it was taken
		// from, which the thunk reaches the method through.
		callee = zyxc.New("method-value", call.Payload...)
		if recv := call.First("recv"); recv != nil {
			callee.Child(recv)
		}
		if ct := call.First("callee-type"); ct != nil && len(ct.Payload) > 0 {
			// The call records the signature the method is reached with,
			// which is the type the value that names it has.
			callee.Child(zyxc.New("type", ct.Payload[0]))
		}
	}
	if callee == nil {
		g.fail(fmt.Errorf("llvmgen: spawn needs a call"))
		return nil
	}
	sig := g.typeOfNode(callee.First("type"), module)
	if sig == nil || sig.Kind != types.KindFunc {
		sig = g.typeOfNode(call.First("callee-type"), module)
	}
	if sig == nil || sig.Kind != types.KindFunc {
		g.fail(fmt.Errorf("llvmgen: the signature of the started call is not settled"))
		return nil
	}
	args := g.argNodes(call)
	if len(args) != len(sig.Elems) {
		g.fail(fmt.Errorf("llvmgen: the started call takes %d arguments, %d given", len(sig.Elems), len(args)))
		return nil
	}
	// The first slot carries the function value, so the thunk knows what to
	// call and with which receiver.
	ptypes := append([]*types.Type{types.FuncOf(sig.Result, sig.Elems...)}, sig.Elems...)
	block := g.blockType(ptypes)
	blockLL := g.llType(block)
	base := g.allocate(block)
	fn := g.load(g.expr(module, callee, nil))
	if fn == nil {
		return nil
	}
	g.storeArgument(base, blockLL, 0, ptypes[0], fn)
	for i, arg := range args {
		v := g.expr(module, arg, ptypes[i+1])
		if v == nil {
			return nil
		}
		if hasMark(arg, "boundary-clone") && byAddress(v.typ) {
			if v = g.cloneInto(v); v == nil {
				return nil
			}
		}
		g.storeArgument(base, blockLL, i+1, ptypes[i+1], v)
	}
	thunk := g.spawnValueThunk(sig, ptypes, blockLL)
	// A call through a value is not followed when the stack a coroutine
	// starts on is settled, and a stack that turns out to be too small
	// grows, so the small tier is what such a start takes.
	return g.spawnHandle(n, module, watched, thunk, base, sig.Result, g.stackTier(""))
}

// blockType is the tuple the argument block of a started call is laid out as:
// one slot per parameter, in the representation the callee receives. A
// parameter that passes as a reference takes a pointer slot, because the object
// belongs to the caller: a copy here would leave a mutation the callee made
// sitting in the block instead of in the object the callee was handed.
func (g *generator) blockType(ptypes []*types.Type) *types.Type {
	slots := make([]*types.Type, len(ptypes))
	for i, t := range ptypes {
		slots[i] = t
		if byAddress(t) {
			slots[i] = types.Raw
		}
	}
	return types.TupleOf(slots...)
}

// argumentPart renders one argument of a started call from the block slot that
// holds it: a reference parameter arrives as the pointer the slot holds, which
// is the object the caller named.
func (g *generator) argumentPart(typ *types.Type, slot string) string {
	reg := g.newReg()
	g.emit("  %s = load %s, ptr %s", reg, g.llPass(typ), slot)
	return g.llPass(typ) + " " + reg
}

// storeResult leaves the value a started call answered where the joiner reads
// it. The runtime takes an address, which a reference aggregate already is.
func (g *generator) storeResult(result *types.Type, reg string) {
	addr := reg
	if g.llPass(result) != "ptr" {
		addr = g.newReg()
		g.emit("  %s = inttoptr %s %s to ptr", addr, g.llPass(result), reg)
	}
	g.declareRuntime("zyxrt_coro_set_result", "void", "ptr")
	g.emit("  call void @zyxrt_coro_set_result(ptr %s)", addr)
}

// storeArgument fills one slot of the block a started call reads back, in the
// representation the callee receives: a reference parameter stores the pointer
// to the caller's object, so what the callee writes is that object.
func (g *generator) storeArgument(base, blockLL string, index int, typ *types.Type, v *value) {
	slot := g.newReg()
	g.emit("  %s = getelementptr inbounds %s, ptr %s, i32 0, i32 %d", slot, blockLL, base, index)
	g.storeInto(slot, typ, v, false)
}

// spawnHandle starts the coroutine over the block a caller filled.
func (g *generator) spawnHandle(n *zyxc.Node, module string, watched bool, thunk, base string, result *types.Type, tier int) *value {
	handle := g.newReg()
	if watched {
		// The supervisor is a function value, so the code it carries and the
		// receiver it carries are what the runtime calls.
		watch := g.load(g.expr(module, g.wrapped(n, "watcher"), nil))
		if watch == nil {
			return nil
		}
		code := g.newReg()
		g.emit("  %s = extractvalue %s %s, 0", code, watch.ll, watch.text)
		recv := g.newReg()
		g.emit("  %s = extractvalue %s %s, 1", recv, watch.ll, watch.text)
		g.declareRuntime("zyxrt_coro_spawn_watched", "ptr", "ptr", "ptr", "ptr", "ptr", "i32")
		g.emit("  %s = call ptr @zyxrt_coro_spawn_watched(ptr @%s, ptr %s, ptr %s, ptr %s, i32 %d)",
			handle, thunk, base, code, recv, tier)
		return &value{text: handle, ll: "ptr", typ: types.JoinHandle.Instantiate(result)}
	}
	g.declareRuntime("zyxrt_coro_spawn", "ptr", "ptr", "ptr", "i32")
	g.emit("  %s = call ptr @zyxrt_coro_spawn(ptr @%s, ptr %s, i32 %d)", handle, thunk, base, tier)
	return &value{text: handle, ll: "ptr", typ: types.JoinHandle.Instantiate(result)}
}

// cloneInto copies an object into the running coroutine's region, which is
// what a value crossing a coroutine boundary needs when it is not SyncSafe.
// The copy is shallow: a field that names another reference object is left
// naming it, so a type that reaches one is reported rather than copied
// halfway.
func (g *generator) cloneInto(v *value) *value {
	typ := v.typ
	fields, ok := g.tab.Fields(typ)
	if !ok {
		g.fail(fmt.Errorf("llvmgen: the fields of %s are not known", typ))
		return nil
	}
	for _, f := range fields {
		if byAddress(f.Type) || f.Type.Kind == types.KindStr || f.Type.Kind == types.KindSlice {
			g.fail(fmt.Errorf("llvmgen: a boundary copy of %s reaches %s, which is not lowered", typ, f.Type))
			return nil
		}
	}
	slot := g.allocate(typ)
	g.copyObject(slot, g.addrText(v), typ)
	return &value{text: slot, ll: "ptr", typ: typ}
}

// join lowers Join and TryJoin on a coroutine handle. A coroutine reports
// either the value it produced or the exception it raised, and the handle
// turns that into a Result. TryJoin answers the same Result inside an Option,
// because the coroutine may not have finished yet.
func (g *generator) join(module string, n *zyxc.Node) *value {
	name := ""
	if len(n.Payload) > 0 {
		name = n.Payload[0]
	}
	if name != "Join" && name != "TryJoin" {
		g.fail(fmt.Errorf("llvmgen: %s on a coroutine handle is not lowered", name))
		return nil
	}
	handle := g.load(g.expr(module, g.wrapped(n, "recv"), nil))
	if handle == nil {
		return nil
	}
	typ := g.typeOfNode(n.First("type"), module)
	if typ == nil || typ.Kind != types.KindEnum || len(typ.Args) == 0 {
		g.fail(fmt.Errorf("llvmgen: the result of %s is not settled", name))
		return nil
	}
	// The Result the handle reports: Join answers it, TryJoin answers it
	// inside an Option.
	result := typ
	if name == "TryJoin" {
		result = typ.Args[0]
	}
	if result == nil || result.Kind != types.KindEnum || len(result.Args) == 0 {
		g.fail(fmt.Errorf("llvmgen: the result of %s is not settled", name))
		return nil
	}
	produced := result.Args[0]
	if produced.Kind != types.Invalid && !g.crossesAsRegister(produced) {
		g.fail(fmt.Errorf("llvmgen: a coroutine result of %s needs a boundary copy, which is not lowered", produced))
		return nil
	}
	panicSlot := g.alloca(types.Raw)
	rawSlot := g.alloca(types.Raw)

	if name == "Join" {
		g.declareRuntime("zyxrt_coro_join", "ptr", "ptr", "ptr")
		raw := g.newReg()
		g.emit("  %s = call ptr @zyxrt_coro_join(ptr %s, ptr %s)", raw, handle.text, panicSlot)
		raised := g.newReg()
		g.emit("  %s = load ptr, ptr %s", raised, panicSlot)
		return g.resultValue(result, raw, raised)
	}

	g.declareRuntime("zyxrt_coro_try_join", "i32", "ptr", "ptr", "ptr")
	done := g.newReg()
	g.emit("  %s = call i32 @zyxrt_coro_try_join(ptr %s, ptr %s, ptr %s)",
		done, handle.text, rawSlot, panicSlot)
	raw := g.newReg()
	g.emit("  %s = load ptr, ptr %s", raw, rawSlot)
	raised := g.newReg()
	g.emit("  %s = load ptr, ptr %s", raised, panicSlot)

	some, _, ok := g.variantOf(typ, "Some")
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no variant Some", typ))
		return nil
	}
	finished := g.boolFrom(done)
	obj := g.allocate(typ)
	tagType := g.tagType(g.tagSize(typ))
	tag := g.newReg()
	g.emit("  %s = select i1 %s, %s %d, %s 0", tag, finished.text, tagType, some, tagType)
	g.emit("  store %s %s, ptr %s", tagType, tag, obj)
	doneLabel := g.newLabel("tryjoin.done")
	endLabel := g.newLabel("tryjoin.end")
	g.terminator("  br i1 %s, label %%%s, label %%%s", finished.text, doneLabel, endLabel)
	g.label(doneLabel)
	inner := g.resultValue(result, raw, raised)
	// The payload of an Option holds the Result inline, so its bytes are
	// copied in rather than its address stored.
	g.copyObject(g.payloadElement(obj, typ, []*types.Type{result}, 0), inner.text, result)
	g.terminator("  br label %%%s", endLabel)
	g.label(endLabel)
	return &value{text: obj, ll: "ptr", typ: typ}
}

// resultValue builds the Result a coroutine handle reports from the raw value
// the coroutine recorded and the exception it raised.
func (g *generator) resultValue(typ *types.Type, raw, raised string) *value {
	produced := typ.Args[0]
	errIndex, _, ok := g.variantOf(typ, "Err")
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no variant Err", typ))
		return nil
	}
	okIndex, _, ok := g.variantOf(typ, "Ok")
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no variant Ok", typ))
		return nil
	}
	obj := g.allocate(typ)
	tagType := g.tagType(g.tagSize(typ))
	isErr := g.newReg()
	g.emit("  %s = icmp ne ptr %s, null", isErr, raised)
	tag := g.newReg()
	g.emit("  %s = select i1 %s, %s %d, %s %d", tag, isErr, tagType, errIndex, tagType, okIndex)
	g.emit("  store %s %s, ptr %s", tagType, tag, obj)

	// The union holds one payload, so the two outcomes are written on their
	// own paths.
	errLabel := g.newLabel("join.err")
	okLabel := g.newLabel("join.ok")
	endLabel := g.newLabel("join.end")
	g.terminator("  br i1 %s, label %%%s, label %%%s", isErr, errLabel, okLabel)

	g.label(okLabel)
	if produced.Kind != types.Invalid {
		// A coroutine that returns nothing reports no value, so the Ok side
		// carries no payload.
		g.emit("  store %s %s, ptr %s", g.llPass(produced), g.fromRaw(raw, produced),
			g.payloadElement(obj, typ, []*types.Type{produced}, 0))
	}
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
// spawnValueThunk emits the entry a call through a function value runs
// through: the first slot of the block carries the value, and the thunk calls
// it with the receiver it holds, followed by the arguments.
func (g *generator) spawnValueThunk(sig *types.Type, ptypes []*types.Type, blockLL string) string {
	g.thunks++
	name := fmt.Sprintf("zyx.spawn.%d", g.thunks)
	saved := g.fn
	g.fn = &funcState{symbol: name, vars: map[string]*varSlot{}}
	g.fn.entry = g.newLabel("thunk")
	g.fn.ret = g.newLabel("thunk.done")

	fnType := types.FuncOf(sig.Result, sig.Elems...)
	fnSlot := g.newReg()
	g.emit("  %s = getelementptr inbounds %s, ptr %%arg, i32 0, i32 0", fnSlot, blockLL)
	fn := g.newReg()
	g.emit("  %s = load %s, ptr %s", fn, g.llPass(fnType), fnSlot)
	code := g.newReg()
	g.emit("  %s = extractvalue %s %s, 0", code, g.llPass(fnType), fn)
	recv := g.newReg()
	g.emit("  %s = extractvalue %s %s, 1", recv, g.llPass(fnType), fn)
	parts := []string{"ptr " + recv}
	for i, typ := range sig.Elems {
		slot := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %%arg, i32 0, i32 %d", slot, blockLL, i+1)
		parts = append(parts, g.argumentPart(typ, slot))
	}
	if sig.Result == nil {
		g.emit("  call void %s(%s)", code, strings.Join(parts, ", "))
	} else {
		reg := g.newReg()
		g.emit("  %s = call %s %s(%s)", reg, g.llPass(sig.Result), code, strings.Join(parts, ", "))
		g.storeResult(sig.Result, reg)
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

// spawnThunk emits the entry a started call to a named function runs through:
// it reads the argument block back and calls the function.
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
		parts = append(parts, g.argumentPart(typ, slot))
	}
	if result == nil {
		g.emit("  call void @%s(%s)", llvmName(sym), strings.Join(parts, ", "))
	} else {
		reg := g.newReg()
		g.emit("  %s = call %s @%s(%s)", reg, g.llPass(result), llvmName(sym), strings.Join(parts, ", "))
		g.storeResult(result, reg)
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
