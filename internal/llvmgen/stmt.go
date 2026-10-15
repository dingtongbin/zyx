// Statement lowering.

package llvmgen

import (
	"fmt"
	"strings"

	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// stmt lowers one statement.
func (g *generator) stmt(module string, n *zyxc.Node) {
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
	case "for":
		g.forStmt(module, n)
	case "break":
		g.breakStmt(n)
	case "continue":
		g.continueStmt(n)
	case "return":
		g.returnStmt(module, n)
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

// initChild returns the initializer of a binding, which is the child that
// is not a type annotation.
func (g *generator) initChild(n *zyxc.Node) *zyxc.Node {
	for _, child := range n.Children {
		switch child.Kind {
		case "type", "result":
			continue
		}
		return child
	}
	return nil
}

// bind lowers a local declaration.
func (g *generator) bind(module string, n *zyxc.Node) {
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
	// The result is kept in a local that the shared return block reads, so
	// that every return path converges on one epilogue.
	if g.fn.resultSlot == "" {
		g.fn.resultSlot = g.alloca(g.fn.result)
	}
	g.storeInto(g.fn.resultSlot, g.fn.result, v, false)
	g.terminator("  br label %%%s", g.fn.ret)
}
