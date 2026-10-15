// Statements.

package ast

import (
	"zyx/internal/source"
	"zyx/internal/token"
)

// BlockStmt is a brace-delimited statement list.
type BlockStmt struct {
	Sp source.Span
	// Stmts holds the block statements in source order.
	Stmts []Stmt
}

// Span returns the block source range.
func (n *BlockStmt) Span() source.Span { return n.Sp }

func (n *BlockStmt) stmtNode() {}

// ExprStmt evaluates an expression for its effects. The parser restricts
// this statement to calls, spawn expressions, fast calls, and try calls.
type ExprStmt struct {
	Sp source.Span
	// X is the evaluated expression.
	X Expr
}

// Span returns the statement source range.
func (n *ExprStmt) Span() source.Span { return n.Sp }

func (n *ExprStmt) stmtNode() {}

// AssignStmt assigns to a variable, field, or index.
type AssignStmt struct {
	Sp source.Span
	// Target is the assignment destination.
	Target Expr
	// Op is `=` or one of the compound assignment operators.
	Op token.Kind
	// Value is the assigned expression.
	Value Expr
}

// Span returns the statement source range.
func (n *AssignStmt) Span() source.Span { return n.Sp }

func (n *AssignStmt) stmtNode() {}

// IfStmt branches on a condition; it produces no value.
type IfStmt struct {
	Sp source.Span
	// Cond is the condition expression.
	Cond Expr
	// Then is the then branch: a *BlockStmt or a single-line statement.
	Then Stmt
	// Else is the optional else branch: a *BlockStmt, a single-line
	// statement, or an *IfStmt for `else if`.
	Else Stmt
}

// Span returns the statement source range.
func (n *IfStmt) Span() source.Span { return n.Sp }

func (n *IfStmt) stmtNode() {}

// ForStmt is one of the three loop forms. Label is set only on a labeled
// loop; Bindings is set only for the iterator form; Iter holds the
// condition of a while-style loop or the iterable expression.
type ForStmt struct {
	Sp source.Span
	// Label is the optional loop label.
	Label *Ident
	// Bindings holds the iterator binding list; nil for the other forms.
	Bindings []*Ident
	// Iter is the loop head; nil selects the infinite form.
	Iter Expr
	// Body is the loop body: a *BlockStmt or a single-line statement.
	Body Stmt
}

// Span returns the statement source range.
func (n *ForStmt) Span() source.Span { return n.Sp }

func (n *ForStmt) stmtNode() {}

// BreakStmt leaves a loop, optionally targeting a label.
type BreakStmt struct {
	Sp source.Span
	// Label is the optional target label.
	Label *Ident
}

// Span returns the statement source range.
func (n *BreakStmt) Span() source.Span { return n.Sp }

func (n *BreakStmt) stmtNode() {}

// ContinueStmt continues a loop, optionally targeting a label.
type ContinueStmt struct {
	Sp source.Span
	// Label is the optional target label.
	Label *Ident
}

// Span returns the statement source range.
func (n *ContinueStmt) Span() source.Span { return n.Sp }

func (n *ContinueStmt) stmtNode() {}

// ReturnStmt returns from the current function.
type ReturnStmt struct {
	Sp source.Span
	// Values holds the returned expressions; empty for a bare return.
	Values []Expr
}

// Span returns the statement source range.
func (n *ReturnStmt) Span() source.Span { return n.Sp }

func (n *ReturnStmt) stmtNode() {}

// DeferStmt defers a call to the exit of the enclosing function or region.
type DeferStmt struct {
	Sp source.Span
	// Call is the deferred call; the parser requires a call expression.
	Call Expr
}

// Span returns the statement source range.
func (n *DeferStmt) Span() source.Span { return n.Sp }

func (n *DeferStmt) stmtNode() {}

// RegionStmt opens an independent memory region; coroutines may only start
// lexically inside one.
type RegionStmt struct {
	Sp source.Span
	// Body is the region block.
	Body *BlockStmt
}

// Span returns the statement source range.
func (n *RegionStmt) Span() source.Span { return n.Sp }

func (n *RegionStmt) stmtNode() {}

// ComptimeBlockStmt evaluates its block at compile time.
type ComptimeBlockStmt struct {
	Sp source.Span
	// Body is the compile-time block.
	Body *BlockStmt
}

// Span returns the statement source range.
func (n *ComptimeBlockStmt) Span() source.Span { return n.Sp }

func (n *ComptimeBlockStmt) stmtNode() {}

// MatchStmt dispatches on a value's shape; it produces no value.
type MatchStmt struct {
	Sp source.Span
	// Subject is the matched expression.
	Subject Expr
	// Arms holds the match arms in source order.
	Arms []*MatchArm
}

// Span returns the statement source range.
func (n *MatchStmt) Span() source.Span { return n.Sp }

func (n *MatchStmt) stmtNode() {}

// MatchArm is one pattern arm of a match statement.
type MatchArm struct {
	Sp source.Span
	// Pattern is the arm pattern.
	Pattern Pattern
	// Guard is the optional `if` condition.
	Guard Expr
	// Body is the arm body: a *BlockStmt or a single-line statement.
	Body Stmt
}

// Span returns the arm source range.
func (n *MatchArm) Span() source.Span { return n.Sp }

// SelectKind discriminates the four select arm forms.
type SelectKind uint8

// Select arm forms.
const (
	// SelectRecv is the `recv(ch) -> v, ok` arm.
	SelectRecv SelectKind = iota
	// SelectSend is the `send(ch, v)` arm.
	SelectSend
	// SelectTimeout is the `timeout(d)` arm.
	SelectTimeout
	// SelectDefault is the bare `default` arm.
	SelectDefault
)

// SelectArm is one arm of a select statement. Arm order is priority order.
type SelectArm struct {
	Sp source.Span
	// Kind selects the arm form.
	Kind SelectKind
	// Chan is the receive channel or the send channel.
	Chan Expr
	// Value is the send payload or the timeout duration.
	Value Expr
	// RecvValue binds the received value; `_` is allowed.
	RecvValue *Ident
	// RecvOk binds the delivery flag; `_` is allowed.
	RecvOk *Ident
	// Body is the arm body: a *BlockStmt or a single-line statement.
	Body Stmt
}

// Span returns the arm source range.
func (n *SelectArm) Span() source.Span { return n.Sp }

// SelectStmt multiplexes channel operations; arm order decides priority.
type SelectStmt struct {
	Sp source.Span
	// Arms holds the select arms in source order.
	Arms []*SelectArm
}

// Span returns the statement source range.
func (n *SelectStmt) Span() source.Span { return n.Sp }

func (n *SelectStmt) stmtNode() {}
