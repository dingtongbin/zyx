// Declaration checking: signatures, type members, and the rules that bind
// them.

package check

import (
	"strings"

	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/resolve"
	"zyx/internal/source"
	"zyx/internal/types"
)

// selfParam is the identity of the implementer parameter of a predeclared
// interface. An interface declared in source uses <declaration>$Self.
const selfParam = "Self"

// builtinDecls gives the members of the pre-imported types: the variants of
// the enums, the fields of PanicError, and the methods of the interfaces and
// handles. Those types have no source declaration, so their members live
// here and the checker reads them through declOf.
var builtinDecls = buildBuiltinDecls()

// buildBuiltinDecls builds the member table of the pre-imported types.
func buildBuiltinDecls() map[string]*Decl {
	optionT := predeclaredParams("Option")[0]
	resultParams := predeclaredParams("Result")
	resultT, resultE := resultParams[0], resultParams[1]
	handleR := types.Param("JoinHandle$R", "R")
	self := types.Param(selfParam, selfParam)
	resultOf := func(ok, err *types.Type) *types.Type {
		return types.Result.Instantiate(ok, err)
	}
	return map[string]*Decl{
		"Option": {
			Type:     types.Option,
			Params:   predeclaredParams("Option"),
			Variants: predeclaredVariants("Option"),
			Methods: map[string]*types.Type{
				"IsSome":   types.FuncOf(types.Bool),
				"IsNone":   types.FuncOf(types.Bool),
				"Unwrap":   types.FuncOf(optionT),
				"UnwrapOr": types.FuncOf(optionT, optionT),
			},
		},
		"Result": {
			Type:     types.Result,
			Params:   predeclaredParams("Result"),
			Variants: predeclaredVariants("Result"),
			Methods: map[string]*types.Type{
				"IsOk":     types.FuncOf(types.Bool),
				"IsErr":    types.FuncOf(types.Bool),
				"Unwrap":   types.FuncOf(resultT),
				"UnwrapOr": types.FuncOf(resultT, resultT),
				"Ok":       types.FuncOf(types.Option.Instantiate(resultT)),
				"Err":      types.FuncOf(types.Option.Instantiate(resultE)),
			},
		},
		"JoinHandle": {
			Type:   types.JoinHandle,
			Params: []*types.Type{handleR},
			Methods: map[string]*types.Type{
				"Join":    types.FuncOf(resultOf(handleR, types.PanicError)),
				"TryJoin": types.FuncOf(types.Option.Instantiate(resultOf(handleR, types.PanicError))),
			},
		},
		"Value": {
			Type:     types.Value,
			Variants: predeclaredVariants("Value"),
		},
		"PanicError": {
			Type:   types.PanicError,
			Fields: predeclaredFields("PanicError"),
		},
		"Eq": {
			Type:    types.Eq,
			Methods: map[string]*types.Type{"Eq": types.FuncOf(types.Bool, self)},
		},
		"Hash": {
			Type:    types.Hash,
			Methods: map[string]*types.Type{"Hash": types.FuncOf(types.Uint64)},
		},
		"Show": {
			Type: types.Show,
			// Show writes the text of a value into the buffer it is given,
			// so no value becomes a string on the way.
			Methods: map[string]*types.Type{"Show": types.FuncOf(nil, types.Buf)},
		},
		"SyncSafe": {
			Type:    types.SyncSafe,
			Methods: map[string]*types.Type{},
		},
	}
}

// predeclaredFields returns the fields of a predeclared struct in the
// checker's own form.
func predeclaredFields(name string) []*Field {
	list, _ := types.PredeclaredFields(name)
	out := make([]*Field, 0, len(list))
	for _, f := range list {
		out = append(out, &Field{Name: f.Name, Type: f.Type})
	}
	return out
}

// predeclaredParams returns the type parameters of a predeclared enum, which
// the types package owns so that the checker and the backend agree on them.
func predeclaredParams(name string) []*types.Type {
	params, _ := types.PredeclaredParams(name)
	return params
}

