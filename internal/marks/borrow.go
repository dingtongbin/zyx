// Borrow bookkeeping: the views of a container's buffer that are live, and the
// region a stored value has to outlive.

package marks

import (
	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/types"
)

// A viewBorrow is one live view of a container's buffer.
type viewBorrow struct {
	// Key names the container the view borrows, so two mentions of the same
	// container compare equal.
	Key string
	// At is the binding that holds the view.
	At ast.Node
}

// openScope marks how many borrows are live where a block opens.
func (b *builder) openScope() { b.scopes = append(b.scopes, len(b.borrows)) }

// closeScope drops the borrows a block introduced: a view a block binds does
// not outlive the block.
func (b *builder) closeScope() {
	if len(b.scopes) == 0 {
		return
	}
	mark := b.scopes[len(b.scopes)-1]
	b.scopes = b.scopes[:len(b.scopes)-1]
	b.borrows = b.borrows[:mark]
}

// noteViewBinding records a binding that holds a view of a container's buffer.
// The view names the buffer itself, and a structural modification may replace
// it, so the container must not be modified while the view is live.
func (b *builder) noteViewBinding(v *ast.VarDecl) {
	if !isDerivedView(v.Value) {
		return
	}
	key := b.containerKey(viewSource(v.Value))
	if key == "" {
		return
	}
	b.borrows = append(b.borrows, viewBorrow{Key: key, At: v})
}

// noteLoopBorrow records that a loop reaches its elements through the buffer
// of the container it walks, so the container must not be structurally
// modified for as long as the body runs. The runtime guard makes the same
// case a fault when the modification is not visible here, such as one made
// through another name for the container.
func (b *builder) noteLoopBorrow(v *ast.ForStmt) {
	t := b.cpkg.Nodes[v.Iter]
	if t == nil || t.Kind != types.KindContainer {
		return
	}
	key := b.containerKey(v.Iter)
	if key == "" {
		return
	}
	b.borrows = append(b.borrows, viewBorrow{Key: key, At: v})
}

// viewSource returns the container a view expression borrows.
func viewSource(e ast.Expr) ast.Expr {
	switch v := e.(type) {
	case *ast.IndexExpr:
		return v.X
	case *ast.CallExpr:
		if sel, ok := v.Fn.(*ast.SelectorExpr); ok {
			return sel.X
		}
	}
	return nil
}

// noteStore records that a value is stored where the destination holds it, so
// the value belongs to the destination's region and is created there.
func (b *builder) noteStore(value, dest ast.Expr) {
	a := b.allocFor(value)
	if a == nil {
		return
	}
	b.uses[a] = append(b.uses[a], b.destRegion(dest))
}

// destRegion returns the region of the object a destination expression names.
// A destination the function created itself has a known region, whether the
// expression reaches the object directly or through a field or an element of
// it. Anything else, such as a parameter or a field of one, may outlive every
// region the function can name, so the value belongs to the function's own
// region, which is the outermost one it can name.
func (b *builder) destRegion(dest ast.Expr) *Region {
	if a := b.allocFor(dest); a != nil && a.Region != nil {
		return a.Region
	}
	if root := rootIdent(dest); root != nil {
		if a := b.allocFor(root); a != nil && a.Region != nil {
			return a.Region
		}
	}
	return b.root
}

// storesIntoReceiver reports whether a method keeps its argument in the
// receiver, which makes the argument outlive the call.
func (b *builder) storesIntoReceiver(sel *ast.SelectorExpr) bool {
	if !storeMethod(sel.Name.Name) {
		return false
	}
	t := b.cpkg.Nodes[sel.X]
	if t == nil {
		return false
	}
	return t.Kind == types.KindContainer || t.Kind == types.KindChan
}

// isStructuralMod reports whether a method replaces or moves the buffer of a
// container, which leaves a view of that buffer naming storage the container
// no longer holds.
func (b *builder) isStructuralMod(sel *ast.SelectorExpr) bool {
	switch sel.Name.Name {
	case "Push", "Pop", "Remove", "Insert", "RemoveRange", "Clear", "Reserve", "Reset":
	default:
		return false
	}
	t := b.cpkg.Nodes[sel.X]
	return t != nil && t.Kind == types.KindContainer
}

// checkBorrowModification reports a structural modification of a container
// while a view of its buffer is live. The language forbids it because the view
// borrows the buffer rather than the elements, and the modification may hand
// the container a different one.
func (b *builder) checkBorrowModification(container ast.Expr, at ast.Node) {
	if len(b.borrows) == 0 {
		return
	}
	key := b.containerKey(container)
	if key == "" {
		return
	}
	for _, borrow := range b.borrows {
		if borrow.Key != key {
			continue
		}
		b.diags = append(b.diags, diag.New(diag.CodeBorrowModified, at.Span(),
			"the container is structurally modified while a view of its buffer is live").
			WithHelp("copy what the view names into a container of its own, or modify the container before taking the view"))
		return
	}
}

// containerKey names the container an expression reaches, so two mentions of
// the same container compare equal.
func (b *builder) containerKey(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		if sym := b.rinfo.Defs[v]; sym != nil {
			return sym.ID
		}
	case *ast.SelectorExpr:
		base := b.containerKey(v.X)
		if base == "" {
			return ""
		}
		return base + "." + v.Name.Name
	}
	return ""
}
