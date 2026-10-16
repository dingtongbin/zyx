// Escape analysis: home regions, move, transfer, and shared binding.

package marks

import (
	"zyx/internal/ast"
)

// An allocation is one object created in a function.
type allocation struct {
	// Node is the allocating expression, which carries the home mark.
	Node ast.Expr
	// Sym is the symbol of the binding that holds the object, empty when
	// the value is not bound to a local name.
	Sym string
	// Region is the region the allocation site sits in.
	Region *Region
}

// escapeKind records how a value leaves the function that created it.
type escapeKind uint8

const (
	// escapeNone means the object stays inside the function.
	escapeNone escapeKind = iota
	// escapeReturn means the object is returned, so its ownership moves to
	// the caller.
	escapeReturn
	// escapeCoroutine means the object is handed to a coroutine, so its
	// ownership moves to that coroutine's region.
	escapeCoroutine
	// escapeStore means the object is stored somewhere that outlives the
	// binding, so it is truly shared.
	escapeStore
)

// finishFunction classifies every allocation of the function it just
// walked. An object that stays inside one region is released at its last
// use; one that leaves the function is transferred or moved; one that is
// stored where it outlives its binding is shared.
func (b *builder) finishFunction() {
	for _, a := range b.allocsByNode {
		b.classify(a)
	}
}

// classify decides the marks one allocation takes.
func (b *builder) classify(a *allocation) {
	switch b.escapes[a] {
	case escapeReturn:
		b.info.Add(a.Node, "transfer", "caller")
		b.info.Add(a.Node, "alias", "")
	case escapeCoroutine:
		b.info.Add(a.Node, "move", "c0")
		b.info.Add(a.Node, "alias", "")
	case escapeStore:
		// The object is truly shared, so it binds to the region that
		// contains every use.
		b.info.Add(a.Node, "alias", "")
		b.sharedBind(a)
	default:
		b.homeAndRelease(a)
	}
}

// sharedBind binds an object more than one region uses to the innermost
// region that contains every use. The object is created there, so no use
// outlives the region that holds it.
func (b *builder) sharedBind(a *allocation) {
	regions := append([]*Region{a.Region}, b.uses[a]...)
	home := commonAll(regions)
	if home == nil {
		home = a.Region
	}
	b.info.Add(a.Node, "shared-bind", home.Name)
}

// homeAndRelease settles the home region of an object that stays inside the
// function: the innermost region that contains the allocation site and every
// use. An object that leaves its region moves; one that does not is released
// at its last use.
func (b *builder) homeAndRelease(a *allocation) {
	regions := append([]*Region{a.Region}, b.uses[a]...)
	home := commonAll(regions)
	if home == nil || home == a.Region {
		b.release(a)
		return
	}
	b.info.Add(a.Node, "move", home.Name)
}
