// Expression checking.

package check

import (
	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/resolve"
	"zyx/internal/token"
	"zyx/internal/types"
)

// checkExpr checks an expression and returns its type. want is the type the
// surrounding position expects, which decides the type of an unsuffixed
// numeric literal; it may be nil.
func (c *checker) checkExpr(e ast.Expr, want *types.Type) *types.Type {
	t := c.exprType(e, want)
	if e != nil && t != nil {
		c.out.Nodes[e] = t
	}
	return t
}

// exprType dispatches on the expression form.
func (c *checker) exprType(e ast.Expr, want *types.Type) *types.Type {
	switch v := e.(type) {
	case nil:
		return nil
	case *ast.Ident:
		return c.identType(v)
	case *ast.Literal:
		return c.literalType(v, want)
	case *ast.BinaryExpr:
		return c.binaryType(v, want)
	case *ast.UnaryExpr:
		return c.unaryType(v, want)
	case *ast.AddrOfExpr:
		return c.addrType(v)
	case *ast.IsExpr:
		return c.isType(v)
	case *ast.CallExpr:
		return c.checkCall(v, want)
	case *ast.SelectorExpr:
		return c.selectorType(v)
	case *ast.TupleIndexExpr:
		return c.tupleIndexType(v)
	case *ast.IndexExpr:
		return c.indexType(v)
	case *ast.RangeExpr:
		return c.rangeType(v)
	case *ast.NewExpr:
		return c.newType(v)
	case *ast.TryExpr:
		return c.tryType(v)
	case *ast.SpawnExpr:
		return c.spawnType(v)
	case *ast.FuncRefExpr:
		return c.funcRefType(v)
	case *ast.LambdaExpr:
		return c.lambdaType(v)
	case *ast.TupleExpr:
		return c.tupleType(v, want)
	case *ast.ArrayLitExpr:
		return c.arrayLitType(v, want)
	case *ast.ArrayRepeatExpr:
		return c.arrayRepeatType(v, want)
	case *ast.AggregateLitExpr:
		return c.aggregateType(v, want)
	case *ast.FastExpr:
		return c.checkExpr(v.X, want)
	}
	return nil
}

// identType returns the type a name denotes.
func (c *checker) identType(id *ast.Ident) *types.Type {
	if id.Name == "self" {
		return c.self
	}
	sym := c.symOf(id)
	if sym == nil {
		return nil
	}
	if t, ok := c.bySym[sym.ID]; ok {
		return t
	}
	if isTypeSymbol(sym) {
		return c.typeOfSymbol(sym)
	}
	return nil
}

// literalType returns the type of a literal. A numeric literal takes the
// expected type when the position supplies one, and the default type for
// its kind otherwise.
func (c *checker) literalType(lit *ast.Literal, want *types.Type) *types.Type {
	switch lit.Kind {
	case token.Int:
		if t := suffixType(lit.Suffix); t != nil {
			return t
		}
		if want != nil && (want.IsInteger() || want.IsFloat()) {
			return want
		}
		return types.Int64
	case token.Float:
		if t := suffixType(lit.Suffix); t != nil {
			return t
		}
		if want != nil && want.IsFloat() {
			return want
		}
		return types.Float64
	case token.Char:
		return types.Char
	case token.Str:
		return types.Str
	case token.ByteStr:
		return types.SliceOf(types.Uint8)
	case token.True, token.False:
		return types.Bool
	case token.FString:
		return c.fstringType(lit)
	}
	return nil
}

// suffixType returns the type a numeric literal suffix selects.
func suffixType(s token.NumSuffix) *types.Type {
	switch s {
	case token.SuffixI8:
		return types.Int8
	case token.SuffixI16:
		return types.Int16
	case token.SuffixI32:
		return types.Int32
	case token.SuffixI64:
		return types.Int64
	case token.SuffixI128:
		return types.Int128
	case token.SuffixU8:
		return types.Uint8
	case token.SuffixU16:
		return types.Uint16
	case token.SuffixU32:
		return types.Uint32
	case token.SuffixU64:
		return types.Uint64
	case token.SuffixU128:
		return types.Uint128
	case token.SuffixUsize:
		return types.Usize
	case token.SuffixF32:
		return types.Float32
	case token.SuffixF64:
		return types.Float64
	}
	return nil
}

