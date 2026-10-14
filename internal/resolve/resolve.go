// Package resolve performs name resolution for one package. It builds the
// package symbol table, binds every name occurrence to the declaration it
// names, and enforces the import, visibility, shadowing, and label rules of
// the language.
//
// Resolution covers the names the front end can settle without types:
// locals, parameters, package-level declarations, imported packages, the
// predeclared universe, type names, and the C namespace. Member references
// that depend on a receiver's type (field access, method calls, method
// values, and enum variants named in patterns) are bound by the type
// checker, which runs after resolution.
//
// Two kinds of name stay outside the symbol-id scheme of the toolchain
// document, because the .zyxc layer carries their source spelling instead:
// loop labels, which are unique within a function, and names that come from
// an imported C header.
package resolve

import (
	"strings"

	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/source"
	"zyx/internal/types"
)

// A Package is one package's parsed source files. Every file must declare
// the same package name, and that name must be the last segment of Path
// unless it is the command package `main`.
type Package struct {
	// Path is the import path, for example "std/net". Symbol ids are built
	// from it, so it must be the path other packages import.
	Path string
	// Files holds the parsed files.
	Files []*ast.File
}

// An Interface is the top-level declaration table of one resolved package.
// Cross-package resolution reads it instead of the package's full symbol
// table; visibility is enforced at each reference, so the table holds
// private declarations too and a reference to one reports a visibility
// error rather than an undefined name.
type Interface struct {
	// Path is the import path.
	Path string
	// Name is the package name, which is also the namespace qualifier.
	Name string
	// Decls maps every top-level name to its symbol.
	Decls map[string]*Symbol
}

// Imports resolves an import path to the public surface of that package.
// The caller resolves imported packages first, so the graph is traversed in
// dependency order.
type Imports interface {
	// Lookup returns the interface of the package at path.
	Lookup(path string) (*Interface, bool)
}

// Info is the result of resolving one package.
type Info struct {
	// Path is the import path of the package.
	Path string
	// Name is the resolved package name.
	Name string
	// Interface is the public surface, for packages that import this one.
	Interface *Interface
	// Symbols holds every symbol in creation order.
	Symbols []*Symbol
	// Defs maps every identifier occurrence and every lambda node to the
	// symbol it names. Declarations and references share the map.
	Defs map[ast.Node]*Symbol
}

// Resolve resolves one package and returns its symbols, the binding of
// every name, and the public interface that downstream packages consume.
func Resolve(pkg *Package, imports Imports) (*Info, []diag.Diagnostic) {
	r := newResolver(pkg, imports)
	r.run()
	return r.info, r.diags
}

// predeclaredNonTypes lists the predeclared names that are not types: the
// two comptime namespaces and the single global function. The type names
// come from the types package, which owns the predeclared type table, so
// the universe and the type model cannot drift apart.
var predeclaredNonTypes = []string{"meta", "dur", "panic"}

// fileScope holds the import bindings of one file. Imports are file
// scoped, matching the package rules the language copies from Go.
type fileScope struct {
	file    *ast.File
	scope   *scope
	imports []*importUse
	c       *Symbol
	cDecls  []*ast.ImportDecl
	cUsed   bool
}

// importUse tracks one ordinary import for the unused-import check.
type importUse struct {
	decl *ast.ImportDecl
	sym  *Symbol
	itf  *Interface
	used bool
}

// importOf returns the use record of an imported package qualifier.
func (f *fileScope) importOf(sym *Symbol) *importUse {
	for _, use := range f.imports {
		if use.sym == sym {
			return use
		}
	}
	return nil
}

// resolver carries the state of one package's resolution.
type resolver struct {
	pkg     *Package
	imports Imports

	path string
	name string

	pkgNameSpan source.Span

	universe *scope
	pkgScope *scope
	scope    *scope

	file       *fileScope
	fileScopes []*fileScope

	info  *Info
	diags []diag.Diagnostic

	types []*Symbol

	typeSym  *Symbol
	funcSym  *Symbol
	self     *Symbol
	exported bool
	localN   int
	labels   map[string]bool

	lambdaBase *scope

	cSyms map[string]*Symbol
}

