// Call resolution, method lookup, and generic inference.

package check

import (
	"sort"
	"strconv"
	"strings"

	"zyx/internal/ast"
	"zyx/internal/cheader"
	"zyx/internal/diag"
	"zyx/internal/resolve"
	"zyx/internal/source"
	"zyx/internal/token"
	"zyx/internal/types"
)

// checkCall checks a call and returns its result type.
func (c *checker) checkCall(v *ast.CallExpr, want *types.Type) *types.Type {
	// A call to a compile-time function must be evaluated at compile time.
	if _, sym := c.comptimeCallee(v.Fn); sym != nil && !c.inComptime {
		c.errorHelp(diag.CodeComptimeOnly, v.Sp,
			"call it from a comptime context",
			"%s is a compile-time function", sym.Name)
	}
	// A callee that names a type is an explicit conversion.
	if t := c.calleeType(v.Fn); t != nil {
		return c.checkConvert(t, firstArg(v), v.Sp)
	}
	if sel, ok := v.Fn.(*ast.SelectorExpr); ok {
		return c.checkSelectorCall(sel, v, want)
	}
	return c.checkValueCall(v, want)
}

// calleeType returns the type a callee names when the call is a conversion,
// or nil when the callee is an ordinary function or method.
func (c *checker) calleeType(fn ast.Expr) *types.Type {
	switch v := fn.(type) {
	case *ast.Ident:
		if !isNamespaceName(v.Name) && IsTypeSymbol(c.symOf(v)) {
			return c.buildType(typeExprOf(v))
		}
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok && !isNamespaceName(id.Name) && IsTypeSymbol(c.symOf(id)) {
			return c.buildType(typeExprOf(v))
		}
	}
	return nil
}

// isNamespaceName reports whether a name is one of the two comptime
// namespaces, which are predeclared but do not name types.
func isNamespaceName(name string) bool { return name == "meta" || name == "dur" }

// firstArg returns the first argument of a conversion.
func firstArg(v *ast.CallExpr) ast.Expr {
	if len(v.Args) == 0 {
		return nil
	}
	return v.Args[0]
}

// checkValueCall checks a call through a function value.
func (c *checker) checkValueCall(v *ast.CallExpr, want *types.Type) *types.Type {
	sig := c.checkExpr(v.Fn, nil)
	if sig == nil || sig.Kind == types.Invalid {
		return nil
	}
	if sig.Kind != types.KindFunc {
		c.errorHelp(diag.CodeNotCallable, v.Fn.Span(),
			"call a function or method",
			"%s is not callable", sig)
		return nil
	}
	return c.withAddrOK(v.Fn, func() *types.Type {
		min := len(sig.Elems)
		if id, ok := v.Fn.(*ast.Ident); ok {
			min = c.requiredArgs(id)
		}
		return c.checkArgs(sig, v.Args, callArgs{fn: v.Fn, sp: v.Fn.Span(), min: min, want: want})
	})
}

// withAddrOK runs run with the address-of operator allowed when the callee
// crosses the C boundary.
func (c *checker) withAddrOK(fn ast.Expr, run func() *types.Type) *types.Type {
	prev := c.addrOK
	c.addrOK = prev || c.cCall(fn)
	defer func() { c.addrOK = prev }()
	return run()
}

// cCall reports whether a callee takes its arguments across the C boundary,
// where `&x` is allowed.
func (c *checker) cCall(fn ast.Expr) bool {
	var sym *resolve.Symbol
	switch v := fn.(type) {
	case *ast.Ident:
		sym = c.symOf(v)
	case *ast.SelectorExpr:
		sym = c.symOf(v.Name)
	}
	if sym == nil {
		return false
	}
	f, ok := sym.Decl.(*ast.FuncDecl)
	return ok && (f.Quals.Extern || f.Quals.CAbi)
}

// checkSelectorCall checks `recv.Name(args)`: a qualified call, a variant
// construction, or a method call.
func (c *checker) checkSelectorCall(sel *ast.SelectorExpr, v *ast.CallExpr, want *types.Type) *types.Type {
	if id, ok := sel.X.(*ast.Ident); ok {
		if isNamespaceName(id.Name) {
			return c.checkNamespaceCall(sel, v, id.Name)
		}
		switch sym := c.symOf(id); {
		case sym != nil && sym.Kind == resolve.KindPackage:
			return c.checkQualifiedCall(sel, v, sym)
		case IsTypeSymbol(sym):
			return c.checkVariantCall(sel, v, sym, want)
		}
	}
	recv := c.checkExpr(sel.X, nil)
	if recv == nil || recv.Kind == types.Invalid {
		return nil
	}
	if sel.Name.Name == "clone" {
		return c.checkClone(recv, sel.Name.Sp)
	}
	sig := c.methodOf(recv, sel.Name.Name)
	if sig == nil {
		c.errorHelp(diag.CodeNoMethod, sel.Name.Sp,
			"check the method name, or the type of the receiver",
			"%s has no method %q", recv, sel.Name.Name)
		return nil
	}
	c.out.Nodes[sel.Name] = sig
	return c.withAddrOK(sel, func() *types.Type {
		return c.checkArgs(sig, v.Args, callArgs{
			fn:   sel.Name,
			sp:   sel.Name.Sp,
			min:  c.requiredArgs(sel.Name),
			want: want,
			recv: recv,
		})
	})
}