// predeclaredVariants returns the variants of a predeclared enum in the
// checker's own form.
func predeclaredVariants(name string) []*Variant {
	list, _ := types.PredeclaredVariants(name)
	out := make([]*Variant, 0, len(list))
	for _, v := range list {
		out = append(out, &Variant{Name: v.Name, Payload: v.Payload})
	}
	return out
}

// funcContext describes where a function declaration appears.
type funcContext struct {
	// method marks a method inside a type body.
	method bool
	// iface marks an interface method signature.
	iface bool
	// impl marks a method inside an impl block.
	impl bool
}

// checkDecls checks every top-level declaration.
func (c *checker) checkDecls() {
	for _, f := range c.pkg.Files {
		for _, d := range f.Decls {
			switch v := d.(type) {
			case *ast.TypeAlias:
				c.resolveAlias(v)
			case *ast.ConstDecl:
				c.checkConst(v)
			case *ast.VarDecl:
				c.checkVar(v)
			case *ast.FuncDecl:
				c.checkFreeFunc(v)
			case *ast.StructDecl:
				c.checkStruct(v)
			case *ast.UnionDecl:
				c.checkUnion(v)
			case *ast.EnumDecl:
				c.checkEnum(v)
			case *ast.InterfaceDecl:
				c.checkInterface(v)
			}
		}
	}
}

// checkConst checks a top-level constant. Its initializer is evaluated by
// the comptime pass.
func (c *checker) checkConst(v *ast.ConstDecl) {
	t := c.buildType(v.Type)
	if t == nil || v.Name == nil {
		return
	}
	c.out.Decls[v.Name.Name] = t
	c.recordDecl(v.Name, t)
}

// checkVar checks a top-level variable. Its type must be able to live in
// static storage, and its initializer is evaluated by the comptime pass.
func (c *checker) checkVar(v *ast.VarDecl) {
	t := c.buildType(v.Type)
	if t == nil {
		return
	}
	if !staticStorageOK(t) {
		c.errorHelp(diag.CodeTopLevelVar, typeSpan(v.Type, v.Sp),
			"use a value type, str, a fixed-size array of those, or a sync primitive",
			"a top-level var cannot hold %s", t)
	}
	for _, n := range v.Names {
		c.out.Decls[n.Name] = t
		c.recordDecl(n, t)
	}
}

// staticStorageOK reports whether a type may live in the static storage a
// top-level var occupies: a value type, str, a fixed-size array of those,
// or one of the four sync primitives.
func staticStorageOK(t *types.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case types.KindStr, types.KindExternStruct, types.KindUnion:
		return true
	case types.KindArray:
		return staticStorageOK(t.Elem)
	case types.KindStruct:
		return isSyncPrimitive(t)
	}
	return t.IsValue()
}

// isSyncPrimitive reports whether a named type is one of the sync types the
// language allows in static storage.
func isSyncPrimitive(t *types.Type) bool {
	if t.Pkg != "sync" && !strings.HasSuffix(t.Pkg, "/sync") {
		return false
	}
	switch t.Name {
	case "Atomic", "Mutex", "Once", "WaitGroup":
		return true
	}
	return false
}

// checkFreeFunc checks a top-level function.
func (c *checker) checkFreeFunc(v *ast.FuncDecl) {
	for _, tp := range v.TypeParams {
		for _, con := range tp.Constraints {
			c.constraintType(con)
		}
	}
	c.withParams(v.TypeParams, func() {
		t := c.funcType(v)
		if sym := c.symOf(v.Name); sym != nil {
			c.bySym[sym.ID] = t
		}
		if v.Name != nil && t != nil {
			c.out.Decls[v.Name.Name] = t
			c.recordDecl(v.Name, t)
		}
		c.checkFuncShape(v, funcContext{})
	})
}