// newResolver returns a resolver with the predeclared universe and the
// package scope in place.
func newResolver(pkg *Package, imports Imports) *resolver {
	r := &resolver{pkg: pkg, imports: imports, path: pkg.Path}
	if r.path == "" {
		r.path = "main"
	}
	r.info = &Info{Path: r.path, Defs: map[ast.Node]*Symbol{}}
	r.universe = newScope(nil, scopeUniverse)
	for _, name := range types.Names() {
		r.declareBuiltin(name)
	}
	for _, name := range predeclaredNonTypes {
		r.declareBuiltin(name)
	}
	r.pkgScope = newScope(r.universe, scopePackage)
	return r
}

// declareBuiltin adds one predeclared name to the universe scope.
func (r *resolver) declareBuiltin(name string) {
	r.universe.names[name] = &Symbol{
		ID:     name,
		Name:   name,
		Kind:   KindBuiltin,
		Public: true,
	}
}

// run performs the resolution passes in order.
func (r *resolver) run() {
	r.resolvePackageName()
	r.collectDecls()
	r.collectMembers()
	r.buildFileScopes()
	r.resolveDecls()
	r.checkUnusedImports()
	r.buildInterface()
}

// resolvePackageName checks the package declaration of every file against
// the other files, the reserved namespaces, and the import path.
func (r *resolver) resolvePackageName() {
	for _, f := range r.pkg.Files {
		if f.Package == nil || f.Package.Name == nil {
			continue
		}
		name := f.Package.Name.Name
		if r.name == "" {
			r.name = name
			r.pkgNameSpan = f.Package.Name.Sp
			continue
		}
		if name != r.name {
			r.diags = append(r.diags, diag.New(diag.CodePackageName, f.Package.Name.Sp,
				"package %s here, package %s in another file of the directory", name, r.name).
				WithHelp("give every file of the directory the same package name"))
		}
	}
	if r.name == "" {
		return
	}
	r.info.Name = r.name
	switch r.name {
	case "c", "meta", "dur":
		r.diags = append(r.diags, diag.New(diag.CodePackageName, r.pkgNameSpan,
			"package %s is a predeclared namespace", r.name).
			WithHelp("rename the package"))
		return
	}
	if r.pkg.Path == "" || r.name == "main" {
		return
	}
	if seg := lastSegment(r.pkg.Path); seg != r.name {
		r.diags = append(r.diags, diag.New(diag.CodePackageName, r.pkgNameSpan,
			"package %s does not match the last segment %q of the import path", r.name, seg).
			WithHelp("name the package after its directory"))
	}
}