// checkClone checks the built-in deep copy, which only a type with
// reference members needs.
func (c *checker) checkClone(recv *types.Type, sp source.Span) *types.Type {
	if !c.hasReferenceMember(recv) {
		c.errorHelp(diag.CodeClone, sp,
			"copy a value type by assignment; it already copies",
			"%s has no reference member to copy", recv)
	}
	return recv
}

// checkQualifiedCall checks `pkg.Fn(args)` and `c.fn(args)`.
func (c *checker) checkQualifiedCall(sel *ast.SelectorExpr, v *ast.CallExpr, pkg *resolve.Symbol) *types.Type {
	if pkg.Name == "c" {
		return c.checkCCall(sel, v)
	}
	member := c.symOf(sel.Name)
	if member == nil {
		return nil
	}
	sig := c.typeOfSymbol(member)
	if sig == nil {
		// A declaration of an imported package carries its type in that
		// package's interface, not in this package's symbol table.
		sig = c.importedType(pkg, sel.Name.Name)
	}
	if sig == nil || sig.Kind != types.KindFunc {
		c.errorHelp(diag.CodeNotCallable, sel.Name.Sp,
			"call a function",
			"%s.%s is not callable", pkg.Name, sel.Name.Name)
		return nil
	}
	c.out.Nodes[sel.Name] = sig
	return c.checkArgs(sig, v.Args, callArgs{fn: sel.Name, sp: sel.Name.Sp, min: c.requiredArgs(sel.Name)})
}

// checkCCall checks a call into the C namespace. The signature comes from the
// header the package imported, so the arguments are checked against the
// declaration and the call answers with the declaration's result.
func (c *checker) checkCCall(sel *ast.SelectorExpr, v *ast.CallExpr) *types.Type {
	name := sel.Name.Name
	fn, ok := c.cFunction(name)
	if !ok {
		c.errorHelp(diag.CodeUndefinedName, sel.Name.Sp,
			"import the header that declares it",
			"%s is not declared by an imported C header", name)
		return nil
	}
	// An argument may be the address of a value, which only a C call takes.
	prev := c.addrOK
	c.addrOK = true
	defer func() { c.addrOK = prev }()

	// A str and a view cross as the pointer and the length they are, so one
	// argument can fill two parameters.
	at := 0
	for _, a := range v.Args {
		var want *types.Type
		if at < len(fn.Params) {
			want = fn.Params[at]
		}
		t := c.checkExpr(a, want)
		if t == nil || t.Kind == types.Invalid {
			return nil
		}
		width := 1
		if t.Kind == types.KindStr || t.Kind == types.KindSlice {
			width = 2
		}
		if at+width > len(fn.Params) {
			if !fn.Variadic {
				c.errorHelp(diag.CodeArgCount, a.Span(),
					"pass the declared number of arguments",
					"%s takes %d arguments", name, len(fn.Params))
				return nil
			}
			at += width
			continue
		}
		if !cArgFits(t, fn.Params[at:at+width]) {
			c.errorHelp(diag.CodeTypeMismatch, a.Span(),
				"pass a value of the declared type",
				"%s takes %s here, not %s", name, cArgText(fn.Params[at:at+width]), t)
			return nil
		}
		at += width
	}
	if at != len(fn.Params) && !fn.Variadic {
		c.errorHelp(diag.CodeArgCount, sel.Name.Sp,
			"pass the declared number of arguments",
			"%s takes %d arguments, %d given", name, len(fn.Params), at)
		return nil
	}
	if fn.Result == nil {
		return void
	}
	return fn.Result
}

// cFunction returns the declaration of a C function, searching the headers
// the package imported.
func (c *checker) cFunction(name string) (*cheader.Func, bool) {
	for _, header := range sortedKeys(c.cdecls) {
		if fn, ok := c.cdecls[header].Functions[name]; ok {
			return fn, true
		}
	}
	return nil, false
}

