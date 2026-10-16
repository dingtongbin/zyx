// Walking a sequence: a fixed-size array, a slice, or a range. A container is
// an opaque runtime handle, so walking one needs the container runtime.

package llvmgen

import (
	"fmt"

	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// A loopBind is the name a for-in binds to the value it walks.
type loopBind struct {
	name   string
	symbol string
	typ    *types.Type
}

// forIn lowers a for-in statement.
func (g *generator) forIn(module string, n *zyxc.Node) {
	bind, err := g.loopBinding(n)
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
		g.forRange(module, n, iter, bind)
		return
	}
	seq := g.expr(module, iter, nil)
	if seq == nil {
		return
	}
	if seq.typ.Kind == types.KindArray || seq.typ.Kind == types.KindSlice {
		bind.typ = seq.typ.Elem
	}
	if bind.typ == nil {
		g.fail(fmt.Errorf("llvmgen: the type of the loop binding %s is not settled", bind.name))
		return
	}
	switch seq.typ.Kind {
	case types.KindArray:
		base := g.addrText(seq)
		count := g.constValue(types.Int64, fmt.Sprintf("%d", seq.typ.Len))
		g.forWalk(module, n, bind, count, func(index string) *value {
			reg := g.newReg()
			g.emit("  %s = getelementptr inbounds %s, ptr %s, i64 0, i64 %s",
				reg, g.llType(seq.typ), base, index)
			return &value{text: reg, ll: g.llPass(seq.typ.Elem), typ: seq.typ.Elem, lvalue: true, inline: true}
		})
	case types.KindSlice:
		seq = g.load(seq)
		ptr, length := g.splitFat(seq)
		count := &value{text: length, ll: g.ptrInt(), typ: types.Int64}
		g.forWalk(module, n, bind, count, func(index string) *value {
			reg := g.newReg()
			g.emit("  %s = getelementptr inbounds %s, ptr %s, i64 %s",
				reg, g.llType(seq.typ.Elem), ptr, index)
			return &value{text: reg, ll: g.llPass(seq.typ.Elem), typ: seq.typ.Elem, lvalue: true, inline: true}
		})
	default:
		g.fail(fmt.Errorf("llvmgen: a for-in over %s is not lowered", seq.typ))
	}
}

// loopBinding returns the name a for-in binds. The type is the element type
// of the sequence, which the walk settles. A second name belongs to a map,
// which binds a key and a value, so a sequence binds one name.
func (g *generator) loopBinding(n *zyxc.Node) (loopBind, error) {
	if len(n.Payload) < 2 {
		return loopBind{}, fmt.Errorf("llvmgen: a for-in needs a binding")
	}
	bind := loopBind{name: n.Payload[0], symbol: n.Payload[1]}
	if bind.name == "_" {
		bind.symbol = ""
	}
	return bind, nil
}

// forRange lowers a for-in over a range, which counts from the lower bound to
// the upper one.
func (g *generator) forRange(module string, n *zyxc.Node, iter *zyxc.Node, bind loopBind) {
	lo := g.load(g.expr(module, g.wrapped(iter, "lo"), nil))
	hi := g.load(g.expr(module, g.wrapped(iter, "hi"), nil))
	if lo == nil || hi == nil {
		return
	}
	// The bounds carry the element type, which the range yields.
	bind.typ = lo.typ
	count := g.newReg()
	g.emit("  %s = sub %s %s, %s", count, lo.ll, hi.text, lo.text)
	g.forWalk(module, n, bind, &value{text: count, ll: lo.ll, typ: types.Int64}, func(index string) *value {
		reg := g.newReg()
		g.emit("  %s = add %s %s, %s", reg, lo.ll, lo.text, index)
		return &value{text: reg, ll: lo.ll, typ: bind.typ}
	})
}

// forWalk lowers a loop that walks length elements. element returns the
// element at an index, or nil when the element cannot be reached.
func (g *generator) forWalk(module string, n *zyxc.Node, bind loopBind, length *value, element func(index string) *value) {
	indexSlot := g.alloca(types.Int64)
	g.emit("  store i64 0, ptr %s", indexSlot)
	lengthSlot := g.alloca(types.Int64)
	g.storeInto(lengthSlot, types.Int64, length, false)

	valueSlot := ""
	if bind.name != "_" {
		valueSlot = g.alloca(bind.typ)
		if bind.symbol != "" {
			g.fn.vars[bind.symbol] = &varSlot{alloca: valueSlot, typ: bind.typ}
		}
	}

	condLabel := g.newLabel("forin.cond")
	bodyLabel := g.newLabel("forin.body")
	stepLabel := g.newLabel("forin.step")
	endLabel := g.newLabel("forin.end")
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
	g.fn.loops = append(g.fn.loops, loopInfo{breakTo: endLabel, continueTo: stepLabel})
	g.stmt(module, g.wrapped(n, "body"))
	g.fn.loops = g.fn.loops[:len(g.fn.loops)-1]
	if !g.fn.terminated {
		g.terminator("  br label %%%s", stepLabel)
	}
	g.label(stepLabel)
	next := g.newReg()
	g.emit("  %s = add i64 %s, 1", next, index)
	g.emit("  store i64 %s, ptr %s", next, indexSlot)
	g.terminator("  br label %%%s", condLabel)
	g.label(endLabel)
}

// constValue returns a constant of a type as a value.
func (g *generator) constValue(typ *types.Type, text string) *value {
	return &value{text: text, ll: g.llType(typ), typ: typ}
}