// lastSegment returns the final path element of an import path.
func lastSegment(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// collectDecls builds a symbol for every top-level declaration.
func (r *resolver) collectDecls() {
	for _, f := range r.pkg.Files {
		for _, d := range f.Decls {
			r.declare(d)
		}
	}
}

// declare records one top-level declaration.
func (r *resolver) declare(d ast.Decl) {
	switch v := d.(type) {
	case *ast.ConstDecl:
		r.declareTop(v.Name, KindConst, v)
	case *ast.VarDecl:
		for _, name := range v.Names {
			r.declareTop(name, KindVar, v)
		}
	case *ast.TypeAlias:
		r.declareTop(v.Name, KindType, v)
	case *ast.FuncDecl:
		sym := r.declareTop(v.Name, KindFunc, v)
		if sym == nil {
			break
		}
		switch sym.Name {
		case "init":
			r.diags = append(r.diags, diag.New(diag.CodeDuplicateName, v.Name.Sp,
				"init is reserved for the constructor declared inside a struct body").
				WithHelp("rename the function, or move init into a struct body"))
		case "main":
			if r.name != "" && r.name != "main" {
				r.diags = append(r.diags, diag.New(diag.CodeMainPackage, v.Name.Sp,
					"main is the program entry point and belongs to package main").
					WithHelp("rename the function, or move it into package main"))
			}
		}
	case *ast.StructDecl:
		r.addType(r.declareTop(v.Name, KindType, v))
	case *ast.UnionDecl:
		r.addType(r.declareTop(v.Name, KindType, v))
	case *ast.EnumDecl:
		r.addType(r.declareTop(v.Name, KindType, v))
	case *ast.InterfaceDecl:
		r.addType(r.declareTop(v.Name, KindType, v))
	}
}

// addType records a type symbol for member collection.
func (r *resolver) addType(sym *Symbol) {
	if sym != nil {
		r.types = append(r.types, sym)
	}
}

// declareTop creates a package-level symbol and reports a collision with a
// predeclared name or another top-level declaration.
func (r *resolver) declareTop(id *ast.Ident, kind SymbolKind, decl ast.Node) *Symbol {
	if id == nil {
		return nil
	}
	if prev := r.universe.lookup(id.Name); prev != nil {
		r.diags = append(r.diags, diag.New(diag.CodePredeclaredName, id.Sp,
			"%q is a predeclared name and cannot be redeclared", id.Name).
			WithHelp("choose another name"))
	}
	sym := &Symbol{
		ID:     topID(r.path, id.Name),
		Name:   id.Name,
		Kind:   kind,
		Public: Public(id.Name),
		Decl:   decl,
	}
	if prev := r.pkgScope.insert(sym); prev != nil {
		r.duplicate(id, prev)
	}
	r.add(sym)
	r.info.Defs[id] = sym
	return sym
}

// collectMembers builds the member table of every type declaration.
func (r *resolver) collectMembers() {
	for _, s := range r.types {
		switch d := s.Decl.(type) {
		case *ast.StructDecl:
			for _, m := range d.Members {
				r.declareMember(s, m)
			}
		case *ast.EnumDecl:
			for _, m := range d.Members {
				r.declareMember(s, m)
			}
		case *ast.InterfaceDecl:
			for _, m := range d.Methods {
				r.declareMember(s, m)
			}
		case *ast.UnionDecl:
			for _, m := range d.Fields {
				r.declareMember(s, m)
			}
		}
	}
}

// declareMember records one member of a type body.
func (r *resolver) declareMember(owner *Symbol, m ast.Decl) {
	switch v := m.(type) {
	case *ast.FieldDecl:
		r.declareMemberName(owner, v.Name, KindField, v)
	case *ast.VariantDecl:
		r.declareMemberName(owner, v.Name, KindVariant, v)
	case *ast.ConstDecl:
		r.declareMemberName(owner, v.Name, KindConst, v)
	case *ast.FuncDecl:
		r.declareMethod(owner, v, false)
	case *ast.ImplBlock:
		for _, f := range v.Methods {
			r.declareMethod(owner, f, true)
		}
	}
}

// declareMethod records a method and applies the rules that reserve main and
// init. inImpl marks a method declared by an impl block, where init is not
// a constructor.
func (r *resolver) declareMethod(owner *Symbol, f *ast.FuncDecl, inImpl bool) {
	sym := r.declareMemberName(owner, f.Name, KindMethod, f)
	if sym == nil {
		return
	}
	switch sym.Name {
	case "main":
		r.diags = append(r.diags, diag.New(diag.CodeDuplicateName, f.Name.Sp,
			"main is the program entry point and cannot be a method").
			WithHelp("rename the method"))
	case "init":
		_, isStruct := owner.Decl.(*ast.StructDecl)
		if !isStruct || inImpl {
			r.diags = append(r.diags, diag.New(diag.CodeDuplicateName, f.Name.Sp,
				"init is only allowed as a direct member of a struct body").
				WithHelp("rename the method"))
		}
	}
}

// declareMemberName creates a member symbol. Fields, methods, variants, and
// constants of one type share a single name space.
func (r *resolver) declareMemberName(owner *Symbol, id *ast.Ident, kind SymbolKind, decl ast.Node) *Symbol {
	if id == nil {
		return nil
	}
	sym := &Symbol{
		ID:     owner.ID + "." + id.Name,
		Name:   id.Name,
		Kind:   kind,
		Public: Public(id.Name),
		Decl:   decl,
		Owner:  owner,
	}
	if prev := owner.insertMember(sym); prev != nil {
		d := diag.New(diag.CodeDuplicateName, id.Sp,
			"%s %q is already declared in %s", prev.Kind, id.Name, owner.Name)
		if prev.Decl != nil {
			d = d.WithNote(prev.Decl.Span(), "previous declaration")
		}
		r.diags = append(r.diags, d.WithHelp(
			"fields and methods of a type share one name space"))
	}
	r.add(sym)
	r.info.Defs[id] = sym
	return sym
}

// buildFileScopes binds the import qualifiers of every file.
func (r *resolver) buildFileScopes() {
	for _, f := range r.pkg.Files {
		fs := &fileScope{file: f, scope: newScope(r.pkgScope, scopeFile)}
		for _, imp := range f.Imports {
			if imp.CHeader {
				if fs.c == nil {
					fs.c = &Symbol{ID: "c", Name: "c", Kind: KindPackage, Public: true, Decl: imp}
					fs.scope.insert(fs.c)
				}
				fs.cDecls = append(fs.cDecls, imp)
				continue
			}
			itf, ok := r.lookupImport(imp)
			if !ok {
				continue
			}
			sym := &Symbol{
				ID:     imp.Path,
				Name:   itf.Name,
				Kind:   KindPackage,
				Public: true,
				Decl:   imp,
			}
			if prev := fs.scope.insert(sym); prev != nil {
				d := diag.New(diag.CodeImportDuplicate, imp.PathSp,
					"package name %s is already used by another import in this file", itf.Name)
				if prev.Decl != nil {
					d = d.WithNote(prev.Decl.Span(), "previous import")
				}
				r.diags = append(r.diags, d.WithHelp(
					"the language has no import aliases, so two imports cannot share a name"))
				continue
			}
			fs.imports = append(fs.imports, &importUse{decl: imp, sym: sym, itf: itf})
		}
		r.fileScopes = append(r.fileScopes, fs)
	}
}

// lookupImport returns the public surface of an imported package.
func (r *resolver) lookupImport(imp *ast.ImportDecl) (*Interface, bool) {
	if !isModulePath(imp.Path) {
		r.diags = append(r.diags, diag.New(diag.CodeImportRelative, imp.PathSp,
			"import path %q is not a full module path", imp.Path).
			WithHelp("write the module path and directory, such as std/net"))
		return nil, false
	}
	if r.imports == nil {
		r.reportImport(imp)
		return nil, false
	}
	itf, ok := r.imports.Lookup(imp.Path)
	if !ok || itf == nil || itf.Name == "" {
		r.reportImport(imp)
		return nil, false
	}
	return itf, true
}

// isModulePath reports whether an import path is a full module path. The
// language has no relative or absolute file imports.
func isModulePath(path string) bool {
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, ".") || strings.HasPrefix(path, "/") {
		return false
	}
	return !strings.ContainsAny(path, "\\:")
}