// cArgFits reports whether a language value fits the C parameters its
// position fills.
//
// The C boundary is a boundary of representations: a bool is one byte and a
// char is four, and an integer of the same width crosses as the parameter
// whether or not the signedness matches, because the bytes are the same. A
// str and a view fill the pointer and the length they are.
func cArgFits(t *types.Type, params []*types.Type) bool {
	switch len(params) {
	case 2:
		return types.Identical(params[0], types.Raw) && params[1].IsInteger()
	case 1:
	default:
		return false
	}
	p := params[0]
	if types.Identical(t, p) {
		return true
	}
	if t.IsNumeric() && p.IsNumeric() {
		return cWidth(t) == cWidth(p)
	}
	switch t.Kind {
	case types.KindBool:
		return cWidth(p) == 1
	case types.KindChar:
		return cWidth(p) == 4
	}
	return false
}

// cWidth returns the byte width of an integer type, and zero for a type that
// is not one.
func cWidth(t *types.Type) int {
	switch t.Kind {
	case types.KindInt8, types.KindUint8:
		return 1
	case types.KindInt16, types.KindUint16:
		return 2
	case types.KindInt32, types.KindUint32:
		return 4
	case types.KindInt64, types.KindUint64, types.KindUsize:
		return 8
	case types.KindInt128, types.KindUint128:
		return 16
	}
	return 0
}

// cArgText renders the parameters one argument fills.
func cArgText(params []*types.Type) string {
	parts := make([]string, 0, len(params))
	for _, pt := range params {
		parts = append(parts, pt.String())
	}
	return strings.Join(parts, ", ")
}

