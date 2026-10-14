// Type member declarations: struct, enum, interface, impl, fields, variants.

package ast

import "zyx/internal/source"

// FieldDecl declares one struct field.
type FieldDecl struct {
	Sp source.Span
	// Doc is the attached doc comment, if any.
	Doc *Doc
	// Name is the field identifier.
	Name *Ident
	// Type is the field type.
	Type TypeExpr
}

// Span returns the field source range.
func (n *FieldDecl) Span() source.Span { return n.Sp }

func (n *FieldDecl) declNode() {}

// StructDecl declares a struct. Extern selects the C-compatible layout form.
type StructDecl struct {
	Sp source.Span
	// Doc is the attached doc comment, if any.
	Doc *Doc
	// Extern marks `extern struct`.
	Extern bool
	// Name is the type identifier.
	Name *Ident
	// TypeParams holds the generic type parameters.
	TypeParams []*TypeParam
	// Members holds fields, methods, impl blocks, and constants in source
	// order. Only *FieldDecl, *FuncDecl, *ImplBlock, and *ConstDecl appear.
	Members []Decl
}

// Span returns the declaration source range.
func (n *StructDecl) Span() source.Span { return n.Sp }

func (n *StructDecl) declNode() {}

// VariantDecl declares one enum variant with an optional positional payload.
type VariantDecl struct {
	Sp source.Span
	// Doc is the attached doc comment, if any.
	Doc *Doc
	// Name is the variant identifier.
	Name *Ident
	// Payload holds the payload types; nil for a payload-free variant.
	Payload []TypeExpr
}

// Span returns the variant source range.
func (n *VariantDecl) Span() source.Span { return n.Sp }

func (n *VariantDecl) declNode() {}

// EnumDecl declares an enum.
type EnumDecl struct {
	Sp source.Span
	// Doc is the attached doc comment, if any.
	Doc *Doc
	// Name is the type identifier.
	Name *Ident
	// TypeParams holds the generic type parameters.
	TypeParams []*TypeParam
	// Members holds variants, methods, impl blocks, and constants in source
	// order. Only *VariantDecl, *FuncDecl, *ImplBlock, and *ConstDecl appear.
	Members []Decl
}

// Span returns the declaration source range.
func (n *EnumDecl) Span() source.Span { return n.Sp }

func (n *EnumDecl) declNode() {}

// InterfaceDecl declares an interface; its body holds method signatures.
type InterfaceDecl struct {
	Sp source.Span
	// Doc is the attached doc comment, if any.
	Doc *Doc
	// Name is the interface identifier.
	Name *Ident
	// TypeParams holds the generic type parameters.
	TypeParams []*TypeParam
	// Methods holds the required method signatures in source order.
	Methods []*FuncDecl
}

// Span returns the declaration source range.
func (n *InterfaceDecl) Span() source.Span { return n.Sp }

func (n *InterfaceDecl) declNode() {}

// ImplBlock declares one interface implementation inside a type body.
type ImplBlock struct {
	Sp source.Span
	// Doc is the attached doc comment, if any.
	Doc *Doc
	// TypeParams holds the restated type parameters and extra constraints.
	TypeParams []*TypeParam
	// Interface is the implemented interface type.
	Interface TypeExpr
	// Methods holds the implementation signatures in source order.
	Methods []*FuncDecl
}

// Span returns the declaration source range.
func (n *ImplBlock) Span() source.Span { return n.Sp }

func (n *ImplBlock) declNode() {}
