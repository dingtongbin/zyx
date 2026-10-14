// Match patterns.

package ast

import "zyx/internal/source"

// LitPattern matches a literal value; the parser rejects float literals.
type LitPattern struct {
	Sp source.Span
	// X is the literal, possibly a negative integer.
	X *Literal
}

// Span returns the pattern source range.
func (n *LitPattern) Span() source.Span { return n.Sp }

func (n *LitPattern) patternNode() {}

// WildcardPattern matches anything: `_`.
type WildcardPattern struct {
	Sp source.Span
}

// Span returns the pattern source range.
func (n *WildcardPattern) Span() source.Span { return n.Sp }

func (n *WildcardPattern) patternNode() {}

// BindPattern binds the matched value to a name for the arm.
type BindPattern struct {
	Sp source.Span
	// Name is the bound identifier.
	Name *Ident
}

// Span returns the pattern source range.
func (n *BindPattern) Span() source.Span { return n.Sp }

func (n *BindPattern) patternNode() {}

// VariantPattern matches an enum variant, with payload patterns when
// written with parentheses.
type VariantPattern struct {
	Sp source.Span
	// Name is the variant identifier.
	Name *Ident
	// Payload holds the payload patterns; nil without parentheses.
	Payload []Pattern
}

// Span returns the pattern source range.
func (n *VariantPattern) Span() source.Span { return n.Sp }

func (n *VariantPattern) patternNode() {}

// TuplePattern matches a tuple elementwise; tuples always have two or
// more elements.
type TuplePattern struct {
	Sp source.Span
	// Elems holds the element patterns.
	Elems []Pattern
}

// Span returns the pattern source range.
func (n *TuplePattern) Span() source.Span { return n.Sp }

func (n *TuplePattern) patternNode() {}

// OrPattern matches any one of its alternatives: `p1 | p2 | p3`.
type OrPattern struct {
	Sp source.Span
	// Patterns holds the alternatives in source order.
	Patterns []Pattern
}

// Span returns the pattern source range.
func (n *OrPattern) Span() source.Span { return n.Sp }

func (n *OrPattern) patternNode() {}

// RangePattern matches a half-open range of integers or chars: `0..10`.
type RangePattern struct {
	Sp source.Span
	// Lo is the inclusive lower bound expression.
	Lo Expr
	// Hi is the exclusive upper bound expression.
	Hi Expr
}

// Span returns the pattern source range.
func (n *RangePattern) Span() source.Span { return n.Sp }

func (n *RangePattern) patternNode() {}