// funcType returns the function type of a declaration. A variadic parameter
// is a C-level extension, so it does not appear in the type.
func (c *checker) funcType(v *ast.FuncDecl) *types.Type {
	params := make([]*types.Type, 0, len(v.Params))
	for _, p := range v.Params {
		if p.Variadic {
			continue
		}
		params = append(params, c.buildType(p.Type))
	}
	return types.FuncOf(c.buildType(v.Result), params...)
}

// checkFuncShape checks the shape of a function declaration: body presence,
// the variadic parameter, default values, and the reserved names.
func (c *checker) checkFuncShape(v *ast.FuncDecl, ctx funcContext) {
	hasBody := v.Body != nil || v.BodyExpr != nil
	if v.Quals.Fast && !v.Quals.Extern {
		// Whether a C function blocks is a property of the function, so the
		// declaration that links it is where fast belongs. A function the
		// program defines is not a C symbol at all.
		c.errorHelp(diag.CodeUnexpectedToken, nameSpan(v),
			"mark the function extern, or drop fast",
			"fast states the blocking behavior of a C symbol, so it needs extern")
	}
	switch {
	case v.Quals.Extern:
		if hasBody {
			c.errorHelp(diag.CodeExternBody, nameSpan(v),
				"remove the body, or drop the extern qualifier",
				"an extern function declares a symbol and has no body")
		}
	case ctx.iface:
		if hasBody {
			c.errorHelp(diag.CodeInterfaceBody, nameSpan(v),
				"remove the body; an interface declares signatures only",
				"an interface method has no body")
		}
	default:
		if !hasBody && v.Name != nil {
			c.errorHelp(diag.CodeMissingBody, nameSpan(v),
				"write the body, or mark the function extern",
				"function %q has no body", v.Name.Name)
		}
	}
	c.checkVariadic(v)
	c.checkDefaults(v, ctx)
	if ctx.method && len(v.TypeParams) > 0 {
		c.errorHelp(diag.CodeMethodTypeParams, nameSpan(v),
			"move the type parameters to the type declaration",
			"a method cannot declare type parameters")
	}
	if !ctx.method && v.Name != nil && v.Name.Name == "main" {
		c.checkMainShape(v)
	}
}

// checkVariadic checks that only an extern function ends with `...`.
func (c *checker) checkVariadic(v *ast.FuncDecl) {
	for i, p := range v.Params {
		if !p.Variadic {
			continue
		}
		if !v.Quals.Extern {
			c.errorHelp(diag.CodeVariadic, p.Sp,
				"drop the variadic parameter, or mark the function extern",
				"only an extern function may end with ...")
		}
		if i != len(v.Params)-1 {
			c.errorHelp(diag.CodeVariadic, p.Sp,
				"move ... to the end of the parameter list",
				"the variadic parameter must be last")
		}
	}
}

// checkDefaults checks the trailing rule for default values and the ban on
// them in interface methods and their implementations.
func (c *checker) checkDefaults(v *ast.FuncDecl, ctx funcContext) {
	seen := false
	for _, p := range v.Params {
		if p.Default != nil {
			if ctx.iface || ctx.impl {
				c.errorHelp(diag.CodeDefaultParam, identSpan(p.Name, p.Sp),
					"remove the default value from the signature and its implementation",
					"an interface method and its implementation take no default values")
			}
			seen = true
			continue
		}
		if seen {
			c.errorHelp(diag.CodeDefaultParam, identSpan(p.Name, p.Sp),
				"give this parameter a default too, or move it before the defaulted ones",
				"every parameter after a default value needs one")
		}
	}
}

