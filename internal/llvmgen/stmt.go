// Statement lowering.

package llvmgen

import (
	"fmt"
	"strings"

	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// stmt lowers one statement, then releases the objects the analysis marked as
// no longer used inside it.
func (g *generator) stmt(module string, n *zyxc.Node) {
	mark := len(g.pending)
	g.stmtNode(module, n)
	g.flushReleases(mark)
}

// flushReleases releases the objects a statement finished with.
func (g *generator) flushReleases(from int) {
	pending := g.pending[from:]
	g.pending = g.pending[:from]
	for _, p := range pending {
		g.release(p.typ, p.addr)
	}
}

// stmtNode lowers one statement.
func (g *generator) stmtNode(module string, n *zyxc.Node) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "block":
		for _, child := range n.Children {
			g.stmt(module, child)
		}
	case "expr-stmt":
		g.expr(module, g.sole(n), nil)
	case "assign":
		g.assign(module, n)
	case "assign-op":
		g.assignOp(module, n)
	case "bind", "bind-const":
		g.bind(module, n)
	case "if":
		g.ifStmt(module, n)
	case "match":
		g.matchStmt(module, n)
	case "for":
		g.forStmt(module, n)
	case "for-in":
		g.forIn(module, n)
	case "break":
		g.breakStmt(n)
	case "continue":
		g.continueStmt(n)
	case "return":
		g.returnStmt(module, n)
	case "select":
		g.selectStmt(module, n)
	case "region":
		g.regionStmt(module, n)
	case "defer":
		g.deferStmt(module, n)
	default:
		g.fail(fmt.Errorf("llvmgen: statement %s is not lowered", n.Kind))
	}
}

// sole returns the single child of a wrapper node.
func (g *generator) sole(n *zyxc.Node) *zyxc.Node {
	if n == nil || len(n.Children) == 0 {
		return nil
	}
	return n.Children[0]
}

// wrapped returns the child of a labelled wrapper, or nil.
func (g *generator) wrapped(n *zyxc.Node, label string) *zyxc.Node {
	return g.sole(n.First(label))
}

// initChild returns the initializer of a binding. The format names it, so
// the backend never has to work out which child carries it.
func (g *generator) initChild(n *zyxc.Node) *zyxc.Node {
	if init := n.First("init"); init != nil {
		return g.sole(init)
	}
	if value := n.First("value"); value != nil {
		return g.sole(value)
	}
	return nil
}

// bind lowers a local declaration. A binding may introduce several names at
// once, which take the elements of the tuple it is given.
func (g *generator) bind(module string, n *zyxc.Node) {
	if len(n.Payload) > 2 {
		g.bindTuple(module, n)
		return
	}
	if len(n.Payload) < 2 {
		g.fail(fmt.Errorf("llvmgen: a binding needs a name"))
		return
	}
	name, id := n.Payload[0], n.Payload[1]
	typ := g.typeOfNode(n.First("type"), module)
	init := g.initChild(n)
	var v *value
	if init != nil {
		v = g.expr(module, init, typ)
		if typ == nil {
			typ = v.typ
		}
	}
	if typ == nil {
		g.fail(fmt.Errorf("llvmgen: the type of %s is not settled", name))
		return
	}
	slot := g.alloca(typ)
	if v != nil {
		g.storeInto(slot, typ, v, false)
	}
	if id != "" {
		g.fn.vars[id] = &varSlot{alloca: slot, typ: typ}
	}
}

// bindTuple lowers a binding that introduces several names from one tuple.
// The element types come from the tuple, so the format writes no type for the
// binding itself.
func (g *generator) bindTuple(module string, n *zyxc.Node) {
	init := g.initChild(n)
	if init == nil {
		g.fail(fmt.Errorf("llvmgen: a binding of several names needs a value"))
		return
	}
	v := g.load(g.expr(module, init, nil))
	if v == nil {
		return
	}
	if v.typ == nil || v.typ.Kind != types.KindTuple {
		g.fail(fmt.Errorf("llvmgen: a binding of several names needs a tuple, got %s", v.typ))
		return
	}
	for i := 0; i+1 < len(n.Payload); i += 2 {
		name, symbol := n.Payload[i], n.Payload[i+1]
		elem := i / 2
		if elem >= len(v.typ.Elems) {
			g.fail(fmt.Errorf("llvmgen: the binding has more names than %s has elements", v.typ))
			return
		}
		typ := v.typ.Elems[elem]
		slot := g.alloca(typ)
		g.storeInto(slot, typ, g.tupleElement(v, elem, typ), false)
		if name != "_" && symbol != "" {
			g.fn.vars[symbol] = &varSlot{alloca: slot, typ: typ}
		}
	}
}

// tupleElement returns one element of a tuple value.
func (g *generator) tupleElement(v *value, index int, typ *types.Type) *value {
	reg := g.newReg()
	g.emit("  %s = extractvalue %s %s, %d", reg, g.llType(v.typ), v.text, index)
	return &value{text: reg, ll: g.llPass(typ), typ: typ}
}

// assign lowers an assignment.
func (g *generator) assign(module string, n *zyxc.Node) {
	target := g.expr(module, g.wrapped(n, "target"), nil)
	if target == nil {
		return
	}
	src := g.expr(module, g.wrapped(n, "value"), target.typ)
	if src == nil {
		return
	}
	g.storeInto(target.text, target.typ, src, target.inline)
}

