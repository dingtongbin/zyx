// Type expressions.

package ast

import "zyx/internal/source"

// TypeName is a named type: `i64`, `Server`, `Array<i64>`, `net.Conn`.
// The keywords `Self` and `type` also appear as bare names.
type TypeName struct {
	Sp source.Span
	// Qual is the optional package qualifier, `net` in `net.Conn`.
	Qual *Ident
	// Name is the type name.
	Name *Ident
	// Args holds the generic type arguments; nil without `<...>`.
	Args []TypeExpr
}

// Span returns the type source range.
func (n *TypeName) Span() source.Span { return n.Sp }

func (n *TypeName) typeNode() {}

// TupleType is a parenthesized tuple type `(A, B)`; unit tuples do not
// exist.
type TupleType struct {
	Sp source.Span
	// Elems holds the element types; tuples always have two or more.
	Elems []TypeExpr
}

// Span returns the type source range.
func (n *TupleType) Span() source.Span { return n.Sp }

func (n *TupleType) typeNode() {}

// SliceType is the read-only view type `[T]`.
type SliceType struct {
	Sp source.Span
	// Elem is the element type.
	Elem TypeExpr
}

// Span returns the type source range.
func (n *SliceType) Span() source.Span { return n.Sp }

func (n *SliceType) typeNode() {}

// ArrayType is the fixed-size array type `[T; N]`.
type ArrayType struct {
	Sp source.Span
	// Elem is the element type.
	Elem TypeExpr
	// Len is the comptime element count.
	Len Expr
}

// Span returns the type source range.
func (n *ArrayType) Span() source.Span { return n.Sp }

func (n *ArrayType) typeNode() {}

// FuncType is the function type `func(A, B) -> C`; Result is nil for a
// result-free function type.
type FuncType struct {
	Sp source.Span
	// Params holds the parameter types.
	Params []TypeExpr
	// Result is the optional result type.
	Result TypeExpr
}

// Span returns the type source range.
func (n *FuncType) Span() source.Span { return n.Sp }

func (n *FuncType) typeNode() {}

// ChanType is the channel type `chan<T>`.
type ChanType struct {
	Sp source.Span
	// Elem is the element type.
	Elem TypeExpr
}

// Span returns the type source range.
func (n *ChanType) Span() source.Span { return n.Sp }

func (n *ChanType) typeNode() {}