// checkMainShape checks the fixed shape of the program entry point.
func (c *checker) checkMainShape(v *ast.FuncDecl) {
	if len(v.Params) > 0 {
		c.errorHelp(diag.CodeMainSignature, nameSpan(v),
			"drop the parameters", "main takes no parameters")
	}
	if len(v.TypeParams) > 0 {
		c.errorHelp(diag.CodeMainSignature, nameSpan(v),
			"drop the type parameters", "main cannot be generic")
	}
	if v.Quals.Comptime || v.Quals.Extern || v.Quals.CAbi {
		c.errorHelp(diag.CodeMainSignature, nameSpan(v),
			"drop the qualifier", "main takes no qualifier")
	}
	if v.BodyExpr != nil {
		c.errorHelp(diag.CodeMainSignature, nameSpan(v),
			"write main with a block body", "main cannot use an expression body")
	}
	if v.Result != nil {
		if t := c.buildType(v.Result); t != nil && !types.Identical(t, types.Int32) {
			c.errorHelp(diag.CodeMainSignature, v.Result.Span(),
				"return i32, or declare no result", "main returns i32, not %s", t)
		}
	}
}

// checkStruct checks a struct body: fields, methods, impl blocks, and the
// constant metadata.
func (c *checker) checkStruct(v *ast.StructDecl) {
	d := c.declOfDecl(v.Name)
	if d == nil {
		return
	}
	defer c.withSelf(d.Type)()
	c.withParams(v.TypeParams, func() {
		for _, m := range v.Members {
			switch x := m.(type) {
			case *ast.FieldDecl:
				t := c.buildType(x.Type)
				c.addField(d, x, t)
				if v.Extern {
					c.checkABIField(x, t)
				}
			case *ast.ConstDecl:
				c.checkMemberConst(d, x)
			case *ast.FuncDecl:
				c.checkMethod(d, x)
			case *ast.ImplBlock:
				c.checkImplBlock(d, x)
			}
		}
		c.checkRecursion(d, v.Name)
		if v.Extern && len(d.Fields) == 0 {
			c.errorHelp(diag.CodeEmptyCLayout, v.Name.Sp,
				"declare at least one field",
				"an empty extern struct has no C layout")
		}
	})
}

// checkUnion checks the fields of a C union, which has no other members.
func (c *checker) checkUnion(v *ast.UnionDecl) {
	d := c.declOfDecl(v.Name)
	if d == nil {
		return
	}
	for _, f := range v.Fields {
		t := c.buildType(f.Type)
		c.addField(d, f, t)
		c.checkABIField(f, t)
	}
	c.checkRecursion(d, v.Name)
	if len(d.Fields) == 0 {
		c.errorHelp(diag.CodeEmptyCLayout, v.Name.Sp,
			"declare at least one member",
			"an empty extern union has no C layout")
	}
}

// checkEnum checks an enum body: variants, methods, impl blocks, and the
// constant metadata.
func (c *checker) checkEnum(v *ast.EnumDecl) {
	d := c.declOfDecl(v.Name)
	if d == nil {
		return
	}
	defer c.withSelf(d.Type)()
	c.withParams(v.TypeParams, func() {
		for _, m := range v.Members {
			switch x := m.(type) {
			case *ast.VariantDecl:
				if x.Name == nil {
					continue
				}
				payload := make([]*types.Type, 0, len(x.Payload))
				for _, p := range x.Payload {
					payload = append(payload, c.buildType(p))
				}
				d.Variants = append(d.Variants, &Variant{
					Sym:     c.symOf(x.Name),
					Name:    x.Name.Name,
					Payload: payload,
				})
				d.variant[x.Name.Name] = d.Variants[len(d.Variants)-1]
				c.recordDecl(x.Name, d.Type)
			case *ast.ConstDecl:
				c.checkMemberConst(d, x)
			case *ast.FuncDecl:
				c.checkMethod(d, x)
			case *ast.ImplBlock:
				c.checkImplBlock(d, x)
			}
		}
		c.checkRecursion(d, v.Name)
	})
}

// checkInterface checks the method signatures of an interface. Self names
// the implementer, which the interface cannot know, so it stays a
// parameter until an impl block applies it.
func (c *checker) checkInterface(v *ast.InterfaceDecl) {
	sym := c.symOf(v.Name)
	d := c.declOfDecl(v.Name)
	if d == nil {
		return
	}
	defer c.withSelf(types.Param(sym.ID+"$Self", selfParam))()
	c.withParams(v.TypeParams, func() {
		for _, m := range v.Methods {
			t := c.funcType(m)
			if m.Name != nil {
				d.Methods[m.Name.Name] = t
				c.recordDecl(m.Name, t)
			}
			c.checkFuncShape(m, funcContext{method: true, iface: true})
		}
	})
}