// assignOp lowers a compound assignment.
func (g *generator) assignOp(module string, n *zyxc.Node) {
	op := ""
	if len(n.Payload) > 0 {
		// A compound assignment carries its own spelling, such as `+=`.
		op = strings.TrimSuffix(n.Payload[0], "=")
	}
	target := g.expr(module, g.wrapped(n, "target"), nil)
	if target == nil {
		return
	}
	addr := g.addrText(target)
	cur := g.load(target)
	rhs := g.expr(module, g.wrapped(n, "value"), target.typ)
	if rhs == nil {
		return
	}
	result := g.arith(op, cur, rhs)
	if result == nil {
		return
	}
	g.storeInto(addr, target.typ, result, false)
}

// ifStmt lowers a conditional.
func (g *generator) ifStmt(module string, n *zyxc.Node) {
	cond := g.expr(module, g.wrapped(n, "cond"), types.Bool)
	thenLabel := g.newLabel("if.then")
	elseLabel := g.newLabel("if.else")
	endLabel := g.newLabel("if.end")
	if cond == nil {
		return
	}
	g.terminator("  br i1 %s, label %%%s, label %%%s", g.toBool(cond), thenLabel, elseLabel)
	g.label(thenLabel)
	g.stmt(module, g.wrapped(n, "then"))
	if !g.fn.terminated {
		g.terminator("  br label %%%s", endLabel)
	}
	g.label(elseLabel)
	if elseNode := g.wrapped(n, "else"); elseNode != nil {
		g.stmt(module, elseNode)
	}
	if !g.fn.terminated {
		g.terminator("  br label %%%s", endLabel)
	}
	g.label(endLabel)
}

// forStmt lowers the conditional loop form.
func (g *generator) forStmt(module string, n *zyxc.Node) {
	condLabel := g.newLabel("for.cond")
	bodyLabel := g.newLabel("for.body")
	endLabel := g.newLabel("for.end")
	g.terminator("  br label %%%s", condLabel)
	g.label(condLabel)
	cond := g.wrapped(n, "cond")
	if cond != nil {
		v := g.expr(module, cond, types.Bool)
		if v != nil {
			g.terminator("  br i1 %s, label %%%s, label %%%s", g.toBool(v), bodyLabel, endLabel)
		}
	} else {
		g.terminator("  br label %%%s", bodyLabel)
	}
	g.label(bodyLabel)
	g.fn.loops = append(g.fn.loops, loopInfo{breakTo: endLabel, continueTo: condLabel})
	g.stmt(module, g.wrapped(n, "body"))
	g.fn.loops = g.fn.loops[:len(g.fn.loops)-1]
	if !g.fn.terminated {
		g.terminator("  br label %%%s", condLabel)
	}
	g.label(endLabel)
}

// breakStmt lowers a break.
func (g *generator) breakStmt(n *zyxc.Node) {
	loop, ok := g.loopFor(n)
	if !ok {
		return
	}
	g.terminator("  br label %%%s", loop.breakTo)
}

// continueStmt lowers a continue.
func (g *generator) continueStmt(n *zyxc.Node) {
	loop, ok := g.loopFor(n)
	if !ok {
		return
	}
	g.terminator("  br label %%%s", loop.continueTo)
}

// loopFor returns the loop a break or a continue reaches. A labelled
// statement reaches the loop that carries the label; an unlabelled one
// reaches the innermost loop.
func (g *generator) loopFor(n *zyxc.Node) (loopInfo, bool) {
	if len(g.fn.loops) == 0 {
		g.fail(fmt.Errorf("llvmgen: a loop control statement is outside a loop"))
		return loopInfo{}, false
	}
	if len(n.Payload) == 0 || n.Payload[0] == "" {
		return g.fn.loops[len(g.fn.loops)-1], true
	}
	for i := len(g.fn.loops) - 1; i >= 0; i-- {
		if g.fn.loops[i].label == n.Payload[0] {
			return g.fn.loops[i], true
		}
	}
	g.fail(fmt.Errorf("llvmgen: no loop is labelled %s", n.Payload[0]))
	return loopInfo{}, false
}

// returnStmt lowers a return.
func (g *generator) returnStmt(module string, n *zyxc.Node) {
	if g.fn.result == nil {
		if len(n.Children) != 0 {
			g.fail(fmt.Errorf("llvmgen: a result-free function returns a value"))
		}
		g.terminator("  br label %%%s", g.fn.ret)
		return
	}
	if len(n.Children) == 0 {
		g.fail(fmt.Errorf("llvmgen: a function with a result returns nothing"))
		g.terminator("  br label %%%s", g.fn.ret)
		return
	}
	v := g.expr(module, n.Children[0], g.fn.result)
	if v == nil {
		g.terminator("  br label %%%s", g.fn.ret)
		return
	}
	if g.fn.result.Kind == types.KindInterface && v.typ.Kind != types.KindInterface {
		// A concrete result becomes an interface value on the way out.
		if v = g.ifaceValue(g.fn.result, v); v == nil {
			g.terminator("  br label %%%s", g.fn.ret)
			return
		}
	}
	// The result is kept in a local that the shared return block reads, so
	// that every return path converges on one epilogue.
	if g.fn.resultSlot == "" {
		g.fn.resultSlot = g.alloca(g.fn.result)
	}
	g.storeInto(g.fn.resultSlot, g.fn.result, v, false)
	g.terminator("  br label %%%s", g.fn.ret)
}