// sortedKeys returns the keys of a table in byte order, so a search over
// several headers does not depend on the order they were read in.
func sortedKeys(t map[string]*cheader.Table) []string {
	out := make([]string, 0, len(t))
	for key := range t {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// checkVariantCall checks `Enum.Variant(args)`. The enum's type arguments
// come from the payload arguments and, for the parameters those leave
// unbound, from the expected type.
func (c *checker) checkVariantCall(sel *ast.SelectorExpr, v *ast.CallExpr, sym *resolve.Symbol, want *types.Type) *types.Type {
	enum := c.typeOfSymbol(sym)
	d := c.declOf(enum)
	if enum == nil || enum.Kind != types.KindEnum || d == nil {
		c.errorHelp(diag.CodeEnumLiteral, sel.Name.Sp,
			"build an enum value through its variants",
			"%s is not an enum", enum)
		return nil
	}
	variant := d.Variant(sel.Name.Name)
	if variant == nil {
		c.errorHelp(diag.CodeNoMethod, sel.Name.Sp,
			"check the variant name",
			"%s has no variant %q", enum.Name, sel.Name.Name)
		return nil
	}
	c.out.Nodes[sel.Name] = enum

	argTypes := make([]*types.Type, len(v.Args))
	for i, a := range v.Args {
		argTypes[i] = c.checkExpr(a, nil)
	}
	bindings := map[string]*types.Type{}
	// The expected type decides first, so an unsuffixed literal argument can
	// adopt it rather than fall back to its default type. The enum is
	// identified by its name and its package rather than by the id the
	// checker gives an instantiation.
	if want != nil && want.Name == enum.Name && want.Pkg == enum.Pkg {
		for i, p := range d.Params {
			if i < len(want.Args) {
				bindings[p.ID] = want.Args[i]
			}
		}
	}
	for i, pt := range variant.Payload {
		if i >= len(argTypes) || argTypes[i] == nil {
			continue
		}
		b, ok := types.Bindings(pt, argTypes[i])
		if !ok {
			continue
		}
		for k, val := range b {
			if _, seen := bindings[k]; !seen {
				bindings[k] = val
			}
		}
	}
	missing := false
	for _, p := range d.Params {
		if _, ok := bindings[p.ID]; !ok {
			missing = true
		}
	}
	inst := enum.Instantiate(orderedArgs(enum, d, bindings)...)
	if missing {
		c.errorHelp(diag.CodeTypeArity, sel.Name.Sp,
			"declare the result or argument type so the arguments can be inferred",
			"cannot infer the type arguments of %s", enum.Name)
	}
	for i, pt := range variant.Payload {
		if i >= len(argTypes) {
			break
		}
		want := types.Substitute(pt, bindings)
		if !c.accepts(want, v.Args[i], argTypes[i]) {
			c.errorHelp(diag.CodeTypeMismatch, v.Args[i].Span(),
				"pass a value of the payload type",
				"argument %d is %s, want %s", i+1, argTypes[i], want)
		}
	}
	if len(v.Args) != len(variant.Payload) {
		// A variant whose payload is the unit type carries nothing, so it is
		// written without arguments rather than with a value that says
		// nothing.
		unit := false
		if len(v.Args) == 0 && len(variant.Payload) == 1 {
			unit = types.Substitute(variant.Payload[0], bindings).ID == "void"
		}
		if !unit {
			c.errorHelp(diag.CodeArgCount, sel.Name.Sp,
				"pass the declared number of arguments",
				"%s.%s takes %d arguments, got %d",
				enum.Name, variant.Name, len(variant.Payload), len(v.Args))
		}
	}
	return inst
}

// orderedArgs returns the type arguments of a named type in declaration
// order, taking each from the bindings.
func orderedArgs(t *types.Type, d *Decl, bindings map[string]*types.Type) []*types.Type {
	if d == nil {
		return nil
	}
	out := make([]*types.Type, 0, len(d.Params))
	for _, p := range d.Params {
		if bound, ok := bindings[p.ID]; ok {
			out = append(out, bound)
		} else {
			out = append(out, p)
		}
	}
	return out
}

// callArgs describes the arguments of one call site.
type callArgs struct {
	// fn is the callee name, which is annotated with the signature this call
	// instantiates so that no later stage has to work the instantiation out.
	fn ast.Expr
	// sp is the span reported for an argument-count mismatch.
	sp source.Span
	// min is the number of parameters without a default value.
	min int
	// variadic marks a C function whose parameter list ends with `...`.
	variadic bool
	// want is the expected result type, which decides the type arguments an
	// argument list leaves unbound.
	want *types.Type
	// recv is the receiver of a method call. The type arguments of a receiver
	// are already decided, so they are not this call's to infer; it is nil for
	// a free function.
	recv *types.Type
}

// checkArgs checks a call's arguments against a signature and returns the
// result type.
func (c *checker) checkArgs(sig *types.Type, args []ast.Expr, site callArgs) *types.Type {
	if sig == nil || sig.Kind != types.KindFunc {
		return nil
	}
	// Type every argument once. A concrete parameter type decides the type
	// of a literal argument; a type parameter cannot, so the literal keeps
	// its default type and is adapted once the parameter is bound.
	argTypes := make([]*types.Type, len(args))
	for i, a := range args {
		var expect *types.Type
		if i < len(sig.Elems) && !types.ContainsTypeParam(sig.Elems[i]) {
			expect = sig.Elems[i]
		}
		argTypes[i] = c.checkExpr(a, expect)
	}
	params := sig.Elems
	if types.ContainsTypeParam(sig) {
		sig = c.bindParams(sig, args, argTypes, site.want, site.recv)
		params = sig.Elems
		if site.fn != nil {
			c.out.Nodes[site.fn] = sig
		}
	}
	switch {
	case site.variadic && len(args) < len(params):
		c.errorHelp(diag.CodeArgCount, site.sp,
			"pass at least the fixed parameters",
			"%s takes at least %d arguments, got %d", sig, len(params), len(args))
	case !site.variadic && (len(args) < site.min || len(args) > len(params)):
		c.errorHelp(diag.CodeArgCount, site.sp,
			"pass the declared number of arguments",
			"%s takes %s, got %d", sig, arityText(site.min, len(params)), len(args))
	}
	for i, a := range args {
		if i >= len(params) {
			break
		}
		if !c.accepts(params[i], a, argTypes[i]) {
			c.errorHelp(diag.CodeTypeMismatch, a.Span(),
				"pass a value of the declared type",
				"argument %d is %s, want %s", i+1, argTypes[i], params[i])
		}
	}
	return sig.Result
}

// arityText renders the accepted argument counts.
func arityText(min, max int) string {
	if min == max {
		return plural(max)
	}
	return plural(min) + " to " + plural(max)
}

// plural renders an argument count.
func plural(n int) string {
	if n == 1 {
		return "1 argument"
	}
	return strconv.Itoa(n) + " arguments"
}

// requiredArgs returns how many arguments the declaration at sp requires:
// the parameters without a default value.
func (c *checker) requiredArgs(sp *ast.Ident) int {
	sym := c.symOf(sp)
	if sym == nil {
		return 0
	}
	f, ok := sym.Decl.(*ast.FuncDecl)
	if !ok {
		return 0
	}
	n := 0
	for _, p := range f.Params {
		if p.Default == nil && !p.Variadic {
			n++
		}
	}
	return n
}

// methodOf returns the signature of the method named name on recv, with the
// receiver's type arguments applied, or nil when no such method exists.
func (c *checker) methodOf(recv *types.Type, name string) *types.Type {
	if recv == nil || recv.Kind == types.Invalid {
		return nil
	}
	if recv.Kind == types.KindTypeParam {
		// A type parameter reaches the methods its constraints declare,
		// which is the only interface it is known to satisfy.
		return c.out.paramMethod(recv, name)
	}
	if d := c.declOf(recv); d != nil {
		if sig, ok := d.Methods[name]; ok {
			return instantiateMethod(recv, d, sig)
		}
	}
	return builtinMethod(recv, name)
}

// instantiateMethod substitutes a method signature's type parameters with
// the receiver's type arguments.
func instantiateMethod(recv *types.Type, d *Decl, sig *types.Type) *types.Type {
	if len(d.Params) == 0 || len(recv.Args) == 0 {
		return sig
	}
	bindings := map[string]*types.Type{}
	for i, p := range d.Params {
		if i < len(recv.Args) {
			bindings[p.ID] = recv.Args[i]
		}
	}
	return types.Substitute(sig, bindings)
}

// MethodOf returns the signature of the method named name on a receiver
// type, whether the method is declared in source or built into the
// language.
func (p *Package) MethodOf(recv *types.Type, name string) *types.Type {
	if recv == nil || recv.Kind == types.Invalid {
		return nil
	}
	if recv.Kind == types.KindTypeParam {
		return p.paramMethod(recv, name)
	}
	if d := p.Decl(recv); d != nil {
		if sig, ok := d.Methods[name]; ok {
			return instantiateMethod(recv, d, sig)
		}
	}
	return builtinMethod(recv, name)
}

// containerMethods gives the methods of the pre-imported containers. Every
// signature is built from the receiver, because it depends on the
// receiver's type arguments.
var containerMethods = map[string]map[string]func(*types.Type) *types.Type{
	"Array": {
		"Len":   func(*types.Type) *types.Type { return types.FuncOf(types.Usize) },
		"Cap":   func(*types.Type) *types.Type { return types.FuncOf(types.Usize) },
		"Clear": func(*types.Type) *types.Type { return types.FuncOf(void) },
		"Reserve": func(*types.Type) *types.Type {
			return types.FuncOf(void, types.Usize)
		},
		"Insert": func(recv *types.Type) *types.Type {
			return types.FuncOf(void, types.Usize, arg(recv, 0))
		},
		"RemoveRange": func(*types.Type) *types.Type {
			return types.FuncOf(void, types.Usize, types.Usize)
		},
		"Push": func(recv *types.Type) *types.Type {
			return types.FuncOf(void, arg(recv, 0))
		},
		"Pop": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.Option.Instantiate(arg(recv, 0)))
		},
		"Remove": func(*types.Type) *types.Type { return types.FuncOf(void, types.Usize) },
		"Get": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.Option.Instantiate(arg(recv, 0)), types.Usize)
		},
		"get_unchecked": func(recv *types.Type) *types.Type {
			return types.FuncOf(arg(recv, 0), types.Usize)
		},
	},
	"Map": {
		"Len": func(*types.Type) *types.Type { return types.FuncOf(types.Usize) },
		"Get": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.Option.Instantiate(arg(recv, 1)), arg(recv, 0))
		},
		"Contains": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.Bool, arg(recv, 0))
		},
		"Insert": func(recv *types.Type) *types.Type {
			return types.FuncOf(void, arg(recv, 0), arg(recv, 1))
		},
		"Remove": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.Option.Instantiate(arg(recv, 1)), arg(recv, 0))
		},
		"Keys": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.SliceOf(arg(recv, 0)))
		},
		"Values": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.SliceOf(arg(recv, 1)))
		},
		"KeyList": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.Array.Instantiate(arg(recv, 0)))
		},
	},
	"Set": {
		"Len": func(*types.Type) *types.Type { return types.FuncOf(types.Usize) },
		"Add": func(recv *types.Type) *types.Type {
			return types.FuncOf(void, arg(recv, 0))
		},
		"Contains": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.Bool, arg(recv, 0))
		},
		"Remove": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.Option.Instantiate(arg(recv, 0)), arg(recv, 0))
		},
		"Union": func(recv *types.Type) *types.Type {
			return types.FuncOf(void, recv)
		},
		"Intersect": func(recv *types.Type) *types.Type {
			return types.FuncOf(void, recv)
		},
		"Difference": func(recv *types.Type) *types.Type {
			return types.FuncOf(void, recv)
		},
		"ValueList": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.Array.Instantiate(arg(recv, 0)))
		},
	},
	"PriorityQueue": {
		"Len": func(*types.Type) *types.Type { return types.FuncOf(types.Usize) },
		"Push": func(recv *types.Type) *types.Type {
			return types.FuncOf(void, arg(recv, 0))
		},
		"Pop": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.Option.Instantiate(arg(recv, 0)))
		},
	},
	"Ring": {
		"Len":  func(*types.Type) *types.Type { return types.FuncOf(types.Usize) },
		"Cap":  func(*types.Type) *types.Type { return types.FuncOf(types.Usize) },
		"Push": func(recv *types.Type) *types.Type { return types.FuncOf(void, arg(recv, 0)) },
		"Pop": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.Option.Instantiate(arg(recv, 0)))
		},
	},
	"Buf": {
		"Len":       func(*types.Type) *types.Type { return types.FuncOf(types.Usize) },
		"Cap":       func(*types.Type) *types.Type { return types.FuncOf(types.Usize) },
		"Clear":     func(*types.Type) *types.Type { return types.FuncOf(void) },
		"Reset":     func(*types.Type) *types.Type { return types.FuncOf(void) },
		"Reserve":   func(*types.Type) *types.Type { return types.FuncOf(void, types.Usize) },
		"Consume":   func(*types.Type) *types.Type { return types.FuncOf(void, types.Usize) },
		"WriteByte": func(*types.Type) *types.Type { return types.FuncOf(void, types.Uint8) },
		"Write":     func(*types.Type) *types.Type { return types.FuncOf(void, types.SliceOf(types.Uint8)) },
		"WriteStr":  func(*types.Type) *types.Type { return types.FuncOf(void, types.Str) },
		"Bytes":     func(*types.Type) *types.Type { return types.FuncOf(types.SliceOf(types.Uint8)) },
		"Str":       func(*types.Type) *types.Type { return types.FuncOf(types.Str) },
		"Take":      func(*types.Type) *types.Type { return types.FuncOf(types.Array.Instantiate(types.Uint8)) },
	},
	"BlockingQueue": {
		"Len": func(*types.Type) *types.Type { return types.FuncOf(types.Usize) },
		"Push": func(recv *types.Type) *types.Type {
			return types.FuncOf(void, arg(recv, 0))
		},
		"Pop": func(recv *types.Type) *types.Type { return types.FuncOf(arg(recv, 0)) },
		"TryPush": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.Bool, arg(recv, 0))
		},
		"TryPop": func(recv *types.Type) *types.Type {
			return types.FuncOf(types.Option.Instantiate(arg(recv, 0)))
		},
	},
}