// reportImport reports an import path that no resolver could satisfy.
func (r *resolver) reportImport(imp *ast.ImportDecl) {
	r.diags = append(r.diags, diag.New(diag.CodeImportInvalid, imp.PathSp,
		"cannot resolve import %q", imp.Path).
		WithHelp("check the import path and the module that provides it"))
}

// resolveDecls resolves the signatures and bodies of every declaration.
func (r *resolver) resolveDecls() {
	for _, fs := range r.fileScopes {
		r.file = fs
		r.scope = fs.scope
		for _, d := range fs.file.Decls {
			r.resolveDecl(d)
		}
	}
	r.file = nil
	r.scope = nil
}

// resolveDecl resolves one top-level declaration.
func (r *resolver) resolveDecl(d ast.Decl) {
	switch v := d.(type) {
	case *ast.ConstDecl:
		r.withDecl(r.sym(v.Name), func() {
			r.resolveType(v.Type)
			r.resolveExpr(v.Value)
		})
	case *ast.VarDecl:
		r.withDecl(r.varOwner(v), func() {
			r.resolveType(v.Type)
			r.resolveExpr(v.Value)
		})
	case *ast.TypeAlias:
		r.withDecl(r.sym(v.Name), func() { r.resolveType(v.Type) })
	case *ast.FuncDecl:
		r.resolveFunc(v, r.sym(v.Name), nil)
	case *ast.StructDecl:
		r.resolveTypeDecl(r.sym(v.Name), v.TypeParams, v.Members)
	case *ast.EnumDecl:
		r.resolveTypeDecl(r.sym(v.Name), v.TypeParams, v.Members)
	case *ast.InterfaceDecl:
		r.resolveInterface(r.sym(v.Name), v)
	case *ast.UnionDecl:
		r.resolveUnion(r.sym(v.Name), v)
	}
}

