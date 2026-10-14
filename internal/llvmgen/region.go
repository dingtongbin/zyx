// Region enter and exit lowering.

package llvmgen

import (
	"fmt"

	"zyx/internal/layout"
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// regionStmt lowers a region block. Every object a program creates lives in
// the region of the coroutine that created it, and a coroutine owns its
// region for its whole life, so a region block needs no runtime call of its
// own until nested regions are lowered.
func (g *generator) regionStmt(module string, n *zyxc.Node) {
	g.stmt(module, g.wrapped(n, "body"))
}

// allocate creates an object of a type in the region of the running
// coroutine and returns its address.
func (g *generator) allocate(typ *types.Type) string {
	size, err := layout.Size(typ, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("llvmgen: the size of %s: %w", typ, err))
		return "null"
	}
	align, err := layout.Align(typ, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("llvmgen: the alignment of %s: %w", typ, err))
		return "null"
	}
	g.declareRuntime("zyxrt_alloc_here", "ptr", "i64", "i64")
	reg := g.newReg()
	g.emit("  %s = call ptr @zyxrt_alloc_here(i64 %d, i64 %d)", reg, size, align)
	return reg
}
