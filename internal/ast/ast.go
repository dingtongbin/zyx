// Package ast defines the syntax tree produced by the parser.
package ast

import "zyx/internal/source"

// Node is the interface implemented by every syntax tree node.
type Node interface {
	// Span returns the source range the node was parsed from.
	Span() source.Span
}

// Decl is a declaration: a top-level binding or a type member.
type Decl interface {
	Node
	declNode()
}

// Stmt is a statement, including local declarations.
type Stmt interface {
	Node
	stmtNode()
}

// Expr is an expression.
type Expr interface {
	Node
	exprNode()
}

// Pattern is a match pattern.
type Pattern interface {
	Node
	patternNode()
}

// TypeExpr is a type expression such as Array<i64> or (i64, str).
type TypeExpr interface {
	Node
	typeNode()
}

// Ident is a name occurrence in code. It is also an expression, so qualified
// names such as net.Listen parse as a selector over an identifier.
type Ident struct {
	// Sp covers the identifier text.
	Sp source.Span
	// Name is the identifier text.
	Name string
}

// Span returns the identifier source range.
func (n *Ident) Span() source.Span { return n.Sp }

func (n *Ident) exprNode() {}

// Doc is the doc comment attached to a declaration.
type Doc struct {
	// Sp covers the comment lines.
	Sp source.Span
	// Lines holds one entry per comment line, without the leading slashes.
	Lines []string
}

// File is one parsed source file.
type File struct {
	Sp source.Span
	// Package is the required package declaration.
	Package *PackageDecl
	// Imports holds the import declarations in source order.
	Imports []*ImportDecl
	// Decls holds the remaining top-level declarations in source order.
	Decls []Decl
}

// Span returns the file source range.
func (n *File) Span() source.Span { return n.Sp }
