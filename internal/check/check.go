// Package check type-checks resolved programs. It turns every declaration
// into a type, enforces the declaration rules of the language, and records
// the member table of every declared type.
//
// A package is checked in one pass over its declarations. Named types are
// declared before any signature is read, so declarations may refer to one
// another in any order; aliases resolve on demand, because a type alias is
// fully transparent and denotes its target type.
//
// This stage covers declarations: type expressions, fields and variants,
// method and function signatures, impl completeness and constraint
// coverage, and the shapes of main and init. The bodies of functions and
// the expressions inside them are checked by the expression and statement
// passes that follow.
package check

import (
	"strconv"
	"strings"

	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/resolve"
	"zyx/internal/source"
	"zyx/internal/toolchain"
	"zyx/internal/types"
)

// void is the type of an expression that produces no value, such as a call
// to a function with no result.
var void = &types.Type{Kind: types.Invalid}

// Imports supplies the checked surface of imported packages. The caller
// checks imported packages first, so the graph is traversed in dependency
// order.
type Imports interface {
	// Lookup returns the checked surface of the package at path.
	Lookup(path string) (*Interface, bool)
}

// An Interface is the checked surface of one package: the declared type of
// every top-level name, and the member tables of its public types.
type Interface struct {
	// Path is the import path.
	Path string
	// Name is the package name, which is also the namespace qualifier.
	Name string
	// Decls maps every top-level name to its declared type.
	Decls map[string]*types.Type
	// Declared maps the symbol id of every public type to its members.
	Declared map[string]*Decl
}

// A Const is one constant declared in a type body.
type Const struct {
	// Sym is the constant's symbol, so the checker can evaluate its value
	// on demand.
	Sym *resolve.Symbol
	// Type is the constant's declared type.
	Type *types.Type
}

// A Decl is the checked form of one type declaration.
type Decl struct {
	// Sym is the declaration symbol.
	Sym *resolve.Symbol
	// Type is the named type the declaration introduces.
	Type *types.Type
	// Params holds the declared type parameters in order.
	Params []*types.Type
	// Fields holds the fields of a struct, extern struct, or union.
	Fields []*Field
	// Variants holds the variants of an enum.
	Variants []*Variant
	// Methods maps each method name to its signature.
	Methods map[string]*types.Type
	// Consts maps each constant name to its declaration.
	Consts map[string]*Const
	// Impls holds the interfaces the type implements, in source order.
	Impls []*Impl
	// ParamConstraints maps each declared type parameter to the interfaces
	// its declaration requires.
	ParamConstraints map[string][]*types.Type

	field   map[string]*Field
	variant map[string]*Variant
	impl    map[string]bool
}

// An Impl records one checked impl block.
type Impl struct {
	// Interface is the implemented interface, with the impl's type
	// arguments applied.
	Interface *types.Type
	// Sp covers the impl block.
	Sp source.Span
	// Methods maps each implemented method name to its signature.
	Methods map[string]*types.Type
	// MethodSyms maps each implemented method name to its symbol, for
	// diagnostics.
	MethodSyms map[string]*resolve.Symbol
}