// varOwner returns the symbol of the first name of a top-level var
// declaration, which owns the symbols of any lambda in its initializer.
func (r *resolver) varOwner(v *ast.VarDecl) *Symbol {
	for _, name := range v.Names {
		if sym := r.sym(name); sym != nil {
			return sym
		}
	}
	return nil
}

// withDecl runs fn in the declaration context of sym: symbol ids of locals
// and lambdas hang off it, and its visibility decides whether its signature
// may name private types.
func (r *resolver) withDecl(sym *Symbol, fn func()) {
	prevFunc, prevExported := r.funcSym, r.exported
	r.funcSym = sym
	r.exported = sym != nil && sym.Public
	fn()
	r.funcSym, r.exported = prevFunc, prevExported
}

// withMember narrows the exported context to one member: only a public
// member of a public type has to avoid private types.
func (r *resolver) withMember(id *ast.Ident, fn func()) {
	prev := r.exported
	r.exported = prev && (id == nil || Public(id.Name))
	fn()
	r.exported = prev
}

// resolveFunc resolves a function or method: type parameters, parameters,
// result, then the body.
func (r *resolver) resolveFunc(f *ast.FuncDecl, sym *Symbol, recv *ast.Ident) {
	if sym == nil {
		return
	}
	fs := newScope(r.scope, scopeFunc)
	prevScope, prevFunc, prevSelf := r.scope, r.funcSym, r.self
	prevN, prevLabels, prevExported := r.localN, r.labels, r.exported
	r.scope, r.funcSym, r.self, r.localN = fs, sym, nil, 0
	r.labels, r.exported = map[string]bool{}, sym.Public

	r.declareTypeParams(f.TypeParams, sym)
	if recv != nil {
		r.self = r.bindLocal(recv, KindParam)
	}
	for _, p := range f.Params {
		r.resolveType(p.Type)
		r.resolveExpr(p.Default)
	}
	for _, p := range f.Params {
		if p.Name != nil && p.Name.Name != "_" {
			r.bindLocalIn(p.Name, KindParam, !isTypeLevelParam(p))
		}
	}
	r.resolveType(f.Result)

	// A body is not a signature, so private types are free there.
	r.exported = false
	if f.Body != nil {
		r.resolveStmt(f.Body)
	}
	r.resolveExpr(f.BodyExpr)

	r.scope, r.funcSym, r.self, r.localN = prevScope, prevFunc, prevSelf, prevN
	r.labels, r.exported = prevLabels, prevExported
}

// declareTypeParams binds the generic parameters of a declaration.
func (r *resolver) declareTypeParams(params []*ast.TypeParam, decl *Symbol) {
	for _, tp := range params {
		if tp.Name == nil {
			continue
		}
		for _, c := range tp.Constraints {
			r.resolveType(c)
		}
		sym := &Symbol{
			ID:    typeParamID(decl, tp.Name.Name),
			Name:  tp.Name.Name,
			Kind:  KindTypeParam,
			Decl:  tp,
			Owner: decl,
		}
		if prev := r.scope.insert(sym); prev != nil {
			r.duplicate(tp.Name, prev)
		}
		r.add(sym)
		r.info.Defs[tp.Name] = sym
	}
}

