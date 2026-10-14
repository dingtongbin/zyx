// Expressions.

package ast

import (
	"zyx/internal/source"
	"zyx/internal/token"
)

// Literal is a numeric, char, string, byte string, f-string, or bool
// literal. Numeric values stay as source text; decoding needs 128-bit
// range checks and belongs to the checker.
type Literal struct {
	Sp source.Span
	// Kind is the literal token kind: Int, Float, Char, Str, ByteStr,
	// FString, True, or False.
	Kind token.Kind
	// Value holds the decoded text of Char, Str, and ByteStr literals, the
	// source text of numeric literals, and the source text of True/False.
	Value string
	// Suffix carries the numeric literal type suffix.
	Suffix token.NumSuffix
	// Parts holds the f-string segments; nil for other literals.
	Parts []FStrPart
}

// Span returns the literal source range.
func (n *Literal) Span() source.Span { return n.Sp }

func (n *Literal) exprNode() {}

// FStrPart is one segment of an f-string literal.
type FStrPart struct {
	// Text holds the decoded literal segment; empty for interpolations.
	Text string
	// X is the parsed interpolation expression; nil for literal segments.
	X Expr
}

// BinaryExpr is a left-associative binary operator application.
type BinaryExpr struct {
	Sp source.Span
	// Op is the operator token kind.
	Op token.Kind
	// X and Y are the operands.
	X, Y Expr
}

// Span returns the expression source range.
func (n *BinaryExpr) Span() source.Span { return n.Sp }

func (n *BinaryExpr) exprNode() {}

// UnaryExpr is a prefix operator application: `-`, `!`, or `~`.
type UnaryExpr struct {
	Sp source.Span
	// Op is the operator token kind.
	Op token.Kind
	// X is the operand.
	X Expr
}

// Span returns the expression source range.
func (n *UnaryExpr) Span() source.Span { return n.Sp }

func (n *UnaryExpr) exprNode() {}

// AddrOfExpr takes the address of an lvalue as a C call argument: `&x`.
// The result is a raw pointer valid only for the duration of that call, so
// it cannot be stored, returned, compared, or sent across coroutines.
type AddrOfExpr struct {
	Sp source.Span
	// X is the addressed lvalue.
	X Expr
}

// Span returns the expression source range.
func (n *AddrOfExpr) Span() source.Span { return n.Sp }

func (n *AddrOfExpr) exprNode() {}

// IsExpr tests an interface value: `x is T`.
type IsExpr struct {
	Sp source.Span
	// X is the tested value.
	X Expr
	// Type is the interface type.
	Type TypeExpr
}

// Span returns the expression source range.
func (n *IsExpr) Span() source.Span { return n.Sp }

func (n *IsExpr) exprNode() {}

// CallExpr is a call: `callee(args)`.
type CallExpr struct {
	Sp source.Span
	// Fn is the callee: a name, selector, index, lambda, or paren form.
	Fn Expr
	// Args holds the arguments.
	Args []Expr
}

// Span returns the expression source range.
func (n *CallExpr) Span() source.Span { return n.Sp }

func (n *CallExpr) exprNode() {}

// SelectorExpr is a field access, a method value, or the tail of a
// qualified name (`net.Listen`).
type SelectorExpr struct {
	Sp source.Span
	// X is the value or qualifier expression.
	X Expr
	// Name is the selected member.
	Name *Ident
}

// Span returns the expression source range.
func (n *SelectorExpr) Span() source.Span { return n.Sp }

func (n *SelectorExpr) exprNode() {}

// TupleIndexExpr reads a tuple element by position: `t.0`. The index is a
// non-negative integer literal, so this cannot be a SelectorExpr.
type TupleIndexExpr struct {
	Sp source.Span
	// X is the tuple expression.
	X Expr
	// Index is the element position.
	Index int
}

// Span returns the expression source range.
func (n *TupleIndexExpr) Span() source.Span { return n.Sp }

func (n *TupleIndexExpr) exprNode() {}

// IndexExpr indexes a container or takes a slice: `a[i]`, `a[i..j]`.
type IndexExpr struct {
	Sp source.Span
	// X is the indexed value.
	X Expr
	// Index is the index or slice expression; a slice is a RangeExpr.
	Index Expr
}

// Span returns the expression source range.
func (n *IndexExpr) Span() source.Span { return n.Sp }

func (n *IndexExpr) exprNode() {}

// RangeExpr is the half-open range `lo..hi`. Both sides are required.
type RangeExpr struct {
	Sp source.Span
	// Lo is the inclusive lower bound.
	Lo Expr
	// Hi is the exclusive upper bound.
	Hi Expr
}

// Span returns the expression source range.
func (n *RangeExpr) Span() source.Span { return n.Sp }

func (n *RangeExpr) exprNode() {}