// builtinMethod returns the signature of a method the language builds into a
// type, or nil.
func builtinMethod(recv *types.Type, name string) *types.Type {
	switch recv.Kind {
	case types.KindChan:
		switch name {
		case "Send":
			return types.FuncOf(void, recv.Elem)
		case "Recv":
			return types.FuncOf(types.TupleOf(recv.Elem, types.Bool))
		case "TrySend":
			return types.FuncOf(types.Bool, recv.Elem)
		case "TryRecv":
			return types.FuncOf(types.TupleOf(recv.Elem, types.Bool))
		case "Close":
			return types.FuncOf(void)
		}
	case types.KindContainer:
		if table, ok := containerMethods[recv.Name]; ok {
			if build, ok := table[name]; ok {
				return build(recv)
			}
		}
	case types.KindStr:
		switch name {
		case "Len":
			return types.FuncOf(types.Usize)
		case "Bytes":
			return types.FuncOf(types.SliceOf(types.Uint8))
		}
	case types.KindSlice:
		if name == "get_unchecked" {
			return types.FuncOf(recv.Elem, types.Usize)
		}
	case types.KindRaw:
		if t, ok := loadSuffixType(name, "load_"); ok {
			return types.FuncOf(t)
		}
		if t, ok := loadSuffixType(name, "store_"); ok {
			return types.FuncOf(void, t)
		}
	}
	if recv.IsInteger() {
		switch name {
		case "wrap_add", "wrap_sub", "wrap_mul", "rotl", "rotr":
			return types.FuncOf(recv, recv)
		case "swap_bytes":
			return types.FuncOf(recv)
		case "count_ones", "leading_zeros", "trailing_zeros":
			return types.FuncOf(types.Uint32)
		}
	}
	if name == "Show" && showable(recv) {
		// The language gives a value its Show from the fixed table the
		// backend owns, so the method is callable wherever an interface
		// value or a constrained parameter would call it.
		return types.FuncOf(nil, types.Buf)
	}
	if name == "Eq" && equatable(recv) {
		return types.FuncOf(types.Bool, recv)
	}
	if name == "Hash" && hashable(recv) {
		return types.FuncOf(types.Uint64)
	}
	if name == "Compare" && ordered(recv) {
		// The language gives a value its order from the comparison the
		// backend lowers, so the method is callable wherever a constrained
		// parameter would call it.
		return types.FuncOf(types.Int32, recv)
	}
	return nil
}