// withSelf sets the type Self names and returns a function that restores
// the previous one.
func (c *checker) withSelf(t *types.Type) func() {
	prev := c.self
	c.self = t
	return func() { c.self = prev }
}

// declOfDecl returns the checked declaration of a type name.
func (c *checker) declOfDecl(id *ast.Ident) *Decl {
	sym := c.symOf(id)
	if sym == nil {
		return nil
	}
	return c.out.Declared[sym.ID]
}

// addField records one field of a struct or union.
func (c *checker) addField(d *Decl, f *ast.FieldDecl, t *types.Type) {
	if f.Name == nil {
		return
	}
	field := &Field{Sym: c.symOf(f.Name), Name: f.Name.Name, Type: t}
	d.Fields = append(d.Fields, field)
	d.field[field.Name] = field
	c.recordDecl(f.Name, t)
}

// checkMemberConst records one constant declared inside a type body.
func (c *checker) checkMemberConst(d *Decl, v *ast.ConstDecl) {
	if v.Name == nil {
		return
	}
	t := c.buildType(v.Type)
	if t == nil {
		return
	}
	d.Consts[v.Name.Name] = &Const{Sym: c.symOf(v.Name), Type: t}
	c.recordDecl(v.Name, t)
}

// checkMethod records one method of a type body.
func (c *checker) checkMethod(d *Decl, f *ast.FuncDecl) {
	ctx := funcContext{method: true}
	t := c.funcType(f)
	if f.Name != nil {
		d.Methods[f.Name.Name] = t
		c.recordDecl(f.Name, t)
		if f.Name.Name == "init" {
			c.checkInitShape(f)
		}
	}
	c.checkFuncShape(f, ctx)
}

// checkInitShape checks that init returns Self, the only way to build a
// value through new.
func (c *checker) checkInitShape(f *ast.FuncDecl) {
	if f.Result == nil {
		c.errorHelp(diag.CodeInitSignature, nameSpan(f),
			"declare the result as Self", "init must return Self")
		return
	}
	if t := c.buildType(f.Result); t != nil && !types.Identical(t, c.self) {
		c.errorHelp(diag.CodeInitSignature, f.Result.Span(),
			"declare the result as Self", "init returns Self, not %s", t)
	}
}

// checkImplBlock checks one impl block: the interface it names, the
// constraints it restates, and its methods.
func (c *checker) checkImplBlock(d *Decl, v *ast.ImplBlock) {
	c.withParams(v.TypeParams, func() {
		iface := c.buildType(v.Interface)
		if iface == nil {
			return
		}
		if iface.Kind != types.KindInterface {
			c.errorHelp(diag.CodeNotInterface, v.Interface.Span(),
				"implement an interface",
				"%s is not an interface", iface)
			return
		}
		if d.impl[iface.ID] {
			c.errorHelp(diag.CodeDuplicateImpl, v.Interface.Span(),
				"merge the two blocks, or implement another interface",
				"%s already implements %s", d.Type.Name, iface)
			return
		}
		d.impl[iface.ID] = true
		c.checkImplConstraints(d, v)

		impl := &Impl{
			Interface:  iface,
			Sp:         v.Sp,
			Methods:    map[string]*types.Type{},
			MethodSyms: map[string]*resolve.Symbol{},
		}
		for _, f := range v.Methods {
			t := c.funcType(f)
			if f.Name != nil {
				impl.Methods[f.Name.Name] = t
				impl.MethodSyms[f.Name.Name] = c.symOf(f.Name)
				d.Methods[f.Name.Name] = t
				c.recordDecl(f.Name, t)
			}
			c.checkFuncShape(f, funcContext{method: true, impl: true})
		}
		d.Impls = append(d.Impls, impl)
	})
}

