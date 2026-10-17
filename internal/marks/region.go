// Region scopes and exit ordering.

package marks

import (
	"fmt"

	"zyx/internal/ast"
)

// A Region is one memory region of a function. Regions nest lexically: a
// function body is the root region, an explicit region block is a child of
// the region that encloses it, and a coroutine body is a child of the
// region block that hosts it.
type Region struct {
	// Name is the name the .zyxc layer uses: r0 for a function body, r1 and
	// up for the explicit region blocks in lexical order, and c0 for a
	// coroutine body.
	Name string
	// Node is the region statement, or nil for a function body.
	Node *ast.RegionStmt
	// Spawn is the spawn expression that starts the coroutine, or nil.
	Spawn *ast.SpawnExpr
	// Parent is the enclosing region, or nil for a function body.
	Parent *Region
	// Func is the function the region belongs to.
	Func *ast.FuncDecl
}

// contains reports whether r encloses other or is other.
func (r *Region) contains(other *Region) bool {
	for cur := other; cur != nil; cur = cur.Parent {
		if cur == r {
			return true
		}
	}
	return false
}

// common returns the innermost region that contains both a and b, which is
// the region a truly shared object binds to.
func common(a, b *Region) *Region {
	for cur := a; cur != nil; cur = cur.Parent {
		if cur.contains(b) {
			return cur
		}
	}
	return nil
}

// commonAll returns the innermost region that contains every region of the
// list, or nil for an empty list.
func commonAll(regions []*Region) *Region {
	if len(regions) == 0 {
		return nil
	}
	out := regions[0]
	for _, r := range regions[1:] {
		out = common(out, r)
	}
	return out
}

// regionBuilder numbers the regions of one function.
type regionBuilder struct {
	fn     *ast.FuncDecl
	count  int
	byNode map[ast.Node]*Region
}

// newRegionBuilder returns a builder for one function.
func newRegionBuilder(fn *ast.FuncDecl) *regionBuilder {
	return &regionBuilder{fn: fn, byNode: map[ast.Node]*Region{}}
}

// root returns the function body region.
func (b *regionBuilder) root() *Region {
	r := &Region{Name: "r0", Func: b.fn}
	if b.fn != nil && b.fn.Body != nil {
		b.byNode[b.fn.Body] = r
	}
	return r
}

// explicit returns the region of an explicit region block.
func (b *regionBuilder) explicit(node *ast.RegionStmt, parent *Region) *Region {
	b.count++
	r := &Region{
		Name:   fmt.Sprintf("r%d", b.count),
		Node:   node,
		Parent: parent,
		Func:   b.fn,
	}
	b.byNode[node] = r
	if node.Body != nil {
		b.byNode[node.Body] = r
	}
	return r
}

// coroutine returns the private region of a coroutine body.
func (b *regionBuilder) coroutine(spawn *ast.SpawnExpr, parent *Region) *Region {
	r := &Region{Name: "c0", Spawn: spawn, Parent: parent, Func: b.fn}
	b.byNode[spawn] = r
	return r
}

// regionOf returns the region a node belongs to, or nil.
func (b *regionBuilder) regionOf(n ast.Node) *Region {
	return b.byNode[n]
}