// fstringType checks the interpolations of an f-string, each of which must
// be a primitive type, str, or a type that implements Show.
func (c *checker) fstringType(lit *ast.Literal) *types.Type {
	for _, part := range lit.Parts {
		if part.X == nil {
			continue
		}
		t := c.checkExpr(part.X, nil)
		if t == nil || t.Kind == types.Invalid {
			continue
		}
		if !c.showable(t) {
			c.errorHelp(diag.CodeInterpolation, part.X.Span(),
				"interpolate a primitive type, str, or a type that implements Show",
				"%s cannot appear in an f-string", t)
		}
	}
	return types.Str
}

// showable reports whether a type may be interpolated into an f-string.
func (c *checker) showable(t *types.Type) bool {
	if t.IsBasic() || t.Kind == types.KindStr || t.Kind == types.KindDuration {
		return true
	}
	return c.implements(t, types.Show)
}

// binaryType checks a binary operator against the operand rules of the
// language.
func (c *checker) binaryType(v *ast.BinaryExpr, want *types.Type) *types.Type {
	lt := c.checkExpr(v.X, want)
	rt := c.checkExpr(v.Y, want)
	lt, rt = c.adaptLiterals(v.X, v.Y, lt, rt)
	if lt == nil || rt == nil || lt.Kind == types.Invalid || rt.Kind == types.Invalid {
		return comparisonResult(v.Op)
	}
	if !types.Identical(lt, rt) {
		c.errorHelp(diag.CodeTypeMismatch, v.Sp,
			"convert one operand explicitly; the language has no implicit conversion",
			"operator %s needs two operands of one type, got %s and %s", v.Op, lt, rt)
		return comparisonResult(v.Op)
	}
	switch v.Op {
	case token.Eq, token.Ne:
		if !c.equalityOK(lt) {
			c.errorHelp(diag.CodeTypeMismatch, v.Sp,
				"implement Eq for the type, or compare an enum instead",
				"%s does not support %s", lt, v.Op)
		}
		return types.Bool
	case token.Lt, token.Le, token.Gt, token.Ge:
		if !lt.IsOrdered() {
			c.errorHelp(diag.CodeTypeMismatch, v.Sp,
				"compare numbers or char; text ordering lives in std/str",
				"%s is not ordered", lt)
		}
		return types.Bool
	case token.AndAnd, token.OrOr:
		if !lt.IsBool() {
			c.errorHelp(diag.CodeTypeMismatch, v.Sp,
				"operate on bool", "%s is not bool", lt)
		}
		return types.Bool
	case token.Plus:
		if lt.Kind == types.KindStr {
			return types.Str
		}
		return c.arithmetic(v, lt)
	case token.Minus, token.Star, token.Slash, token.Percent:
		return c.arithmetic(v, lt)
	case token.Shl, token.Shr, token.Amp, token.Caret, token.Pipe:
		if !lt.IsInteger() {
			c.errorHelp(diag.CodeTypeMismatch, v.Sp,
				"operate on integers", "%s is not an integer type", lt)
		}
		return lt
	}
	return nil
}

// arithmetic checks that an arithmetic operator has numeric operands.
func (c *checker) arithmetic(v *ast.BinaryExpr, t *types.Type) *types.Type {
	if !t.IsNumeric() && t.Kind != types.KindDuration {
		c.errorHelp(diag.CodeTypeMismatch, v.Sp,
			"use numeric operands", "%s is not numeric", t)
	}
	return t
}

// comparisonResult returns the type of a comparison or logical operator,
// which is bool, and nil for every other operator.
func comparisonResult(op token.Kind) *types.Type {
	switch op {
	case token.Eq, token.Ne, token.Lt, token.Le, token.Gt, token.Ge,
		token.AndAnd, token.OrOr:
		return types.Bool
	}
	return nil
}

