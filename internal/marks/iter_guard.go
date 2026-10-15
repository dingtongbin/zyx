// Iteration guards for structural modification checks.

package marks

import "zyx/internal/ast"

// guardLoop marks a for-in loop. The body must not modify the iterated
// container structurally while the loop runs; the guard compares the
// container's modification generation, which every structural change
// increments, so the check costs one comparison per round and needs no
// per-object bookkeeping.
func (b *builder) guardLoop(node ast.Node) {
	b.info.Add(node, "iter-guard", "")
}