// NewExpr allocates through an init constructor: `new T(args)`.
type NewExpr struct {
	Sp source.Span
	// Type is the constructed type.
	Type TypeExpr
	// Args holds the constructor arguments.
	Args []Expr
}

// Span returns the expression source range.
func (n *NewExpr) Span() source.Span { return n.Sp }

func (n *NewExpr) exprNode() {}

// TryExpr propagates an Err result: `try call`.
type TryExpr struct {
	Sp source.Span
	// X is the expression returning a Result.
	X Expr
}

// Span returns the expression source range.
func (n *TryExpr) Span() source.Span { return n.Sp }

func (n *TryExpr) exprNode() {}

// SpawnExpr starts a coroutine and yields a JoinHandle. Watcher is set
// only for the spawn_watched form.
type SpawnExpr struct {
	Sp source.Span
	// X is the spawned call.
	X Expr
	// Watcher is the supervisor expression of spawn_watched; nil for spawn.
	Watcher Expr
}

// Span returns the expression source range.
func (n *SpawnExpr) Span() source.Span { return n.Sp }

func (n *SpawnExpr) exprNode() {}

// FuncRefExpr is a function value reference: `func Name` or `func pkg.Name`.
type FuncRefExpr struct {
	Sp source.Span
	// Qualifier is the optional package qualifier.
	Qualifier *Ident
	// Name is the referenced function.
	Name *Ident
}

// Span returns the expression source range.
func (n *FuncRefExpr) Span() source.Span { return n.Sp }

func (n *FuncRefExpr) exprNode() {}

// LambdaExpr is an anonymous function; it captures nothing by design.
type LambdaExpr struct {
	Sp source.Span
	// Params holds the parameter list.
	Params []*Param
	// Result is the optional result type.
	Result TypeExpr
	// Body is the block body; nil for expression bodies.
	Body *BlockStmt
	// BodyExpr is the `=> Expr` body; nil otherwise.
	BodyExpr Expr
}

// Span returns the expression source range.
func (n *LambdaExpr) Span() source.Span { return n.Sp }

func (n *LambdaExpr) exprNode() {}

// TupleExpr is a parenthesized tuple `(a, b)`; the parentheses of a single
// expression do not create a node.
type TupleExpr struct {
	Sp source.Span
	// Elems holds the tuple elements; tuples always have two or more.
	Elems []Expr
}

// Span returns the expression source range.
func (n *TupleExpr) Span() source.Span { return n.Sp }

func (n *TupleExpr) exprNode() {}

// ArrayLitExpr is an array literal `[1, 2, 3]`. A one-element literal
// followed by `*` parses as an ArrayRepeatExpr instead.
type ArrayLitExpr struct {
	Sp source.Span
	// Elems holds the elements.
	Elems []Expr
}

// Span returns the expression source range.
func (n *ArrayLitExpr) Span() source.Span { return n.Sp }

func (n *ArrayLitExpr) exprNode() {}

// ArrayRepeatExpr constructs an array from one repeated element:
// `[expr] * n`. A fixed-size array target requires a comptime n equal to
// its length; any other target produces Array<T> and accepts a runtime n.
type ArrayRepeatExpr struct {
	Sp source.Span
	// Elem is the repeated element.
	Elem Expr
	// Count is the repetition count.
	Count Expr
}

// Span returns the expression source range.
func (n *ArrayRepeatExpr) Span() source.Span { return n.Sp }

func (n *ArrayRepeatExpr) exprNode() {}

// AggregateLitExpr is a construction literal: `Point { x: 1 }`,
// `net.Point { x: 1 }`, `Map { "k": 1 }`, or `Set { 1, 2 }`. The head is a
// type name, so a literal can construct a type of another package, such as
// a C struct reached through `import c`.
type AggregateLitExpr struct {
	Sp source.Span
	// Type is the constructed type. A struct or union literal writes field
	// names as keys; a container literal writes ordinary expressions.
	Type *TypeName
	// Entries holds the entries; empty for `Name { }`.
	Entries []*AggregateEntry
}

// Span returns the expression source range.
func (n *AggregateLitExpr) Span() source.Span { return n.Sp }

func (n *AggregateLitExpr) exprNode() {}

// AggregateEntry is one element of an aggregate literal. Key is nil for
// the set form, where only a value is written.
type AggregateEntry struct {
	Sp source.Span
	// Key is the field name or map key; nil for a bare element.
	Key Expr
	// Value is the entry value or bare element.
	Value Expr
}

// Span returns the entry source range.
func (n *AggregateEntry) Span() source.Span { return n.Sp }

// FastExpr marks a C call that runs directly on the current coroutine:
// `fast c.f(...)`.
type FastExpr struct {
	Sp source.Span
	// X is the marked call.
	X Expr
}

// Span returns the expression source range.
func (n *FastExpr) Span() source.Span { return n.Sp }

func (n *FastExpr) exprNode() {}