// adaptLiterals lets an unsuffixed numeric literal adopt the type of the
// other operand, which fixes the type of `1 + x` and `x + 1`.
func (c *checker) adaptLiterals(x, y ast.Expr, lt, rt *types.Type) (*types.Type, *types.Type) {
	if lt == nil || rt == nil || types.Identical(lt, rt) {
		return lt, rt
	}
	if untypedNumeric(x) && rt.IsNumeric() {
		c.out.Nodes[x] = rt
		return rt, rt
	}
	if untypedNumeric(y) && lt.IsNumeric() {
		c.out.Nodes[y] = lt
		return lt, lt
	}
	return lt, rt
}

// equalityOK reports whether a type supports == and !=: the primitive
// types, str, Duration, and any type that implements Eq. Interface values
// compare only through their interface.
func (c *checker) equalityOK(t *types.Type) bool {
	switch t.Kind {
	case types.KindStr, types.KindDuration:
		return true
	case types.KindInterface:
		return false
	}
	if t.IsBasic() {
		return true
	}
	return c.implements(t, types.Eq)
}

// unaryType checks a prefix operator.
func (c *checker) unaryType(v *ast.UnaryExpr, want *types.Type) *types.Type {
	t := c.checkExpr(v.X, want)
	if t == nil || t.Kind == types.Invalid {
		return t
	}
	switch v.Op {
	case token.Minus:
		if t.IsUnsigned() {
			c.errorHelp(diag.CodeTypeMismatch, v.Sp,
				"negate a signed integer or a float",
				"cannot negate the unsigned type %s", t)
			return t
		}
		if !t.IsSigned() && !t.IsFloat() && t.Kind != types.KindDuration {
			c.errorHelp(diag.CodeTypeMismatch, v.Sp,
				"negate a signed integer or a float", "%s cannot be negated", t)
		}
		return t
	case token.Bang:
		if !t.IsBool() {
			c.errorHelp(diag.CodeTypeMismatch, v.Sp,
				"negate a bool", "%s is not bool", t)
		}
		return types.Bool
	case token.Tilde:
		if !t.IsInteger() {
			c.errorHelp(diag.CodeTypeMismatch, v.Sp,
				"invert an integer", "%s is not an integer type", t)
		}
		return t
	}
	return nil
}

// addrType checks `&x`, which is only allowed as an argument of a C call.
func (c *checker) addrType(v *ast.AddrOfExpr) *types.Type {
	c.checkExpr(v.X, nil)
	if !c.addrOK {
		c.errorHelp(diag.CodeAddrOf, v.Sp,
			"pass the address directly as a C call argument",
			"&x is only allowed in the argument list of a C call")
	}
	return types.Raw
}

// isType checks `x is T`, which tests an interface value and produces bool.
func (c *checker) isType(v *ast.IsExpr) *types.Type {
	t := c.checkExpr(v.X, nil)
	if t != nil && t.Kind != types.Invalid && t.Kind != types.KindInterface {
		c.errorHelp(diag.CodeTypeMismatch, v.Sp,
			"test an interface value; branch on an enum for concrete types",
			"%s is not an interface", t)
	}
	c.buildType(v.Type)
	return types.Bool
}

// selectorType checks `p.x`, `ctx.Handle`, and `net.Listen` in value
// position.
func (c *checker) selectorType(v *ast.SelectorExpr) *types.Type {
	if id, ok := v.X.(*ast.Ident); ok {
		if isNamespaceName(id.Name) {
			return c.namespaceValue(v, id.Name)
		}
		switch sym := c.symOf(id); {
		case sym != nil && sym.Kind == resolve.KindPackage:
			return c.qualifiedValue(v, sym)
		case isTypeSymbol(sym):
			return c.variantValue(v, sym)
		}
	}
	recv := c.checkExpr(v.X, nil)
	if recv == nil || recv.Kind == types.Invalid {
		return nil
	}
	if f, t := c.fieldType(recv, v.Name.Name); f != nil {
		c.out.Nodes[v.Name] = t
		return t
	}
	if sig := c.methodOf(recv, v.Name.Name); sig != nil {
		if c.isBuiltinMethod(recv, v.Name.Name) {
			c.errorHelp(diag.CodeBuiltinMethodValue, v.Name.Sp,
				"call the method directly",
				"a built-in method cannot be used as a value")
			return nil
		}
		c.out.Nodes[v.Name] = sig
		return sig
	}
	c.errorHelp(diag.CodeNoField, v.Name.Sp,
		"check the field or method name",
		"%s has no field or method %q", recv, v.Name.Name)
	return nil
}