// loadSuffixType maps the suffix of a load_* or store_* method to its type.
func loadSuffixType(name, prefix string) (*types.Type, bool) {
	if !strings.HasPrefix(name, prefix) {
		return nil, false
	}
	switch strings.TrimPrefix(name, prefix) {
	case "u8":
		return types.Uint8, true
	case "u16":
		return types.Uint16, true
	case "u32":
		return types.Uint32, true
	case "u64":
		return types.Uint64, true
	case "usize":
		return types.Usize, true
	case "i8":
		return types.Int8, true
	case "i16":
		return types.Int16, true
	case "i32":
		return types.Int32, true
	case "i64":
		return types.Int64, true
	case "f32":
		return types.Float32, true
	case "f64":
		return types.Float64, true
	case "raw":
		return types.Raw, true
	}
	return nil, false
}

// arg returns the i-th type argument of a receiver, or void when the
// receiver carries no type arguments.
func arg(t *types.Type, i int) *types.Type {
	if t != nil && i < len(t.Args) {
		return t.Args[i]
	}
	return void
}

// bindParams binds a generic signature's type parameters from the argument
// types and, for the parameters those leave unbound, from the expected result
// type. A parameter the receiver of a method call already decides is not this
// call's to bind.
func (c *checker) bindParams(sig *types.Type, args []ast.Expr, argTypes []*types.Type, want, recv *types.Type) *types.Type {
	bindings := map[string]*types.Type{}
	// The expected result decides first, so an unsuffixed literal argument
	// can adopt it rather than fall back to its default type.
	if want != nil && sig.Result != nil {
		if b, ok := types.Bindings(sig.Result, want); ok {
			for k, v := range b {
				bindings[k] = v
			}
		}
	}
	for i, pt := range sig.Elems {
		if i >= len(argTypes) || argTypes[i] == nil {
			continue
		}
		b, ok := types.Bindings(pt, argTypes[i])
		if !ok {
			continue
		}
		for k, v := range b {
			if prev, seen := bindings[k]; seen {
				if untypedNumeric(args[i]) {
					// The literal adapts to the type the expected result
					// already fixed.
					continue
				}
				if !types.Identical(prev, v) {
					c.errorHelp(diag.CodeTypeMismatch, args[i].Span(),
						"pass arguments that agree on the type parameter",
						"argument %d binds %s to %s, want %s", i+1, k, v, prev)
				}
				continue
			}
			bindings[k] = v
		}
	}
	known := map[string]bool{}
	for _, id := range types.ParamIDs(recv) {
		known[id] = true
	}
	for _, id := range types.ParamIDs(sig) {
		if _, ok := bindings[id]; ok {
			continue
		}
		// A method of a generic type carries the type's parameters in its
		// signature. The receiver fixed them already, so only the parameters
		// this call has to work out are required.
		if known[id] {
			continue
		}
		c.errorHelp(diag.CodeTypeArity, argsSpan(args),
			"declare the result or argument type so the arguments can be inferred",
			"cannot infer the type arguments of %s", sig)
		break
	}
	return types.Substitute(sig, bindings)
}

