// The integer operations the language builds in.
//
// They exist beside the operators because the operators are checked: a loop
// that cannot afford an overflow or a shift check writes the operation it
// wants instead of the one the operator means. The bit queries read the value
// as its bits, so they answer the same way for a signed and an unsigned type.

package llvmgen

import (
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// intBuiltin lowers one built-in integer operation and reports whether the
// name is one of them.
func (g *generator) intBuiltin(module string, n *zyxc.Node, recv *value, name string) (*value, bool) {
	if recv == nil || !recv.typ.IsInteger() {
		return nil, false
	}
	ll := recv.ll
	switch name {
	case "wrap_add", "wrap_sub", "wrap_mul":
		other := g.load(g.expr(module, g.wrapped(n, "arg"), recv.typ))
		if other == nil {
			return nil, true
		}
		step := map[string]string{"wrap_add": "add", "wrap_sub": "sub", "wrap_mul": "mul"}[name]
		reg := g.newReg()
		g.emit("  %s = %s %s %s, %s", reg, step, ll, recv.text, other.text)
		return &value{text: reg, ll: ll, typ: recv.typ}, true
	case "rotl", "rotr":
		other := g.load(g.expr(module, g.wrapped(n, "arg"), recv.typ))
		if other == nil {
			return nil, true
		}
		// A funnel shift of a value with itself is a rotation, and it takes
		// the amount modulo the width, which is what a rotate means.
		builtin := "llvm.fshl." + ll
		if name == "rotr" {
			builtin = "llvm.fshr." + ll
		}
		g.declareRuntime(builtin, ll, ll, ll, ll)
		reg := g.newReg()
		g.emit("  %s = call %s @%s(%s %s, %s %s, %s %s)",
			reg, ll, builtin, ll, recv.text, ll, recv.text, ll, other.text)
		return &value{text: reg, ll: ll, typ: recv.typ}, true
	case "swap_bytes":
		if intWidth(ll) == 8 {
			// One byte is already in byte order.
			return recv, true
		}
		builtin := "llvm.bswap." + ll
		g.declareRuntime(builtin, ll, ll)
		reg := g.newReg()
		g.emit("  %s = call %s @%s(%s %s)", reg, ll, builtin, ll, recv.text)
		return &value{text: reg, ll: ll, typ: recv.typ}, true
	case "count_ones", "leading_zeros", "trailing_zeros":
		builtin := "llvm.ctpop." + ll
		if name == "leading_zeros" {
			builtin = "llvm.ctlz." + ll
		}
		if name == "trailing_zeros" {
			builtin = "llvm.cttz." + ll
		}
		reg := g.newReg()
		if name == "count_ones" {
			g.declareRuntime(builtin, ll, ll)
			g.emit("  %s = call %s @%s(%s %s)", reg, ll, builtin, ll, recv.text)
		} else {
			// A zero value counts as a full width rather than as an
			// undefined one, which is what the method means.
			g.declareRuntime(builtin, ll, ll, "i1")
			g.emit("  %s = call %s @%s(%s %s, i1 false)", reg, ll, builtin, ll, recv.text)
		}
		return &value{text: g.toUint32(reg, ll), ll: "i32", typ: types.Uint32}, true
	}
	return nil, false
}

// toUint32 converts an integer of any width to the u32 a bit query answers
// with.
func (g *generator) toUint32(reg, ll string) string {
	width := intWidth(ll)
	if width == 32 {
		return reg
	}
	out := g.newReg()
	if width < 32 {
		g.emit("  %s = zext %s %s to i32", out, ll, reg)
	} else {
		g.emit("  %s = trunc %s %s to i32", out, ll, reg)
	}
	return out
}