// fieldType returns a field of a receiver and its type, with the receiver's
// type arguments applied.
func (c *checker) fieldType(recv *types.Type, name string) (*Field, *types.Type) {
	d := c.declOf(recv)
	if d == nil {
		return nil, nil
	}
	f := d.Field(name)
	if f == nil {
		return nil, nil
	}
	return f, c.substituteFor(recv, d, f.Type)
}

// substituteFor applies a receiver's type arguments to a declaration's
// types.
func (c *checker) substituteFor(recv *types.Type, d *Decl, t *types.Type) *types.Type {
	if len(d.Params) == 0 || len(recv.Args) == 0 || !types.ContainsTypeParam(t) {
		return t
	}
	bindings := map[string]*types.Type{}
	for i, p := range d.Params {
		if i < len(recv.Args) {
			bindings[p.ID] = recv.Args[i]
		}
	}
	return types.Substitute(t, bindings)
}

// isBuiltinMethod reports whether a method comes from the language rather
// than a declaration, which forbids taking its value.
func (c *checker) isBuiltinMethod(recv *types.Type, name string) bool {
	if name == "clone" {
		return true
	}
	if d := c.declOf(recv); d != nil {
		if _, ok := d.Methods[name]; ok {
			return false
		}
	}
	return true
}

// qualifiedValue returns the type of `pkg.Name` in value position.
func (c *checker) qualifiedValue(v *ast.SelectorExpr, pkg *resolve.Symbol) *types.Type {
	if pkg.Name == "c" {
		c.errorHelp(diag.CodeBuiltinMethodValue, v.Name.Sp,
			"call the C function directly",
			"a C symbol is not a value")
		return nil
	}
	t := c.importedType(pkg, v.Name.Name)
	if t == nil {
		return nil
	}
	c.out.Nodes[v.Name] = t
	return t
}

// variantValue returns the type of `Enum.Variant` in value position, which
// is only allowed for a variant without a payload.
func (c *checker) variantValue(v *ast.SelectorExpr, sym *resolve.Symbol) *types.Type {
	enum := c.typeOfSymbol(sym)
	d := c.declOf(enum)
	if enum == nil || d == nil {
		return nil
	}
	if variant := d.Variant(v.Name.Name); variant != nil && len(variant.Payload) == 0 {
		c.out.Nodes[v.Name] = enum
		return enum
	}
	c.errorHelp(diag.CodeEnumLiteral, v.Name.Sp,
		"construct the variant with arguments, or match on it",
		"%s.%s needs a construction expression", enum.Name, v.Name.Name)
	return nil
}

// importedType returns the declared type of a name in an imported package.
func (c *checker) importedType(pkg *resolve.Symbol, name string) *types.Type {
	if c.imports == nil {
		return nil
	}
	itf, ok := c.imports.Lookup(pkg.ID)
	if !ok || itf == nil {
		return nil
	}
	return itf.Decls[name]
}

// tupleIndexType checks `t.0`.
func (c *checker) tupleIndexType(v *ast.TupleIndexExpr) *types.Type {
	t := c.checkExpr(v.X, nil)
	if t == nil || t.Kind == types.Invalid {
		return nil
	}
	if t.Kind != types.KindTuple {
		c.errorHelp(diag.CodeNoField, v.Sp,
			"index a tuple with a dot and a position",
			"%s is not a tuple", t)
		return nil
	}
	if v.Index < 0 || v.Index >= len(t.Elems) {
		c.errorHelp(diag.CodeNoField, v.Sp,
			"use a position the tuple has",
			"%s has no element %d", t, v.Index)
		return nil
	}
	return t.Elems[v.Index]
}

