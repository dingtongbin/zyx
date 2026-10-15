// try: unwrapping a Result, and returning its error from the enclosing
// function.

package llvmgen

import (
	"fmt"

	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// tryCall lowers `try f(x)`. The value of a successful call is the result of
// the expression; an error becomes the error of the enclosing function's own
// result, which the call returns through, so the enclosing function must
// return a Result of the same error type.
func (g *generator) tryCall(module string, n *zyxc.Node) *value {
	inner := g.sole(n.First("call"))
	if inner == nil {
		g.fail(fmt.Errorf("llvmgen: try needs a call"))
		return nil
	}
	call := g.expr(module, inner, nil)
	if call == nil {
		return nil
	}
	typ := call.typ
	if typ == nil || typ.Kind != types.KindEnum || len(typ.Args) < 2 {
		g.fail(fmt.Errorf("llvmgen: try needs a call that returns a Result, got %s", typ))
		return nil
	}
	result := g.fn.result
	if result == nil || result.Kind != types.KindEnum || len(result.Args) < 2 {
		g.fail(fmt.Errorf("llvmgen: try needs an enclosing function that returns a Result"))
		return nil
	}

	base := g.addrText(call)
	tagType := g.tagType(g.tagSize(typ))
	tag := g.newReg()
	g.emit("  %s = load %s, ptr %s", tag, tagType, base)
	okIndex, _, ok := g.variantOf(typ, "Ok")
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no variant Ok", typ))
		return nil
	}
	okLabel := g.newLabel("try.ok")
	errLabel := g.newLabel("try.err")
	isOk := g.newReg()
	g.emit("  %s = icmp eq %s %s, %d", isOk, tagType, tag, okIndex)
	g.terminator("  br i1 %s, label %%%s, label %%%s", isOk, okLabel, errLabel)

	// The error returns from the enclosing function.
	g.label(errLabel)
	raised := g.payloadValue(base, typ, typ.Args[1], 0)
	if raised == nil {
		return nil
	}
	obj := g.buildVariant(result, "Err", []*value{raised})
	if obj == "" {
		return nil
	}
	if g.fn.resultSlot == "" {
		g.fn.resultSlot = g.alloca(result)
	}
	g.storeInto(g.fn.resultSlot, result, &value{text: obj, ll: "ptr", typ: result}, false)
	g.terminator("  br label %%%s", g.fn.ret)

	// The successful call yields the value it carried.
	g.label(okLabel)
	return g.payloadValue(base, typ, typ.Args[0], 0)
}