// Field returns the field of a struct or union with the given name, or nil.
func (d *Decl) Field(name string) *Field {
	if d == nil {
		return nil
	}
	if f, ok := d.field[name]; ok {
		return f
	}
	for _, f := range d.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// Variant returns the enum variant with the given name, or nil.
func (d *Decl) Variant(name string) *Variant {
	if d == nil {
		return nil
	}
	if v, ok := d.variant[name]; ok {
		return v
	}
	for _, v := range d.Variants {
		if v.Name == name {
			return v
		}
	}
	return nil
}

// A Field is one checked struct or union field.
type Field struct {
	// Sym is the field symbol.
	Sym *resolve.Symbol
	// Name is the field name.
	Name string
	// Type is the field type.
	Type *types.Type
}

// A Variant is one checked enum variant.
type Variant struct {
	// Sym is the variant symbol.
	Sym *resolve.Symbol
	// Name is the variant name.
	Name string
	// Payload holds the payload types; nil for a payload-free variant.
	Payload []*types.Type
}

// A Package is the checked form of one package.
type Package struct {
	// Path is the import path.
	Path string
	// Name is the package name.
	Name string
	// Info is the resolution result the check consumed.
	Info *resolve.Info
	// Types maps each declared type name to its type. An alias maps to its
	// target, because aliases are transparent.
	Types map[string]*types.Type
	// Decls maps each top-level name to its declared type.
	Decls map[string]*types.Type
	// Declared maps each declared type's symbol id to its members.
	Declared map[string]*Decl
	// Nodes maps each checked type expression and declaration name to its
	// type.
	Nodes map[ast.Node]*types.Type
	// Consts maps each evaluated constant to its value.
	Consts map[string]*Value
	// Interface is the surface downstream packages consume.
	Interface *Interface
}

// Check type-checks one package's declarations for a target. The target
// decides the layout queries a compile-time expression may run.
func Check(pkg *resolve.Package, info *resolve.Info, imports Imports, target toolchain.Target) (*Package, []diag.Diagnostic) {
	c := newChecker(pkg, info, imports, target)
	c.run()
	return c.out, c.diags
}

// checker carries the state of one package's check.
type checker struct {
	pkg     *resolve.Package
	info    *resolve.Info
	imports Imports

	path string
	name string

	out   *Package
	diags []diag.Diagnostic

	// bySym maps a declaration symbol's id to the type it declares. An
	// alias maps to its target.
	bySym map[string]*types.Type
	// params maps a type parameter's symbol id to its type while the
	// declaration that owns it is checked.
	params map[string]*types.Type
	// aliases tracks the alias chain being resolved, so a cycle reports
	// once instead of recursing forever.
	aliases map[*ast.TypeAlias]bool
	// self is the type Self names. Inside an interface body it is the
	// implementer, which the declaration cannot know, so it stays a
	// parameter of the interface.
	self *types.Type
	// declOrder holds the checked type declarations in source order, so
	// later passes produce diagnostics in a stable order.
	declOrder []*Decl
	// result is the result type of the function whose body is checked.
	result *types.Type
	// inComptime marks a comptime context.
	inComptime bool
	// inRegion counts the enclosing region blocks, which spawn requires.
	inRegion int
	// addrOK marks a C call argument list, where &x is allowed.
	addrOK bool
	// target is the target the package is checked for.
	target toolchain.Target
	// consts caches the value of every constant, keyed by symbol id.
	consts map[string]*Value
	// frames holds the compile-time call frames, innermost last.
	frames []*frame
}

// newChecker returns a checker with the package's output maps in place.
func newChecker(pkg *resolve.Package, info *resolve.Info, imports Imports, target toolchain.Target) *checker {
	c := &checker{
		pkg:     pkg,
		info:    info,
		imports: imports,
		path:    pkg.Path,
		target:  target,
		bySym:   map[string]*types.Type{},
		aliases: map[*ast.TypeAlias]bool{},
		consts:  map[string]*Value{},
	}
	c.out = &Package{
		Path:     c.path,
		Types:    map[string]*types.Type{},
		Decls:    map[string]*types.Type{},
		Declared: map[string]*Decl{},
		Nodes:    map[ast.Node]*types.Type{},
		Consts:   map[string]*Value{},
	}
	if info != nil {
		c.out.Name = info.Name
		c.name = info.Name
	}
	return c
}

// run performs the check passes in order.
func (c *checker) run() {
	c.declareTypes()
	c.checkDecls()
	c.checkImpls()
	c.checkBodies()
	c.buildInterface()
}

// declareTypes registers the named type of every type declaration before
// any signature is read, so declarations may refer to one another in any
// order.
func (c *checker) declareTypes() {
	for _, f := range c.pkg.Files {
		for _, d := range f.Decls {
			switch v := d.(type) {
			case *ast.StructDecl:
				kind := types.KindStruct
				if v.Extern {
					kind = types.KindExternStruct
				}
				c.declareNamed(v.Name, kind, v.TypeParams, v)
			case *ast.UnionDecl:
				c.declareNamed(v.Name, types.KindUnion, nil, v)
			case *ast.EnumDecl:
				c.declareNamed(v.Name, types.KindEnum, v.TypeParams, v)
			case *ast.InterfaceDecl:
				c.declareNamed(v.Name, types.KindInterface, v.TypeParams, v)
			}
		}
	}
}

// declareNamed registers one named type and its declared parameters.
func (c *checker) declareNamed(id *ast.Ident, kind types.Kind, params []*ast.TypeParam, decl ast.Node) {
	sym := c.symOf(id)
	if sym == nil {
		return
	}
	named := types.Named(kind, c.path, "", id.Name, len(params))
	c.bySym[sym.ID] = named
	c.out.Types[id.Name] = named
	c.out.Decls[id.Name] = named
	c.out.Nodes[id] = named

	d := &Decl{
		Sym:              sym,
		Type:             named,
		Methods:          map[string]*types.Type{},
		Consts:           map[string]*Const{},
		ParamConstraints: map[string][]*types.Type{},
		field:            map[string]*Field{},
		variant:          map[string]*Variant{},
		impl:             map[string]bool{},
	}
	c.out.Declared[sym.ID] = d
	c.declOrder = append(c.declOrder, d)
	d.Params = c.paramTypes(params)
	c.recordConstraints(d, params)
}

// recordConstraints records the interfaces each declared type parameter
// requires, so an impl block can be checked for weakening them.
func (c *checker) recordConstraints(d *Decl, params []*ast.TypeParam) {
	for _, tp := range params {
		if tp.Name == nil || len(tp.Constraints) == 0 {
			continue
		}
		ifaces := make([]*types.Type, 0, len(tp.Constraints))
		for _, con := range tp.Constraints {
			ifaces = append(ifaces, c.buildType(con))
		}
		d.ParamConstraints[tp.Name.Name] = ifaces
	}
}

// paramTypes returns the type of every declared type parameter, in order.
func (c *checker) paramTypes(params []*ast.TypeParam) []*types.Type {
	out := make([]*types.Type, 0, len(params))
	for _, tp := range params {
		if tp.Name == nil {
			continue
		}
		if sym := c.symOf(tp.Name); sym != nil {
			out = append(out, types.Param(sym.ID, tp.Name.Name))
		}
	}
	return out
}

// withParams runs fn with the type parameters of one declaration in scope.
func (c *checker) withParams(params []*ast.TypeParam, fn func()) {
	prev := c.params
	c.params = map[string]*types.Type{}
	for _, tp := range c.paramTypes(params) {
		c.params[tp.ID] = tp
	}
	fn()
	c.params = prev
}

// buildType turns a type expression into a type. It returns nil when the
// expression cannot be typed; the diagnostic is already reported.
func (c *checker) buildType(t ast.TypeExpr) *types.Type {
	if t == nil {
		return nil
	}
	switch v := t.(type) {
	case *ast.TypeName:
		return c.buildTypeName(v)
	case *ast.TupleType:
		elems := make([]*types.Type, 0, len(v.Elems))
		for _, e := range v.Elems {
			elems = append(elems, c.buildType(e))
		}
		out := types.TupleOf(elems...)
		c.out.Nodes[v] = out
		return out
	case *ast.SliceType:
		out := types.SliceOf(c.buildType(v.Elem))
		c.out.Nodes[v] = out
		return out
	case *ast.ArrayType:
		out := types.ArrayOf(c.buildType(v.Elem), c.arrayLength(v.Len))
		c.out.Nodes[v] = out
		return out
	case *ast.FuncType:
		params := make([]*types.Type, 0, len(v.Params))
		for _, p := range v.Params {
			params = append(params, c.buildType(p))
		}
		out := types.FuncOf(c.buildType(v.Result), params...)
		c.out.Nodes[v] = out
		return out
	case *ast.ChanType:
		out := types.ChanOf(c.buildType(v.Elem))
		c.out.Nodes[v] = out
		return out
	}
	return nil
}

// buildTypeName turns `Name`, `pkg.Name`, `c.Name`, `Self`, or `type` into
// a type.
func (c *checker) buildTypeName(v *ast.TypeName) *types.Type {
	base := c.baseType(v)
	if base == nil {
		return nil
	}
	if len(v.Args) == 0 {
		return base
	}
	if base.Kind == types.KindTypeParam {
		c.errorHelp(diag.CodeTypeArity, v.Sp,
			"a type parameter takes no arguments",
			"type parameter %s cannot take type arguments", base.Name)
		return base
	}
	if base.Arity != len(v.Args) {
		c.errorHelp(diag.CodeTypeArity, v.Sp,
			"give exactly the declared number of type arguments",
			"%s takes %d type arguments, got %d", base.Name, base.Arity, len(v.Args))
		return base
	}
	args := make([]*types.Type, 0, len(v.Args))
	for _, a := range v.Args {
		args = append(args, c.buildType(a))
	}
	out := base.Instantiate(args...)
	c.out.Nodes[v] = out
	return out
}

// baseType resolves the name at the head of a type expression.
func (c *checker) baseType(v *ast.TypeName) *types.Type {
	if v.Qual != nil {
		return c.qualifiedType(v)
	}
	if v.Name == nil {
		return nil
	}
	switch v.Name.Name {
	case "Self":
		if c.self == nil {
			return nil
		}
		c.out.Nodes[v.Name] = c.self
		return c.self
	case "type":
		c.out.Nodes[v.Name] = types.TypeValue
		return types.TypeValue
	}
	sym := c.symOf(v.Name)
	if sym == nil {
		return nil
	}
	out := c.typeOfSymbol(sym)
	if out != nil {
		c.out.Nodes[v.Name] = out
	}
	return out
}

// qualifiedType resolves `pkg.Name` in type position.
func (c *checker) qualifiedType(v *ast.TypeName) *types.Type {
	pkg := c.symOf(v.Qual)
	if pkg == nil || v.Name == nil {
		return nil
	}
	if pkg.Name == "c" {
		// A C type is opaque until the header symbol table arrives from
		// the toolchain stage, so it is modelled as a C value type.
		out := types.Named(types.KindExternStruct, "", "", "c."+v.Name.Name, 0)
		c.out.Nodes[v] = out
		return out
	}
	member := c.symOf(v.Name)
	if member == nil {
		return nil
	}
	if c.imports == nil {
		return nil
	}
	itf, ok := c.imports.Lookup(pkg.ID)
	if !ok || itf == nil {
		return nil
	}
	out, ok := itf.Decls[v.Name.Name]
	if !ok {
		return nil
	}
	// The imported type keeps its own package for identity and its package
	// name for text.
	out = withQual(out, pkg.Name)
	c.out.Nodes[v] = out
	return out
}

// withQual returns a copy of a named type that renders with a qualifier.
func withQual(t *types.Type, qual string) *types.Type {
	if t == nil || !t.IsNamed() || t.Qual == qual {
		return t
	}
	out := *t
	out.Qual = qual
	return &out
}

// typeOfSymbol returns the type a type symbol denotes. An alias denotes its
// target, because aliases are fully transparent.
func (c *checker) typeOfSymbol(sym *resolve.Symbol) *types.Type {
	if sym == nil {
		return nil
	}
	if t, ok := c.bySym[sym.ID]; ok {
		return t
	}
	switch sym.Kind {
	case resolve.KindTypeParam:
		return c.params[sym.ID]
	case resolve.KindBuiltin:
		return types.Predeclared(sym.Name)
	case resolve.KindCSymbol:
		return types.Named(types.KindExternStruct, "", "", "c."+sym.Name, 0)
	}
	alias, ok := sym.Decl.(*ast.TypeAlias)
	if !ok {
		return nil
	}
	return c.resolveAlias(alias)
}

// resolveAlias builds the target of a type alias once and registers it. An
// alias is transparent, so the alias name and its target are one type.
func (c *checker) resolveAlias(v *ast.TypeAlias) *types.Type {
	sym := c.symOf(v.Name)
	if sym != nil {
		if t, ok := c.bySym[sym.ID]; ok {
			return t
		}
	}
	if c.aliases[v] {
		c.errorHelp(diag.CodeTypeMismatch, v.Name.Sp,
			"break the cycle",
			"type alias %q refers to itself", v.Name.Name)
		return nil
	}
	c.aliases[v] = true
	t := c.buildType(v.Type)
	delete(c.aliases, v)
	if sym != nil {
		c.bySym[sym.ID] = t
	}
	if t != nil {
		c.out.Types[v.Name.Name] = t
		c.out.Decls[v.Name.Name] = t
	}
	return t
}

// arrayLength evaluates the length of a fixed-size array. The length must
// be a compile-time integer, which may be a constant rather than a literal.
func (c *checker) arrayLength(e ast.Expr) uint64 {
	v := c.eval(e)
	if v == nil || v.Type == nil || !v.Type.IsInteger() {
		c.errorHelp(diag.CodeArrayLength, spanOf(e),
			"write a constant integer, such as [u8; 16] or [u8; N]",
			"an array length must be a constant integer")
		return 0
	}
	c.out.Nodes[e] = v.Type
	return v.Int
}

// parseCount reads the digits of a constant integer literal, dropping the
// digit separators the language allows.
func parseCount(text string) uint64 {
	n, err := strconv.ParseUint(strings.ReplaceAll(text, "_", ""), 0, 64)
	if err != nil {
		return 0
	}
	return n
}

// symOf returns the symbol resolution bound to an identifier.
func (c *checker) symOf(id *ast.Ident) *resolve.Symbol {
	if id == nil || c.info == nil {
		return nil
	}
	return c.info.Defs[id]
}

// buildInterface collects the surface downstream packages consume.
func (c *checker) buildInterface() {
	itf := &Interface{
		Path:     c.path,
		Name:     c.name,
		Decls:    c.out.Decls,
		Declared: map[string]*Decl{},
	}
	for id, d := range c.out.Declared {
		if d.Sym != nil && d.Sym.Public {
			itf.Declared[id] = d
		}
	}
	c.out.Interface = itf
}