// indexType checks `a[i]` and the slice `a[i..j]`.
func (c *checker) indexType(v *ast.IndexExpr) *types.Type {
	if r, ok := v.Index.(*ast.RangeExpr); ok {
		return c.sliceType(v, r)
	}
	t := c.checkExpr(v.X, nil)
	if t == nil || t.Kind == types.Invalid {
		return nil
	}
	c.checkExpr(v.Index, types.Usize)
	switch t.Kind {
	case types.KindStr:
		return types.Uint8
	case types.KindSlice:
		return t.Elem
	case types.KindArray:
		return t.Elem
	case types.KindContainer:
		switch t.Name {
		case "Array":
			return arg(t, 0)
		case "Map":
			return types.Option.Instantiate(arg(t, 1))
		}
	}
	c.errorHelp(diag.CodeIndex, v.Sp,
		"index a container, a fixed-size array, a slice, or str",
		"%s cannot be indexed", t)
	return nil
}

// sliceType checks `a[i..j]`, which produces the read-only view of the same
// element type.
func (c *checker) sliceType(v *ast.IndexExpr, r *ast.RangeExpr) *types.Type {
	t := c.checkExpr(v.X, nil)
	c.checkExpr(r.Lo, types.Usize)
	c.checkExpr(r.Hi, types.Usize)
	if t == nil || t.Kind == types.Invalid {
		return nil
	}
	switch t.Kind {
	case types.KindStr:
		return types.Str
	case types.KindSlice:
		return t
	case types.KindArray:
		return types.SliceOf(t.Elem)
	case types.KindContainer:
		if t.Name == "Array" {
			return types.SliceOf(arg(t, 0))
		}
	}
	c.errorHelp(diag.CodeIndex, v.Sp,
		"slice a container, a fixed-size array, a slice, or str",
		"%s cannot be sliced", t)
	return nil
}

// rangeType reports a range used outside the three positions that accept
// one: a for loop, a slice, and a match pattern.
func (c *checker) rangeType(v *ast.RangeExpr) *types.Type {
	c.checkExpr(v.Lo, nil)
	c.checkExpr(v.Hi, nil)
	c.errorHelp(diag.CodeTypeMismatch, v.Sp,
		"use a range in a for loop, a slice, or a match pattern",
		"a range is not a value")
	return nil
}

// tryType checks `try call`, which unwraps a Result and returns its error
// from the enclosing function.
func (c *checker) tryType(v *ast.TryExpr) *types.Type {
	t := c.checkExpr(v.X, nil)
	if t == nil || t.Kind == types.Invalid {
		return nil
	}
	if t.ID != types.Result.ID {
		c.errorHelp(diag.CodeTryResult, v.Sp,
			"apply try to a call that returns Result",
			"try needs a Result, got %s", t)
		return nil
	}
	ok := arg(t, 0)
	err := arg(t, 1)
	if c.result == nil || c.result.ID != types.Result.ID || !types.Identical(arg(c.result, 1), err) {
		c.errorHelp(diag.CodeTryResult, v.Sp,
			"declare the enclosing result as a matching Result",
			"try propagates %s, but the function returns %s", err, c.result)
	}
	return ok
}

// spawnType checks `spawn call` and `spawn_watched call, watcher`, which
// yield a coroutine handle.
func (c *checker) spawnType(v *ast.SpawnExpr) *types.Type {
	if c.inComptime {
		c.errorHelp(diag.CodeSpawnComptime, v.Sp,
			"start coroutines at run time",
			"spawn is a run-time operation")
	}
	if c.inRegion == 0 {
		c.errorHelp(diag.CodeSpawnOutsideRegion, v.Sp,
			"wrap the spawn in a region block",
			"spawn is only allowed inside a region")
	}
	r := c.checkExpr(v.X, nil)
	c.checkExpr(v.Watcher, nil)
	if r == nil || r.Kind == types.Invalid {
		return nil
	}
	return types.JoinHandle.Instantiate(r)
}