// argsSpan returns the span of the first argument, or a zero span.
func argsSpan(args []ast.Expr) source.Span {
	if len(args) == 0 {
		return source.Span{}
	}
	return args[0].Span()
}

// accepts reports whether an argument of type got may stand where want is
// expected. An unsuffixed numeric literal adopts the expected numeric type,
// and a generic declaration's own type accepts the form that declaration
// builds when it writes that type with its own parameters.
func (c *checker) accepts(want *types.Type, arg ast.Expr, got *types.Type) bool {
	if c.assignable(want, got) {
		return true
	}
	if want != nil && want.IsNumeric() && untypedNumeric(arg) {
		c.out.Nodes[arg] = want
		return true
	}
	return uninstantiatedForm(want, got)
}

// uninstantiatedForm reports whether want is a generic declaration's own type
// and got is that type instantiated with type parameters, which is the form
// the declaration builds when it names itself without arguments.
func uninstantiatedForm(want, got *types.Type) bool {
	if want == nil || got == nil || !want.IsNamed() || !got.IsNamed() {
		return false
	}
	if want.ID != got.ID || len(want.Args) != 0 || len(got.Args) == 0 {
		return false
	}
	for _, a := range got.Args {
		if a == nil || a.Kind != types.KindTypeParam {
			return false
		}
	}
	return true
}

// assignable reports whether a value of type src may be stored where dst is
// expected: the types must match, or src must implement the interface dst.
func (c *checker) assignable(dst, src *types.Type) bool {
	if types.Assignable(dst, src) {
		return true
	}
	return types.NeedsImplementation(dst, src) && c.implements(src, dst)
}

// untypedNumeric reports whether an expression is a numeric literal that has
// not fixed its type yet, so a numeric position can decide it.
func untypedNumeric(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Literal:
		return (v.Kind == token.Int || v.Kind == token.Float) && v.Suffix == token.SuffixNone
	case *ast.UnaryExpr:
		return v.Op == token.Minus && untypedNumeric(v.X)
	}
	return false
}