// checkImplConstraints verifies that an impl block that restates the type
// parameters keeps every constraint of the declaration.
func (c *checker) checkImplConstraints(d *Decl, v *ast.ImplBlock) {
	if len(v.TypeParams) == 0 {
		return
	}
	if len(v.TypeParams) != len(d.Params) {
		c.errorHelp(diag.CodeConstraintWeakened, v.Sp,
			"restate every type parameter of the declaration",
			"%s declares %d type parameters", d.Type.Name, len(d.Params))
		return
	}
	for i, tp := range v.TypeParams {
		if tp.Name == nil || tp.Name.Name != d.Params[i].Name {
			c.errorHelp(diag.CodeConstraintWeakened, identSpan(tp.Name, tp.Sp),
				"restate the parameters in the declared order",
				"restate the type parameters as %s", d.Params[i].Name)
			continue
		}
		got := make([]*types.Type, 0, len(tp.Constraints))
		for _, con := range tp.Constraints {
			got = append(got, c.buildType(con))
		}
		for _, want := range d.ParamConstraints[tp.Name.Name] {
			if !containsType(got, want) {
				c.errorHelp(diag.CodeConstraintWeakened, tp.Sp,
					"repeat the constraint the declaration requires",
					"the impl block drops the %s constraint", want)
			}
		}
	}
}

// checkImpls verifies that every impl block implements its interface
// completely and adds nothing beyond it.
func (c *checker) checkImpls() {
	for _, d := range c.declOrder {
		for _, impl := range d.Impls {
			c.checkImplComplete(d, impl)
		}
	}
}

// checkImplComplete compares one impl block with the interface it names.
func (c *checker) checkImplComplete(d *Decl, impl *Impl) {
	want := c.out.interfaceMethods(impl.Interface, d.Type)
	for name, sig := range want {
		got, ok := impl.Methods[name]
		if !ok {
			c.errorHelp(diag.CodeMissingMethod, impl.Sp,
				"implement the method",
				"%s does not implement %s.%s", d.Type.Name, impl.Interface, name)
			continue
		}
		if !types.Identical(got, sig) {
			c.errorHelp(diag.CodeTypeMismatch, symSpan(impl.MethodSyms[name], impl.Sp),
				"match the interface signature",
				"%s has signature %s, want %s", name, got, sig)
		}
	}
	for name, sym := range impl.MethodSyms {
		if _, ok := want[name]; !ok {
			c.errorHelp(diag.CodeExtraMethod, symSpan(sym, impl.Sp),
				"remove the method, or declare it in the interface",
				"%s is not part of %s", name, impl.Interface)
		}
	}
}

// interfaceMethods returns the methods an interface requires, with its type
// arguments and its Self parameter applied to the implementing type.
func (p *Package) interfaceMethods(iface, implType *types.Type) map[string]*types.Type {
	raw := p.rawInterfaceMethods(iface)
	if raw == nil {
		return nil
	}
	args := map[string]*types.Type{}
	if d := p.Decl(iface); d != nil {
		for i, p := range d.Params {
			if i < len(iface.Args) {
				args[p.ID] = iface.Args[i]
			}
		}
	}
	out := make(map[string]*types.Type, len(raw))
	for name, sig := range raw {
		bindings := map[string]*types.Type{}
		for k, v := range args {
			bindings[k] = v
		}
		for _, id := range types.ParamIDs(sig) {
			if id == selfParam || strings.HasSuffix(id, "$Self") {
				bindings[id] = implType
			}
		}
		out[name] = types.Substitute(sig, bindings)
	}
	return out
}

// rawInterfaceMethods returns the declared methods of an interface, before
// its type arguments and Self are applied.
func (p *Package) rawInterfaceMethods(iface *types.Type) map[string]*types.Type {
	if iface == nil {
		return nil
	}
	if d := p.Decl(iface); d != nil {
		return d.Methods
	}
	return nil
}