// funcRefType checks `func Name` and `func pkg.Name`.
func (c *checker) funcRefType(v *ast.FuncRefExpr) *types.Type {
	if v.Name == nil {
		return nil
	}
	var sig *types.Type
	if v.Qualifier != nil {
		pkg := c.symOf(v.Qualifier)
		if pkg == nil || pkg.Kind != resolve.KindPackage {
			return nil
		}
		sig = c.importedType(pkg, v.Name.Name)
	} else {
		sym := c.symOf(v.Name)
		if sym == nil {
			return nil
		}
		sig = c.bySym[sym.ID]
		if sig == nil {
			sig = c.typeOfSymbol(sym)
		}
	}
	if sig == nil {
		return nil
	}
	if sig.Kind != types.KindFunc {
		c.errorHelp(diag.CodeNotCallable, v.Name.Sp,
			"reference a function", "%s is not a function", sig)
		return nil
	}
	if types.ContainsTypeParam(sig) {
		c.errorHelp(diag.CodeTypeMismatch, v.Sp,
			"call the generic function directly, so its arguments can be inferred",
			"a generic function has no function value")
		return nil
	}
	c.out.Nodes[v.Name] = sig
	return sig
}

// lambdaType checks a lambda, which captures nothing.
func (c *checker) lambdaType(v *ast.LambdaExpr) *types.Type {
	prevSelf, prevResult, prevComptime := c.self, c.result, c.inComptime
	c.self, c.result, c.inComptime = nil, c.buildType(v.Result), false
	params := make([]*types.Type, 0, len(v.Params))
	for _, p := range v.Params {
		t := c.buildType(p.Type)
		params = append(params, t)
		c.bindName(p.Name, t)
		if p.Default != nil {
			c.inComptime = true
			c.checkExpr(p.Default, t)
			c.inComptime = false
		}
	}
	if v.Body != nil {
		c.checkBlock(v.Body)
	} else if v.BodyExpr != nil {
		c.checkResult(v.BodyExpr, c.checkExpr(v.BodyExpr, c.result))
	}
	c.self, c.result, c.inComptime = prevSelf, prevResult, prevComptime
	return types.FuncOf(c.buildType(v.Result), params...)
}

// tupleType checks a tuple expression.
func (c *checker) tupleType(v *ast.TupleExpr, want *types.Type) *types.Type {
	elems := make([]*types.Type, 0, len(v.Elems))
	for i, e := range v.Elems {
		var expect *types.Type
		if want != nil && want.Kind == types.KindTuple && i < len(want.Elems) {
			expect = want.Elems[i]
		}
		elems = append(elems, c.checkExpr(e, expect))
	}
	return types.TupleOf(elems...)
}

// arrayLitType checks `[a, b, c]`.
func (c *checker) arrayLitType(v *ast.ArrayLitExpr, want *types.Type) *types.Type {
	elem := elementExpectation(want)
	for _, e := range v.Elems {
		t := c.checkExpr(e, elem)
		if elem == nil && t != nil && t.Kind != types.Invalid {
			elem = t
		}
	}
	if elem == nil {
		elem = types.Int64
	}
	if want != nil && want.Kind == types.KindArray {
		return types.ArrayOf(elem, want.Len)
	}
	return types.Array.Instantiate(elem)
}

// arrayRepeatType checks `[expr] * n`.
func (c *checker) arrayRepeatType(v *ast.ArrayRepeatExpr, want *types.Type) *types.Type {
	elem := elementExpectation(want)
	t := c.checkExpr(v.Elem, elem)
	if t != nil && t.Kind != types.Invalid {
		elem = t
	}
	if want != nil && want.Kind == types.KindArray {
		// The count must be the declared length.
		n := c.constCount(v.Count)
		if n != want.Len {
			c.errorHelp(diag.CodeArrayLength, v.Count.Span(),
				"repeat the element exactly the declared number of times",
				"%s repeats %d times, want %d", want, n, want.Len)
		}
		return types.ArrayOf(elem, want.Len)
	}
	c.checkExpr(v.Count, types.Usize)
	if elem == nil {
		elem = types.Int64
	}
	return types.Array.Instantiate(elem)
}

