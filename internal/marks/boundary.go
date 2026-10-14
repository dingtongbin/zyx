// Coroutine boundary copy and share decisions.

package marks

import (
	"zyx/internal/ast"
	"zyx/internal/check"
	"zyx/internal/types"
)

// boundaryMark returns the mark an argument takes when it crosses a
// coroutine boundary: a value type is copied, a SyncSafe reference is shared
// as it is, and every other reference is deep copied into the receiving
// coroutine's region.
func boundaryMark(cpkg *check.Package, arg ast.Expr) string {
	t := cpkg.Nodes[arg]
	if t == nil || t.Kind == types.Invalid {
		return "boundary-share"
	}
	if t.IsValue() {
		return "boundary-clone"
	}
	if cpkg.SyncSafe(t) {
		return "boundary-share"
	}
	return "boundary-clone"
}