// implements reports whether a type implements an interface.
func (c *checker) implements(t, iface *types.Type) bool {
	if t == nil || iface == nil || iface.Kind != types.KindInterface {
		return false
	}
	// A type parameter implements exactly what its constraints require, so a
	// generic body may use the operators and methods the constraint names
	// before any instantiation decides the type behind it.
	if t.Kind == types.KindTypeParam {
		return hasConstraint(t, iface)
	}
	if builtinImplements(t, iface) {
		return true
	}
	if d := c.declOf(t); d != nil {
		for _, impl := range d.Impls {
			if impl.Interface.ID == iface.ID {
				return true
			}
		}
	}
	return false
}

// builtinImplements reports whether the language itself gives a type an
// interface: the types it compares implement Eq, the types it hashes
// implement Hash, the types whose text the backend writes from its fixed
// table implement Show, the types it orders implement Order, and the
// concurrency handles are SyncSafe.
func builtinImplements(t, iface *types.Type) bool {
	switch iface.Name {
	case "Eq":
		return equatable(t)
	case "Hash":
		return hashable(t)
	case "Show":
		return showable(t)
	case "Ord":
		return ordered(t)
	case "SyncSafe":
		switch t.Kind {
		case types.KindChan, types.KindStr:
			return true
		}
		if t.ID == types.JoinHandle.ID || t.ID == types.PanicError.ID {
			return true
		}
		return syncSafeType(t)
	}
	return false
}

// syncSafeType reports whether a named type is one of the concurrency
// primitives the standard library provides. They cross a coroutine boundary
// by reference rather than by copy, because what they guard is shared.
func syncSafeType(t *types.Type) bool {
	if t.Pkg != "sync" && !strings.HasSuffix(t.Pkg, "/sync") {
		return false
	}
	switch t.Name {
	case "Atomic", "Mutex", "Once", "WaitGroup", "Semaphore", "Shared":
		return true
	}
	return false
}

// showable reports whether the language writes the text of the type from the
// backend's fixed table rather than from a declaration. It is exactly the set
// of types whose Show the backend lowers, so the check and the backend never
// disagree about whether a value can be written.
func showable(t *types.Type) bool {
	if t == nil {
		return false
	}
	return t.IsNumeric() || t.Kind == types.KindBool || t.Kind == types.KindChar ||
		t.Kind == types.KindStr || t.Kind == types.KindDuration
}

// equatable reports whether the language gives the type its own equality,
// which is the set the comparison operators accept. It is exactly the set
// whose Eq the backend lowers.
func equatable(t *types.Type) bool {
	if t == nil {
		return false
	}
	return t.IsNumeric() || t.Kind == types.KindBool || t.Kind == types.KindChar ||
		t.Kind == types.KindRaw || t.Kind == types.KindStr || t.Kind == types.KindDuration
}

// ordered reports whether the language gives the type a total order, which is
// the set a sorted container accepts as a key. A floating-point value is left
// out: a NaN makes its comparisons no order at all, so the reasoning that
// keeps it out of Hash keeps it out of Order.
func ordered(t *types.Type) bool {
	if t == nil {
		return false
	}
	return (t.IsNumeric() && !t.IsFloat()) || t.Kind == types.KindBool ||
		t.Kind == types.KindChar || t.Kind == types.KindRaw ||
		t.Kind == types.KindStr || t.Kind == types.KindDuration
}

// hashable reports whether the language gives the type a hash, which is
// exactly the set a container accepts as a key. A floating-point value is
// left out: its equality is not the equality of its bytes, so no hash agrees
// with it.
func hashable(t *types.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case types.KindBool, types.KindInt8, types.KindInt16, types.KindInt32,
		types.KindInt64, types.KindInt128, types.KindUint8, types.KindUint16,
		types.KindUint32, types.KindUint64, types.KindUint128, types.KindUsize,
		types.KindChar, types.KindRaw, types.KindDuration, types.KindStr:
		return true
	}
	return false
}

// hasReferenceMember reports whether a type holds a reference that a deep
// copy would have to follow.
func (c *checker) hasReferenceMember(t *types.Type) bool {
	if t == nil {
		return false
	}
	if t.IsReference() {
		return true
	}
	switch t.Kind {
	case types.KindTuple:
		for _, e := range t.Elems {
			if c.hasReferenceMember(e) {
				return true
			}
		}
	case types.KindArray:
		return c.hasReferenceMember(t.Elem)
	}
	if d := c.declOf(t); d != nil {
		for _, f := range d.Fields {
			if c.hasReferenceMember(f.Type) {
				return true
			}
		}
	}
	return false
}