// newType checks `new T(args)`: a struct through its init, or a
// pre-imported container through its runtime constructor.
func (c *checker) newType(v *ast.NewExpr) *types.Type {
	typ := c.buildType(v.Type)
	if typ == nil {
		c.checkExprs(v.Args)
		return nil
	}
	switch {
	case typ.Kind == types.KindContainer:
		return c.newContainer(v, typ)
	case typ.Kind == types.KindStruct || typ.Kind == types.KindExternStruct:
		return c.newStruct(v, typ)
	}
	c.errorHelp(diag.CodeTypeMismatch, v.Type.Span(),
		"build a struct through new, or use a construction literal",
		"%s cannot be built with new", typ)
	c.checkExprs(v.Args)
	return typ
}

// newStruct checks `new T(args)` against the init of a struct.
func (c *checker) newStruct(v *ast.NewExpr, typ *types.Type) *types.Type {
	d := c.declOf(typ)
	if d == nil {
		c.checkExprs(v.Args)
		return typ
	}
	init, ok := d.Methods["init"]
	if !ok {
		c.errorHelp(diag.CodeTypeMismatch, v.Type.Span(),
			"declare an init in the type body, or build a construction literal",
			"%s has no init", typ)
		c.checkExprs(v.Args)
		return typ
	}
	sig := c.substituteFor(typ, d, init)
	c.checkArgs(sig, v.Args, v.Sp, 0, false, typ)
	return typ
}

// newContainer checks `new Array<T>(cap)` and the other container
// constructors.
func (c *checker) newContainer(v *ast.NewExpr, typ *types.Type) *types.Type {
	elem := arg(typ, 0)
	less := types.FuncOf(types.Bool, elem, elem)
	var params []*types.Type
	switch typ.Name {
	case "Array":
		params = []*types.Type{types.Usize}
	case "Map", "Set":
		params = nil
	case "PriorityQueue":
		params = []*types.Type{less}
	case "BlockingQueue":
		params = []*types.Type{types.Usize, less}
	default:
		c.errorHelp(diag.CodeTypeMismatch, v.Type.Span(),
			"build a container with new",
			"%s cannot be built with new", typ)
	}
	c.checkArgs(types.FuncOf(typ, params...), v.Args, v.Sp, len(params), false, typ)
	return typ
}

// checkExprs checks every expression for its effects.
func (c *checker) checkExprs(exprs []ast.Expr) {
	for _, e := range exprs {
		c.checkExpr(e, nil)
	}
}

// elementExpectation returns the element type a position expects.
func elementExpectation(want *types.Type) *types.Type {
	if want == nil {
		return nil
	}
	switch want.Kind {
	case types.KindArray, types.KindSlice:
		return want.Elem
	case types.KindContainer:
		if want.Name == "Array" {
			return arg(want, 0)
		}
	}
	return nil
}

// constCount evaluates the repetition count of an array literal. Only a
// constant integer is accepted.
func (c *checker) constCount(e ast.Expr) uint64 {
	lit, ok := e.(*ast.Literal)
	if !ok || lit.Kind != token.Int || lit.Suffix != token.SuffixNone {
		c.errorHelp(diag.CodeArrayLength, spanOf(e),
			"write a plain integer, such as [0] * 16",
			"the repetition count must be a constant integer")
		return 0
	}
	c.out.Nodes[e] = types.Usize
	return parseCount(lit.Value)
}

