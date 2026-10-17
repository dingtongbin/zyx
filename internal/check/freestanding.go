// The freestanding subset. A target built without the runtime has no
// coroutines, no channels, no regions, and no runtime allocator, so the
// constructs the specification says such a target does not have are rejected
// where they are written. What is left is value types, control flow, raw, and
// calls into C, which is what an operating system or a driver is written with.

package check

import (
	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/types"
)

// freestandingSpawn reports a coroutine start under a freestanding target, and
// names the supervised form separately because it also needs a supervisor to
// run.
func (c *checker) freestandingSpawn(v *ast.SpawnExpr) {
	if !c.freestanding {
		return
	}
	if v.Watcher != nil {
		c.errorHelp(diag.CodeFreestandingSpawnWatched, v.Sp,
			"call the function directly; a freestanding target has no runtime",
			"spawn_watched is not available on a freestanding target")
		return
	}
	c.errorHelp(diag.CodeFreestandingSpawn, v.Sp,
		"call the function directly; a freestanding target has no runtime",
		"spawn is not available on a freestanding target")
}

// freestandingRegion reports a region block under a freestanding target, which
// has no region allocator to release.
func (c *checker) freestandingRegion(v *ast.RegionStmt) {
	if !c.freestanding {
		return
	}
	c.errorHelp(diag.CodeFreestandingRegion, v.Sp,
		"keep values in the frame; a freestanding target has no allocator",
		"region is not available on a freestanding target")
}

// freestandingSelect reports a select under a freestanding target, which has no
// coroutine or channel to wait on.
func (c *checker) freestandingSelect(v *ast.SelectStmt) {
	if !c.freestanding {
		return
	}
	c.errorHelp(diag.CodeFreestandingSelect, v.Sp,
		"poll the descriptor or flag a freestanding program waits for itself",
		"select is not available on a freestanding target")
}

// freestandingChan reports a channel type under a freestanding target, which
// has no runtime to carry one.
func (c *checker) freestandingChan(v *ast.ChanType) {
	if !c.freestanding {
		return
	}
	c.errorHelp(diag.CodeFreestandingChan, v.Sp,
		"pass a value type, or a raw address C can share",
		"chan is not available on a freestanding target")
}

// freestandingNew reports a runtime allocation under a freestanding target.
func (c *checker) freestandingNew(v *ast.NewExpr) {
	if !c.freestanding {
		return
	}
	c.errorHelp(diag.CodeFreestandingNew, v.Sp,
		"keep the value in the frame, or let C allocate it",
		"new is not available on a freestanding target")
}

// freestandingLiteral reports a literal whose value is a reference, which a
// freestanding target has no allocator to build, and answers the type either
// way. A fixed-size array of value types is a value and stays available.
func (c *checker) freestandingLiteral(v ast.Expr, t *types.Type) *types.Type {
	if !c.freestanding || t == nil || t.Kind == types.Invalid || t.IsValue() {
		return t
	}
	c.errorHelp(diag.CodeFreestandingLiteral, v.Span(),
		"build a value type, or let C build the object",
		"a literal of %s is not available on a freestanding target", t)
	return t
}
