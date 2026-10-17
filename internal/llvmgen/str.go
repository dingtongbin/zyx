// Built-in methods on str. A str is a borrowed pointer and a length, so the
// language gives it methods of its own rather than a declaration: the fixed
// table is what pairs the name with the receiver's type.

package llvmgen

import (
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// strBuiltin lowers one method the language gives a str, or reports that the
// name is not one of them.
func (g *generator) strBuiltin(module string, n *zyxc.Node, v *value, name string) (*value, bool) {
	switch name {
	case "Len":
		// A str counts its bytes, which is what its length is.
		_, length := g.splitFat(v)
		return &value{text: length, ll: g.ptrInt(), typ: types.Usize}, true
	case "Bytes":
		// The bytes in place: the view names the string's own storage rather
		// than a copy of it, which the view lifetime rules cover.
		ptr, length := g.splitFat(v)
		typ := types.SliceOf(types.Uint8)
		ll := g.llType(typ)
		first := g.newReg()
		g.emit("  %s = insertvalue %s undef, ptr %s, 0", first, ll, ptr)
		out := g.newReg()
		g.emit("  %s = insertvalue %s %s, %s %s, 1", out, ll, first, g.ptrInt(), length)
		return &value{text: out, ll: ll, typ: typ}, true
	}
	return nil, false
}