// aggregateType checks a construction literal.
func (c *checker) aggregateType(v *ast.AggregateLitExpr, want *types.Type) *types.Type {
	typ := c.buildType(v.Type)
	// A literal written with a bare generic name takes its type arguments
	// from the expected type, which is how an empty Map or Set is written.
	if typ != nil && typ.Arity > 0 && len(typ.Args) == 0 && want != nil && want.ID == typ.ID {
		typ = want
	}
	if typ == nil {
		for _, e := range v.Entries {
			c.checkExpr(e.Key, nil)
			c.checkExpr(e.Value, nil)
		}
		return nil
	}
	d := c.declOf(typ)
	switch {
	case typ.Kind == types.KindEnum:
		c.errorHelp(diag.CodeEnumLiteral, v.Type.Span(),
			"build an enum value through its variants",
			"%s is an enum", typ)
	case d != nil && (typ.Kind == types.KindStruct || typ.Kind == types.KindExternStruct ||
		typ.Kind == types.KindUnion):
		c.checkStructLiteral(v, typ, d)
	case typ.Kind == types.KindContainer && typ.Name == "Map":
		c.checkMapLiteral(v, typ)
	case typ.Kind == types.KindContainer && typ.Name == "Set":
		c.checkSetLiteral(v, typ)
	default:
		c.errorHelp(diag.CodeTypeMismatch, v.Type.Span(),
			"build a struct, a union, a Map, or a Set with a literal",
			"%s cannot be built with a literal", typ)
	}
	return typ
}

// checkStructLiteral checks `Point { x: 1 }`, which must list every field
// exactly once.
func (c *checker) checkStructLiteral(v *ast.AggregateLitExpr, typ *types.Type, d *Decl) {
	seen := map[string]bool{}
	for _, e := range v.Entries {
		id, ok := e.Key.(*ast.Ident)
		if !ok {
			c.errorHelp(diag.CodeNoField, spanOf(e.Key),
				"write the field name before the colon",
				"a struct literal names fields")
			c.checkExpr(e.Value, nil)
			continue
		}
		f := d.Field(id.Name)
		if f == nil {
			c.errorHelp(diag.CodeNoField, id.Sp,
				"check the field name",
				"%s has no field %q", typ, id.Name)
			c.checkExpr(e.Value, nil)
			continue
		}
		if seen[id.Name] {
			c.errorHelp(diag.CodeDuplicateField, id.Sp,
				"set each field once",
				"field %q is set twice", id.Name)
		}
		seen[id.Name] = true
		want := c.substituteFor(typ, d, f.Type)
		got := c.checkExpr(e.Value, want)
		if !c.accepts(want, e.Value, got) {
			c.errorHelp(diag.CodeTypeMismatch, e.Value.Span(),
				"assign a value of the field type",
				"field %q is %s, want %s", id.Name, got, want)
		}
	}
	for _, f := range d.Fields {
		if !seen[f.Name] {
			c.errorHelp(diag.CodeMissingField, v.Sp,
				"list every field of the type",
				"field %q is not set", f.Name)
		}
	}
}

// checkMapLiteral checks `Map { k: v }`.
func (c *checker) checkMapLiteral(v *ast.AggregateLitExpr, typ *types.Type) {
	key, val := arg(typ, 0), arg(typ, 1)
	for _, e := range v.Entries {
		if e.Key == nil {
			c.errorHelp(diag.CodeTypeMismatch, spanOf(e.Value),
				"write a key and a value", "a Map entry needs a key")
			continue
		}
		kt := c.checkExpr(e.Key, key)
		if !c.accepts(key, e.Key, kt) {
			c.errorHelp(diag.CodeTypeMismatch, e.Key.Span(),
				"use a key of the map's key type",
				"key is %s, want %s", kt, key)
		}
		vt := c.checkExpr(e.Value, val)
		if !c.accepts(val, e.Value, vt) {
			c.errorHelp(diag.CodeTypeMismatch, e.Value.Span(),
				"use a value of the map's value type",
				"value is %s, want %s", vt, val)
		}
	}
}

// checkSetLiteral checks `Set { a, b }`.
func (c *checker) checkSetLiteral(v *ast.AggregateLitExpr, typ *types.Type) {
	elem := arg(typ, 0)
	for _, e := range v.Entries {
		if e.Key != nil {
			c.errorHelp(diag.CodeTypeMismatch, e.Key.Span(),
				"write the elements without keys", "a Set entry has no key")
		}
		t := c.checkExpr(e.Value, elem)
		if !c.accepts(elem, e.Value, t) {
			c.errorHelp(diag.CodeTypeMismatch, e.Value.Span(),
				"use an element of the set's type",
				"element is %s, want %s", t, elem)
		}
	}
}