// declOf returns the checked declaration of a named type, whether it is
// declared here, imported, or pre-imported. It is the same lookup the
// package exposes, so the two cannot drift apart.
func (c *checker) declOf(t *types.Type) *Decl {
	return c.out.Decl(t)
}

// checkABIField reports a field type that cannot appear in C layout.
func (c *checker) checkABIField(f *ast.FieldDecl, t *types.Type) {
	if abiCompatible(t) {
		return
	}
	c.errorHelp(diag.CodeFieldNotABI, typeSpan(f.Type, f.Sp),
		"use a numeric type, bool, char, raw, an extern struct or union, a fixed-size array, or a function type",
		"%s is not allowed in C layout", t)
}

// abiCompatible reports whether a type may appear in a C layout: the
// numeric types, bool, char, raw, function types, the C value types, and
// fixed-size arrays of those.
func abiCompatible(t *types.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case types.KindBool, types.KindChar, types.KindRaw,
		types.KindExternStruct, types.KindUnion:
		return true
	case types.KindFunc:
		// A function value carries the function and the receiver a method
		// value was taken from, so it is not a C function pointer. A C
		// callback field is written from the header's own type once the
		// header symbol table lands.
		return false
	case types.KindArray:
		return abiCompatible(t.Elem)
	}
	return t.IsNumeric()
}

// checkRecursion reports a field or variant payload that contains its own
// type through a tuple or a fixed-size array.
func (c *checker) checkRecursion(d *Decl, id *ast.Ident) {
	for _, f := range d.Fields {
		if containsDirect(f.Type, d.Type) {
			c.errorHelp(diag.CodeFieldRecursion, symSpan(f.Sym, identSpan(id, source.Span{})),
				"hold the value behind Array, Map, Set, or chan, or use an index",
				"field %q contains %s directly", f.Name, d.Type.Name)
		}
	}
	for _, v := range d.Variants {
		for _, p := range v.Payload {
			if containsDirect(p, d.Type) {
				c.errorHelp(diag.CodeFieldRecursion, symSpan(v.Sym, identSpan(id, source.Span{})),
					"hold the payload behind Array, Map, Set, or chan",
					"variant %q contains %s directly", v.Name, d.Type.Name)
			}
		}
	}
}

// containsDirect reports whether a type contains target through a tuple or
// a fixed-size array, which would make the enclosing type infinitely sized.
// The reference containers break the chain.
func containsDirect(t, target *types.Type) bool {
	if t == nil {
		return false
	}
	if types.Identical(t, target) {
		return true
	}
	switch t.Kind {
	case types.KindTuple:
		for _, e := range t.Elems {
			if containsDirect(e, target) {
				return true
			}
		}
	case types.KindArray:
		return containsDirect(t.Elem, target)
	}
	return false
}

// containsType reports whether a type list holds an identical type.
func containsType(list []*types.Type, t *types.Type) bool {
	for _, x := range list {
		if types.Identical(x, t) {
			return true
		}
	}
	return false
}

// nameSpan returns the span of a function name, or the declaration.
func nameSpan(v *ast.FuncDecl) source.Span {
	if v.Name != nil {
		return v.Name.Sp
	}
	return v.Sp
}

// identSpan returns the span of an identifier, or a fallback.
func identSpan(id *ast.Ident, fallback source.Span) source.Span {
	if id != nil {
		return id.Sp
	}
	return fallback
}

// symSpan returns the span of a symbol's declaration, or a fallback.
func symSpan(sym *resolve.Symbol, fallback source.Span) source.Span {
	if sym != nil && sym.Decl != nil {
		return sym.Decl.Span()
	}
	return fallback
}

// typeSpan returns the span of a type expression, or a fallback.
func typeSpan(t ast.TypeExpr, fallback source.Span) source.Span {
	if t != nil {
		return t.Span()
	}
	return fallback
}