// resolveTypeDecl resolves a struct or enum body: member signatures, then
// method bodies in the type's scope.
func (r *resolver) resolveTypeDecl(sym *Symbol, params []*ast.TypeParam, members []ast.Decl) {
	if sym == nil {
		return
	}
	ts := newScope(r.scope, scopeType)
	prevType, prevScope, prevExported := r.typeSym, r.scope, r.exported
	r.typeSym, r.scope, r.exported = sym, ts, sym.Public

	r.declareTypeParams(params, sym)
	for _, m := range members {
		switch v := m.(type) {
		case *ast.FieldDecl:
			r.withMember(v.Name, func() { r.resolveType(v.Type) })
		case *ast.VariantDecl:
			r.withMember(v.Name, func() {
				for _, t := range v.Payload {
					r.resolveType(t)
				}
			})
		case *ast.ConstDecl:
			r.withMember(v.Name, func() {
				r.resolveType(v.Type)
				r.resolveExpr(v.Value)
			})
		case *ast.FuncDecl:
			r.resolveFunc(v, r.sym(v.Name), receiverFor(v))
		case *ast.ImplBlock:
			r.resolveImpl(ts, v)
		}
	}

	r.typeSym, r.scope, r.exported = prevType, prevScope, prevExported
}

// isTypeLevelParam reports whether a parameter takes a type rather than a
// value, which the language writes with an uppercase name.
func isTypeLevelParam(p *ast.Param) bool {
	t, ok := p.Type.(*ast.TypeName)
	return ok && t.Qual == nil && t.Name != nil && t.Name.Name == "type"
}

// resolveImpl resolves an impl block: the type parameters it restates, the
// interface it names, and its methods. A restated parameter takes the
// identity of the declaration's parameter, because the two denote the same
// type.
func (r *resolver) resolveImpl(parent *scope, v *ast.ImplBlock) {
	bs := newScope(parent, scopeBlock)
	prev := r.scope
	r.scope = bs
	r.declareTypeParams(v.TypeParams, r.typeSym)
	r.resolveType(v.Interface)
	for _, f := range v.Methods {
		r.resolveFunc(f, r.sym(f.Name), selfIdent(f))
	}
	r.scope = prev
}

// resolveInterface resolves the signatures of an interface body.
func (r *resolver) resolveInterface(sym *Symbol, v *ast.InterfaceDecl) {
	if sym == nil {
		return
	}
	ts := newScope(r.scope, scopeType)
	prevType, prevScope, prevExported := r.typeSym, r.scope, r.exported
	r.typeSym, r.scope, r.exported = sym, ts, sym.Public

	r.declareTypeParams(v.TypeParams, sym)
	for _, m := range v.Methods {
		msym := r.sym(m.Name)
		r.exported = sym.Public && msym != nil && msym.Public
		for _, p := range m.Params {
			r.resolveType(p.Type)
		}
		r.resolveType(m.Result)
	}

	r.typeSym, r.scope, r.exported = prevType, prevScope, prevExported
}

// resolveUnion resolves the fields of a C union, which has no methods.
func (r *resolver) resolveUnion(sym *Symbol, v *ast.UnionDecl) {
	if sym == nil {
		return
	}
	ts := newScope(r.scope, scopeType)
	prevScope, prevExported := r.scope, r.exported
	r.scope, r.exported = ts, sym.Public
	for _, f := range v.Fields {
		r.withMember(f.Name, func() { r.resolveType(f.Type) })
	}
	r.scope, r.exported = prevScope, prevExported
}

// selfIdent returns the implicit receiver of a method. It carries the
// method's position so diagnostics about self point at the declaration.
func selfIdent(f *ast.FuncDecl) *ast.Ident {
	return &ast.Ident{Sp: f.Name.Sp, Name: "self"}
}

