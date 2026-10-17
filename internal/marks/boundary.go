// Coroutine boundary copy and share decisions.

package marks

import (
	"zyx/internal/ast"
	"zyx/internal/check"
	"zyx/internal/types"
)

// boundaryMarks returns the mark an argument takes when it crosses a coroutine
// boundary, and the reason for it: a value type is copied, a SyncSafe reference
// is shared as it is, and every other reference is deep copied into the
// receiving coroutine's region.
func boundaryMarks(cpkg *check.Package, arg ast.Expr) (mark, reason string) {
	t := cpkg.Nodes[arg]
	if t == nil || t.Kind == types.Invalid {
		return "boundary-share", "why-syncsafe"
	}
	if t.IsValue() {
		return "boundary-clone", "why-not-syncsafe"
	}
	if cpkg.SyncSafe(t) {
		return "boundary-share", "why-syncsafe"
	}
	return "boundary-clone", "why-not-syncsafe"
}
