// Walking a sequence: a fixed-size array, a slice, a range, a str, or a
// container. A container is an opaque runtime handle, so walking one needs the
// container runtime, which also carries the modification count the language
// makes an iteration compare.

package llvmgen

import (
	"fmt"

	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// A loopBind is one name a for-in binds to what it walks.
type loopBind struct {
	name   string
	symbol string
	typ    *types.Type
}

// forIn lowers a for-in statement.
func (g *generator) forIn(module string, n *zyxc.Node) {
	binds, err := g.loopBindings(n)
	if err != nil {
		g.fail(err)
		return
	}
	iter := g.wrapped(n, "iter")
	if iter == nil {
		g.fail(fmt.Errorf("llvmgen: a for-in needs an iterable"))
		return
	}
	if iter.Kind == "range" {
		g.forRange(module, n, iter, binds[0])
		return
	}
	seq := g.expr(module, iter, nil)
	if seq == nil {
		return
	}
	switch seq.typ.Kind {
	case types.KindArray:
		binds[0].typ = seq.typ.Elem
		base := g.addrText(seq)
		count := g.constValue(types.Int64, fmt.Sprintf("%d", seq.typ.Len))
		g.forWalk(module, n, binds[0], count, func(index string) *value {
			reg := g.newReg()
			g.emit("  %s = getelementptr inbounds %s, ptr %s, i64 0, i64 %s",
				reg, g.llType(seq.typ), base, index)
			return &value{text: reg, ll: g.llPass(seq.typ.Elem), typ: seq.typ.Elem, lvalue: true, inline: true}
		}, nil)
	case types.KindSlice:
		binds[0].typ = seq.typ.Elem
		seq = g.load(seq)
		ptr, length := g.splitFat(seq)
		count := &value{text: length, ll: g.ptrInt(), typ: types.Int64}
		g.forWalk(module, n, binds[0], count, func(index string) *value {
			reg := g.newReg()
			g.emit("  %s = getelementptr inbounds %s, ptr %s, i64 %s",
				reg, g.llType(seq.typ.Elem), ptr, index)
			return &value{text: reg, ll: g.llPass(seq.typ.Elem), typ: seq.typ.Elem, lvalue: true, inline: true}
		}, nil)
	case types.KindStr:
		g.forStr(module, n, seq, binds[0])
	case types.KindContainer:
		// The tree annotates the iterable, so the sequence is lowered once
		// and the handle it names is what the runtime calls take.
		seq = g.load(seq)
		switch seq.typ.Name {
		case "Array":
			g.forArray(module, n, seq, binds[0])
		case "Set":
			g.forSet(module, n, seq, binds[0])
		case "Map":
			g.forMap(module, n, seq, binds[0], binds[1])
		default:
			g.fail(fmt.Errorf("llvmgen: a for-in over %s is not lowered", seq.typ))
		}
	default:
		g.fail(fmt.Errorf("llvmgen: a for-in over %s is not lowered", seq.typ))
	}
}

// loopBindings returns the names a for-in binds. The tree writes a label
// first when the loop has one, then two binding slots, so a sequence leaves
// the second slot empty and a map fills both.
func (g *generator) loopBindings(n *zyxc.Node) ([]loopBind, error) {
	payload := n.Payload
	if len(payload) == 5 {
		payload = payload[1:]
	}
	if len(payload) != 4 {
		return nil, fmt.Errorf("llvmgen: a for-in needs two binding slots")
	}
	out := make([]loopBind, 2)
	for i := 0; i < 2; i++ {
		bind := loopBind{name: payload[i*2], symbol: payload[i*2+1]}
		if bind.name == "_" {
			bind.symbol = ""
		}
		out[i] = bind
	}
	return out, nil
}

// loopLabel returns the label a loop carries, or the empty string. The tree
// writes the label first in the payload, before the bindings a for-in adds.
func loopLabel(n *zyxc.Node, bindings int) string {
	if len(n.Payload) > bindings {
		return n.Payload[0]
	}
	return ""
}

// forRange lowers a for-in over a range, which counts from the lower bound to
// the upper one. The walk counts in the pointer-width integer, so the bounds
// are read in it and every element comes back in the range's own type: a range
// whose bounds are narrower than a pointer still compares and subtracts in one
// integer type, which is what the language requires of it.
func (g *generator) forRange(module string, n *zyxc.Node, iter *zyxc.Node, bind loopBind) {
	lo := g.load(g.expr(module, g.wrapped(iter, "lo"), nil))
	hi := g.load(g.expr(module, g.wrapped(iter, "hi"), nil))
	if lo == nil || hi == nil {
		return
	}
	bind.typ = lo.typ
	start := g.cast(lo, types.Int64)
	end := g.cast(hi, types.Int64)
	if start == nil || end == nil {
		return
	}
	count := g.newReg()
	g.emit("  %s = sub i64 %s, %s", count, end.text, start.text)
	g.forWalk(module, n, bind, &value{text: count, ll: "i64", typ: types.Int64}, func(index string) *value {
		at := g.newReg()
		g.emit("  %s = add i64 %s, %s", at, start.text, index)
		element := g.cast(&value{text: at, ll: "i64", typ: types.Int64}, bind.typ)
		if element == nil {
			return nil
		}
		return element
	}, nil)
}

// forWalk lowers a loop that walks length elements. element returns the
// element at an index, or nil when the element cannot be reached. guard runs
// on every back edge, which is where a container iteration compares the
// modification count: a modification the body makes is noticed even on the
// last round, because the loop has not ended until it takes that edge.
func (g *generator) forWalk(module string, n *zyxc.Node, bind loopBind, length *value, element func(index string) *value, guard func()) {
	condLabel := g.newLabel("forin.cond")
	bodyLabel := g.newLabel("forin.body")
	stepLabel := g.newLabel("forin.step")
	endLabel := g.newLabel("forin.end")
	// The loop covers every block it emits, from the first to the last, so
	// the release of an object the loop still reads waits until the loop is
	// left instead of landing inside it.
	g.fn.loops = append(g.fn.loops, loopInfo{label: loopLabel(n, 4), breakTo: endLabel, continueTo: stepLabel})
	defer func() { g.fn.loops = g.fn.loops[:len(g.fn.loops)-1] }()

	indexSlot := g.alloca(types.Int64)
	g.emit("  store i64 0, ptr %s", indexSlot)
	lengthSlot := g.alloca(types.Int64)
	g.storeInto(lengthSlot, types.Int64, length, false)

	valueSlot := g.declareLoopVar(bind)

	g.terminator("  br label %%%s", condLabel)
	g.label(condLabel)
	index := g.newReg()
	g.emit("  %s = load i64, ptr %s", index, indexSlot)
	bound := g.newReg()
	g.emit("  %s = load i64, ptr %s", bound, lengthSlot)
	cond := g.newReg()
	g.emit("  %s = icmp slt i64 %s, %s", cond, index, bound)
	g.terminator("  br i1 %s, label %%%s, label %%%s", cond, bodyLabel, endLabel)
	g.label(bodyLabel)
	if valueSlot != "" {
		elem := element(index)
		if elem == nil {
			return
		}
		g.storeInto(valueSlot, bind.typ, elem, false)
	}
	g.stmt(module, g.wrapped(n, "body"))
	if !g.fn.terminated {
		g.terminator("  br label %%%s", stepLabel)
	}
	g.label(stepLabel)
	if guard != nil {
		guard()
	}
	g.preemptCheck()
	next := g.newReg()
	g.emit("  %s = add i64 %s, 1", next, index)
	g.emit("  store i64 %s, ptr %s", next, indexSlot)
	g.terminator("  br label %%%s", condLabel)
	g.label(endLabel)
}

// declareLoopVar gives a loop binding its storage and records it, so the body
// reads what the walk produced. It answers the storage, or the empty string
// when the source bound nothing.
func (g *generator) declareLoopVar(bind loopBind) string {
	if bind.name == "_" || bind.typ == nil {
		return ""
	}
	slot := g.alloca(bind.typ)
	if bind.symbol != "" {
		g.fn.vars[bind.symbol] = &varSlot{alloca: slot, typ: bind.typ}
		g.rememberBinding(bind.symbol)
	}
	return slot
}

// forArray walks an Array through the runtime: the length is taken once, as
// the language requires, and every round checks that the array has not been
// modified since. The length and the buffer come from the header beside the
// array, so a round pays for an address rather than for a call.
func (g *generator) forArray(module string, n *zyxc.Node, seq *value, bind loopBind) {
	elem := arg(seq.typ, 0)
	bind.typ = elem
	header := g.arrayHeaderOf(seq.text)
	count := g.arrayLen(header)
	data := g.arrayData(header)
	before := g.modGen(seq, "zyxrt_array_mod_gen")
	g.forWalk(module, n, bind, &value{text: count, ll: g.ptrInt(), typ: types.Usize}, func(index string) *value {
		slot := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, %s %s", slot, g.llType(elem), data, g.ptrInt(), index)
		return g.inlineObject(slot, elem)
	}, func() {
		g.checkModGen(seq, before, "zyxrt_array_mod_gen")
	})
}

// forSet walks a Set through the runtime iterator, which hands out one element
// per call.
func (g *generator) forSet(module string, n *zyxc.Node, seq *value, bind loopBind) {
	elem := arg(seq.typ, 0)
	bind.typ = elem
	out := g.objectAlloca(elem)
	g.declareLoopVar(bind)
	g.declareRuntime("zyxrt_set_iter_new", "ptr", "ptr")
	iter := g.newReg()
	g.emit("  %s = call ptr @zyxrt_set_iter_new(ptr %s)", iter, seq.text)
	before := g.modGen(seq, "zyxrt_map_mod_gen")
	g.forIterate(module, n, func() string {
		g.declareRuntime("zyxrt_set_iter_next", "i32", "ptr", "ptr")
		more := g.newReg()
		g.emit("  %s = call i32 @zyxrt_set_iter_next(ptr %s, ptr %s)", more, iter, out)
		return more
	}, func() {
		g.checkModGen(seq, before, "zyxrt_map_mod_gen")
	}, func() {
		g.bindLoopValue(bind, g.inlineObject(out, elem))
	})
}

// forMap walks a Map through the runtime iterator, which hands out one key and
// its value per call.
func (g *generator) forMap(module string, n *zyxc.Node, seq *value, key, val loopBind) {
	keyType, valType := arg(seq.typ, 0), arg(seq.typ, 1)
	key.typ, val.typ = keyType, valType
	keyOut := g.objectAlloca(keyType)
	valOut := g.objectAlloca(valType)
	g.declareLoopVar(key)
	g.declareLoopVar(val)
	g.declareRuntime("zyxrt_map_iter_new", "ptr", "ptr")
	iter := g.newReg()
	g.emit("  %s = call ptr @zyxrt_map_iter_new(ptr %s)", iter, seq.text)
	before := g.modGen(seq, "zyxrt_map_mod_gen")
	g.forIterate(module, n, func() string {
		g.declareRuntime("zyxrt_map_iter_next", "i32", "ptr", "ptr", "ptr")
		more := g.newReg()
		g.emit("  %s = call i32 @zyxrt_map_iter_next(ptr %s, ptr %s, ptr %s)", more, iter, keyOut, valOut)
		return more
	}, func() {
		g.checkModGen(seq, before, "zyxrt_map_mod_gen")
	}, func() {
		g.bindLoopValue(key, g.inlineObject(keyOut, keyType))
		g.bindLoopValue(val, g.inlineObject(valOut, valType))
	})
}

// forStr walks the runes of a str. A rune is one or more bytes, so the loop
// advances by what the decoder reports rather than by one.
func (g *generator) forStr(module string, n *zyxc.Node, seq *value, bind loopBind) {
	bind.typ = types.Char
	seq = g.load(seq)
	ptr, length := g.splitFat(seq)
	condLabel := g.newLabel("forstr.cond")
	bodyLabel := g.newLabel("forstr.body")
	stepLabel := g.newLabel("forstr.step")
	endLabel := g.newLabel("forstr.end")
	g.fn.loops = append(g.fn.loops, loopInfo{label: loopLabel(n, 4), breakTo: endLabel, continueTo: stepLabel})
	defer func() { g.fn.loops = g.fn.loops[:len(g.fn.loops)-1] }()

	atSlot := g.alloca(types.Usize)
	g.emit("  store %s 0, ptr %s", g.ptrInt(), atSlot)
	valueSlot := g.declareLoopVar(bind)

	g.terminator("  br label %%%s", condLabel)
	g.label(condLabel)
	at := g.newReg()
	g.emit("  %s = load %s, ptr %s", at, g.ptrInt(), atSlot)
	cond := g.newReg()
	g.emit("  %s = icmp ult %s %s, %s", cond, g.ptrInt(), at, length)
	g.terminator("  br i1 %s, label %%%s, label %%%s", cond, bodyLabel, endLabel)
	g.label(bodyLabel)
	nextSlot := g.alloca(types.Usize)
	g.declareRuntime("zyxrt_str_rune", "i32", "ptr", g.ptrInt(), g.ptrInt(), "ptr")
	rune := g.newReg()
	g.emit("  %s = call i32 @zyxrt_str_rune(ptr %s, %s %s, %s %s, ptr %s)",
		rune, ptr, g.ptrInt(), length, g.ptrInt(), at, nextSlot)
	if valueSlot != "" {
		g.storeInto(valueSlot, types.Char, &value{text: rune, ll: "i32", typ: types.Char}, false)
	}
	g.stmt(module, g.wrapped(n, "body"))
	if !g.fn.terminated {
		g.terminator("  br label %%%s", stepLabel)
	}
	g.label(stepLabel)
	g.preemptCheck()
	next := g.newReg()
	g.emit("  %s = load %s, ptr %s", next, g.ptrInt(), nextSlot)
	g.emit("  store %s %s, ptr %s", g.ptrInt(), next, atSlot)
	g.terminator("  br label %%%s", condLabel)
	g.label(endLabel)
}

// forIterate lowers a loop over a runtime iterator. next advances the cursor
// and answers whether it produced another entry; bind gives the body the
// storage the call filled; guard runs on every back edge.
func (g *generator) forIterate(module string, n *zyxc.Node, next func() string, guard, bind func()) {
	condLabel := g.newLabel("foriter.cond")
	bodyLabel := g.newLabel("foriter.body")
	latchLabel := g.newLabel("foriter.latch")
	endLabel := g.newLabel("foriter.end")
	g.fn.loops = append(g.fn.loops, loopInfo{label: loopLabel(n, 4), breakTo: endLabel, continueTo: latchLabel})
	defer func() { g.fn.loops = g.fn.loops[:len(g.fn.loops)-1] }()
	g.terminator("  br label %%%s", condLabel)
	g.label(condLabel)
	more := next()
	cond := g.newReg()
	g.emit("  %s = icmp ne i32 %s, 0", cond, more)
	g.terminator("  br i1 %s, label %%%s, label %%%s", cond, bodyLabel, endLabel)
	g.label(bodyLabel)
	bind()
	g.stmt(module, g.wrapped(n, "body"))
	if !g.fn.terminated {
		g.terminator("  br label %%%s", latchLabel)
	}
	g.label(latchLabel)
	if guard != nil {
		guard()
	}
	g.preemptCheck()
	g.terminator("  br label %%%s", condLabel)
	g.label(endLabel)
}

// bindLoopValue writes one binding of an iteration from the value the walk
// produced, leaving the source bound nothing.
func (g *generator) bindLoopValue(bind loopBind, v *value) {
	if bind.name == "_" || bind.typ == nil {
		return
	}
	slot, ok := g.fn.vars[bind.symbol]
	if !ok || slot == nil {
		return
	}
	g.storeInto(slot.alloca, bind.typ, v, false)
}

// modGen takes a container's modification count into a local, which an
// iteration compares each round.
func (g *generator) modGen(seq *value, fn string) string {
	g.declareRuntime(fn, g.ptrInt(), "ptr")
	slot := g.alloca(types.Usize)
	count := g.newReg()
	g.emit("  %s = call %s @%s(ptr %s)", count, g.ptrInt(), fn, seq.text)
	g.emit("  store %s %s, ptr %s", g.ptrInt(), count, slot)
	return slot
}

// checkModGen makes a structural modification during an iteration the fault
// the language defines, because the iteration may be walking storage the
// container no longer holds.
func (g *generator) checkModGen(seq *value, before, fn string) {
	g.declareRuntime(fn, g.ptrInt(), "ptr")
	now := g.newReg()
	g.emit("  %s = call %s @%s(ptr %s)", now, g.ptrInt(), fn, seq.text)
	was := g.newReg()
	g.emit("  %s = load %s, ptr %s", was, g.ptrInt(), before)
	changed := g.newReg()
	g.emit("  %s = icmp ne %s %s, %s", changed, g.ptrInt(), now, was)
	g.guard(changed, "the container was modified while it was being iterated")
}

// constValue returns a constant of a type as a value.
func (g *generator) constValue(typ *types.Type, text string) *value {
	return &value{text: text, ll: g.llType(typ), typ: typ}
}
