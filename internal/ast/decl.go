// Top-level declarations.

package ast

import "zyx/internal/source"

// PackageDecl declares the package name. It must be the first declaration
// of a file.
type PackageDecl struct {
	// Sp covers the whole declaration.
	Sp source.Span
	// Doc is the attached doc comment, if any.
	Doc *Doc
	// Name is the package identifier.
	Name *Ident
}

// Span returns the declaration source range.
func (n *PackageDecl) Span() source.Span { return n.Sp }

func (n *PackageDecl) declNode() {}

// ImportDecl imports a package path or a C header.
type ImportDecl struct {
	Sp source.Span
	// Doc is the attached doc comment, if any.
	Doc *Doc
	// CHeader selects the `import c "..."` form for C headers.
	CHeader bool
	// Path holds the import path or header name from the string literal.
	Path string
	// PathSp covers the string literal.
	PathSp source.Span
	// Fast names the C functions of the header whose calls run on the
	// calling coroutine. The list is the import's own linkage declaration,
	// so a package states the blocking behavior of the functions it links.
	Fast []*Ident
}

// WithDefaults returns a call's arguments with the default expressions of the
// parameters it leaves out appended, in order. The language fills a default
// at the call site, so every pass that walks a call sees one argument list. A
// call that leaves out a parameter with no default is left as it is, because
// the check reports that.
func WithDefaults(args []Expr, params []*Param) []Expr {
	if len(params) <= len(args) {
		return args
	}
	out := make([]Expr, len(params))
	copy(out, args)
	for i := len(args); i < len(params); i++ {
		if params[i].Default == nil {
			return args
		}
		out[i] = params[i].Default
	}
	return out
}

// Span returns the declaration source range.
func (n *ImportDecl) Span() source.Span { return n.Sp }

func (n *ImportDecl) declNode() {}

// ConstDecl declares a constant. Local constants are statements too.
type ConstDecl struct {
	Sp source.Span
	// Doc is the attached doc comment, if any.
	Doc *Doc
	// Name is the constant identifier.
	Name *Ident
	// Type is the optional type annotation.
	Type TypeExpr
	// Value is the compile-time initializer.
	Value Expr
}

// Span returns the declaration source range.
func (n *ConstDecl) Span() source.Span { return n.Sp }

func (n *ConstDecl) declNode() {}
func (n *ConstDecl) stmtNode() {}

// VarDecl declares one or more variables sharing a single initializer.
type VarDecl struct {
	Sp source.Span
	// Doc is the attached doc comment, if any.
	Doc *Doc
	// Names holds the binding list; `_` is allowed as a discard.
	Names []*Ident
	// Type is the optional type annotation.
	Type TypeExpr
	// Value is the initializer, required by the language rules.
	Value Expr
}

// Span returns the declaration source range.
func (n *VarDecl) Span() source.Span { return n.Sp }

func (n *VarDecl) declNode() {}
func (n *VarDecl) stmtNode() {}

// TypeAlias declares a transparent type alias.
type TypeAlias struct {
	Sp source.Span
	// Doc is the attached doc comment, if any.
	Doc *Doc
	// Name is the alias identifier.
	Name *Ident
	// Type is the aliased type.
	Type TypeExpr
}

// Span returns the declaration source range.
func (n *TypeAlias) Span() source.Span { return n.Sp }

func (n *TypeAlias) declNode() {}

// FuncQualifiers records the qualifier set of a function declaration.
type FuncQualifiers struct {
	// Comptime marks `comptime func`.
	Comptime bool
	// Extern marks `extern func`, a C ABI declaration without definition.
	Extern bool
	// CAbi marks `c func`, a C ABI definition.
	CAbi bool
	// Fast marks `fast extern func`, a C symbol whose calls run on the
	// calling coroutine instead of through the blocking pool. Whether a C
	// function blocks is a property of the function, so it is declared with
	// the linkage rather than written at each call.
	Fast bool
}

// Param is one function parameter.
type Param struct {
	Sp source.Span
	// Name is the parameter identifier.
	Name *Ident
	// Type is the parameter type; nil only for the trailing `...` form.
	Type TypeExpr
	// Default is the optional default value expression.
	Default Expr
	// Variadic marks the trailing `...` parameter of an extern func.
	Variadic bool
}

// Span returns the parameter source range.
func (n *Param) Span() source.Span { return n.Sp }

// TypeParam is one generic type parameter with optional constraints.
type TypeParam struct {
	Sp source.Span
	// Name is the type parameter identifier.
	Name *Ident
	// Constraints holds the `Eq + Show` interface list; nil when omitted.
	Constraints []TypeExpr
}

// Span returns the type parameter source range.
func (n *TypeParam) Span() source.Span { return n.Sp }

// FuncDecl declares a function, a method, or an interface signature. It
// also serves as the value-reference form `func Name` through FuncRefExpr
// in expression position; the parser never builds a FuncDecl there.
type FuncDecl struct {
	Sp source.Span
	// Doc is the attached doc comment, if any.
	Doc *Doc
	// Quals holds the comptime / extern / c qualifier set.
	Quals FuncQualifiers
	// Name is the function identifier.
	Name *Ident
	// TypeParams holds the generic type parameters.
	TypeParams []*TypeParam
	// Params holds the parameter list.
	Params []*Param
	// Result is the optional result type.
	Result TypeExpr
	// Body is the block body; nil for expression bodies and signatures.
	Body *BlockStmt
	// BodyExpr is the `=> Expr` body; nil otherwise.
	BodyExpr Expr
}

// Span returns the declaration source range.
func (n *FuncDecl) Span() source.Span { return n.Sp }

func (n *FuncDecl) declNode() {}