// receiverFor returns the implicit receiver of a method. init constructs a
// value rather than reading one, so it has no receiver.
func receiverFor(f *ast.FuncDecl) *ast.Ident {
	if f.Name != nil && f.Name.Name == "init" {
		return nil
	}
	return selfIdent(f)
}

// bindLocal creates a local, parameter, or receiver symbol. The symbol id
// counts declarations inside the enclosing function from one.
func (r *resolver) bindLocal(id *ast.Ident, kind SymbolKind) *Symbol {
	return r.bindLocalIn(id, kind, true)
}

// bindLocalIn creates a local symbol. checkName applies the capitalization
// rule for locals, which a pattern binding skips because its name may be a
// variant of the matched enum, and a variant is uppercase by convention.
func (r *resolver) bindLocalIn(id *ast.Ident, kind SymbolKind, checkName bool) *Symbol {
	if id == nil || id.Name == "_" {
		return nil
	}
	if prev := r.shadowed(id.Name); prev != nil {
		d := diag.New(diag.CodeShadowing, id.Sp,
			"%q shadows the %s declared in this package", id.Name, prev.Kind)
		if prev.Decl != nil {
			d = d.WithNote(prev.Decl.Span(), "%q is declared here", id.Name)
		}
		r.diags = append(r.diags, d.WithHelp("rename the binding"))
	}
	if checkName {
		r.checkLocalName(id)
	}
	r.localN++
	sym := &Symbol{
		ID:    localID(r.funcSym, r.localN),
		Name:  id.Name,
		Kind:  kind,
		Decl:  id,
		Owner: r.funcSym,
	}
	if prev := r.scope.insert(sym); prev != nil {
		r.duplicate(id, prev)
	}
	r.add(sym)
	r.info.Defs[id] = sym
	return sym
}

// shadowed returns the package-level declaration or predeclared name that a
// binding may not reuse, or nil.
func (r *resolver) shadowed(name string) *Symbol {
	if sym, ok := r.pkgScope.names[name]; ok {
		return sym
	}
	return r.universe.names[name]
}

// duplicate reports a second declaration of one name in one scope.
func (r *resolver) duplicate(id *ast.Ident, prev *Symbol) {
	d := diag.New(diag.CodeDuplicateName, id.Sp, "%q is already declared here", id.Name)
	if prev.Decl != nil {
		d = d.WithNote(prev.Decl.Span(), "previous declaration")
	}
	r.diags = append(r.diags, d.WithHelp("rename one of the two declarations"))
}

// checkUnusedImports reports imports that no declaration of their file
// names.
func (r *resolver) checkUnusedImports() {
	for _, fs := range r.fileScopes {
		for _, use := range fs.imports {
			if use.used {
				continue
			}
			r.diags = append(r.diags, diag.New(diag.CodeImportUnused, use.decl.PathSp,
				"%q is imported and never used", use.decl.Path).
				WithHelp("remove the import, or use a name it provides"))
		}
		if fs.c != nil && !fs.cUsed {
			for _, decl := range fs.cDecls {
				r.diags = append(r.diags, diag.New(diag.CodeImportUnused, decl.PathSp,
					"%q is imported and never used", decl.Path).
					WithHelp("remove the import, or use a name it provides"))
			}
		}
	}
}

// buildInterface collects the top-level names of the package. Private
// names are included so that a cross-package reference to one is reported
// as a visibility error.
func (r *resolver) buildInterface() {
	itf := &Interface{Path: r.path, Name: r.name, Decls: map[string]*Symbol{}}
	for name, sym := range r.pkgScope.names {
		itf.Decls[name] = sym
	}
	r.info.Interface = itf
}

// add records a symbol in creation order.
func (r *resolver) add(sym *Symbol) {
	if sym != nil {
		r.info.Symbols = append(r.info.Symbols, sym)
	}
}

// sym returns the symbol a declaration name was bound to.
func (r *resolver) sym(id *ast.Ident) *Symbol {
	if id == nil {
		return nil
	}
	return r.info.Defs[id]
}
