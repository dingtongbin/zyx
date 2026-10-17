// Static release insertion for unshared objects.

package marks

import "zyx/internal/ast"

// markDeferred records the objects a deferred call names. The call runs when the
// region that owns the defer leaves, not where the statement is written, and the
// argument block the backend builds holds the address of a reference argument
// rather than a copy of the object, so the object has to be there when it runs.
func (b *builder) markDeferred(call ast.Expr) {
	c, ok := call.(*ast.CallExpr)
	if !ok {
		return
	}
	if sel, ok := c.Fn.(*ast.SelectorExpr); ok {
		b.noteDeferred(sel.X)
	}
	for _, a := range c.Args {
		b.noteDeferred(a)
	}
}

// noteDeferred records one value a deferred call names.
func (b *builder) noteDeferred(e ast.Expr) {
	b.hold(e)
}

// holdValue records the objects a callee value holds. A bound method carries
// its receiver inside the value, so a coroutine started through one reads the
// receiver after the statement that started it has ended, while the value was
// never an argument the walk recorded.
func (b *builder) holdValue(fn ast.Expr) {
	if sel, ok := fn.(*ast.SelectorExpr); ok {
		b.hold(sel.X)
	}
	b.hold(fn)
}

// hold records one object a call that runs later names.
func (b *builder) hold(e ast.Expr) {
	if a := b.allocFor(e); a != nil {
		b.held[a] = true
	}
}

// release marks an object that no sharing reaches: the last use is the
// release point, and the object's memory returns to the pool there rather
// than at region exit.
func (b *builder) release(a *allocation) {
	if b.held[a] {
		// A call that runs later names this object: a deferred call once its
		// region leaves, or a coroutine started through a value that carries
		// it. The region's own release is then the earliest point that is safe.
		// Leaving the release marks off keeps the object where the region put
		// it, which the batch release gives back after those calls have run.
		return
	}
	last := b.lastUse[a]
	if last == nil {
		// The object is never used again, so the allocation itself is the
		// release point.
		b.info.Add(a.Node, "last-use", "")
		b.info.Add(a.Node, "static-release", "")
		b.info.Add(a.Node, "why-last-use", "")
		return
	}
	b.info.Add(last, "last-use", "")
	b.info.Add(last, "static-release", "")
	b.info.Add(last, "why-last-use", "")
}
