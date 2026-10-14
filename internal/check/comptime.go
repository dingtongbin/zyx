// Compile-time evaluation of constant expressions.

package check

import (
	"errors"
	"strconv"
	"strings"

	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/layout"
	"zyx/internal/resolve"
	"zyx/internal/source"
	"zyx/internal/token"
	"zyx/internal/types"
)

// A Value is a compile-time value.
type Value struct {
	// Type is the value's type.
	Type *types.Type
	// Int holds an integer or char value, read with the signedness of Type.
	Int uint64
	// Float holds a floating-point value.
	Float float64
	// Str holds a text value.
	Str string
	// Bool holds a boolean value.
	Bool bool
	// Elems holds the elements of a tuple, a fixed-size array, a list, or
	// the entries of a map or set.
	Elems []*Value
	// Keys holds the keys of a map value; nil for every other value.
	Keys []*Value
	// Fields holds the fields of a struct value, in declaration order.
	Fields []NamedValue
	// Sym names the declaration of a function value.
	Sym *resolve.Symbol
	// TypeVal holds the type a value of type `type` names.
	TypeVal *types.Type
}

// A NamedValue is one field of a struct value.
type NamedValue struct {
	Name  string
	Value *Value
}

// intValue returns an integer or char value.
func intValue(t *types.Type, n uint64) *Value { return &Value{Type: t, Int: n} }

// boolValue returns a boolean value.
func boolValue(b bool) *Value { return &Value{Type: types.Bool, Bool: b} }

// strValue returns a text value.
func strValue(s string) *Value { return &Value{Type: types.Str, Str: s} }

// constValue returns the value of a constant, evaluating its initializer on
// first use and remembering the result. A constant that is not a
// compile-time expression reports once and yields nil.
func (c *checker) constValue(sym *resolve.Symbol) *Value {
	if sym == nil {
		return nil
	}
	if v, ok := c.consts[sym.ID]; ok {
		return v
	}
	// Mark the constant before evaluating, so a cycle reports instead of
	// recursing forever.
	c.consts[sym.ID] = nil
	switch decl := sym.Decl.(type) {
	case *ast.ConstDecl:
		v := c.eval(decl.Value)
		if v == nil {
			c.errorHelp(diag.CodeComptimeRequired, spanOf(decl.Value),
				"use a literal, a constant, or a compile-time operation",
				"%q must be a compile-time constant", sym.Name)
		}
		c.consts[sym.ID] = v
		return v
	case *ast.VarDecl:
		v := c.eval(decl.Value)
		c.consts[sym.ID] = v
		return v
	}
	return nil
}

// eval evaluates an expression at compile time, or returns nil when the
// expression is not a constant.
func (c *checker) eval(e ast.Expr) *Value {
	switch v := e.(type) {
	case nil:
		return nil
	case *ast.Literal:
		return c.evalLiteral(v)
	case *ast.Ident:
		return c.constValue(c.symOf(v))
	case *ast.UnaryExpr:
		return c.evalUnary(v)
	case *ast.BinaryExpr:
		return c.evalBinary(v.Op, c.eval(v.X), c.eval(v.Y), v.Sp)
	case *ast.CallExpr:
		return c.evalCall(v)
	case *ast.TupleExpr:
		return c.evalElems(v.Elems, types.TupleOf())
	case *ast.ArrayLitExpr:
		return c.evalElems(v.Elems, types.SliceOf(nil))
	case *ast.ArrayRepeatExpr:
		return c.evalRepeat(v)
	case *ast.AggregateLitExpr:
		return c.evalAggregate(v)
	case *ast.SelectorExpr:
		return c.evalSelector(v)
	case *ast.TupleIndexExpr:
		return c.evalTupleIndex(v)
	case *ast.IndexExpr:
		return c.evalIndex(v)
	}
	return nil
}

// evalLiteral evaluates a literal.
func (c *checker) evalLiteral(lit *ast.Literal) *Value {
	t := c.out.Nodes[lit]
	if t == nil {
		t = c.literalType(lit, nil)
	}
	switch lit.Kind {
	case token.Int:
		return intValue(t, parseCount(lit.Value))
	case token.Float:
		f, err := strconv.ParseFloat(strings.ReplaceAll(lit.Value, "_", ""), 64)
		if err != nil {
			return nil
		}
		return &Value{Type: t, Float: f}
	case token.Char:
		runes := []rune(lit.Value)
		if len(runes) != 1 {
			return nil
		}
		return intValue(types.Char, uint64(runes[0]))
	case token.Str:
		return strValue(lit.Value)
	case token.True:
		return boolValue(true)
	case token.False:
		return boolValue(false)
	case token.FString:
		return c.evalFString(lit)
	}
	return nil
}

// evalFString evaluates an f-string whose interpolations are constants.
func (c *checker) evalFString(lit *ast.Literal) *Value {
	var out strings.Builder
	for _, part := range lit.Parts {
		if part.X == nil {
			out.WriteString(part.Text)
			continue
		}
		v := c.eval(part.X)
		if v == nil {
			return nil
		}
		text, ok := showValue(v)
		if !ok {
			return nil
		}
		out.WriteString(text)
	}
	return strValue(out.String())
}

// showValue renders a compile-time value the way an f-string shows it.
func showValue(v *Value) (string, bool) {
	switch {
	case v.Type == nil:
		return "", false
	case v.Type.IsInteger():
		if v.Type.IsSigned() {
			return strconv.FormatInt(int64(v.Int), 10), true
		}
		return strconv.FormatUint(v.Int, 10), true
	case v.Type.IsFloat():
		return strconv.FormatFloat(v.Float, 'g', -1, 64), true
	case v.Type.IsBool():
		return strconv.FormatBool(v.Bool), true
	case v.Type.IsStr():
		return v.Str, true
	case v.Type.IsChar():
		return string(rune(v.Int)), true
	}
	return "", false
}

// evalElems evaluates a list of expressions.
func (c *checker) evalElems(exprs []ast.Expr, elem *types.Type) *Value {
	elems := make([]*Value, 0, len(exprs))
	for _, e := range exprs {
		v := c.eval(e)
		if v == nil {
			return nil
		}
		elems = append(elems, v)
	}
	t := elem
	if t != nil && len(elems) > 0 {
		t = types.Array.Instantiate(elems[0].Type)
	}
	return &Value{Type: t, Elems: elems}
}

// evalRepeat evaluates `[expr] * n`.
func (c *checker) evalRepeat(v *ast.ArrayRepeatExpr) *Value {
	elem := c.eval(v.Elem)
	count := c.eval(v.Count)
	if elem == nil || count == nil || !count.Type.IsInteger() {
		return nil
	}
	out := make([]*Value, 0, count.Int)
	for i := uint64(0); i < count.Int; i++ {
		out = append(out, elem)
	}
	return &Value{Type: types.Array.Instantiate(elem.Type), Elems: out}
}

// evalAggregate evaluates a construction literal.
func (c *checker) evalAggregate(v *ast.AggregateLitExpr) *Value {
	typ := c.out.Nodes[v]
	if typ == nil {
		return nil
	}
	d := c.declOf(typ)
	switch {
	case typ.Kind == types.KindContainer && typ.Name == "Map":
		out := &Value{Type: typ}
		for _, e := range v.Entries {
			k := c.eval(e.Key)
			val := c.eval(e.Value)
			if k == nil || val == nil {
				return nil
			}
			out.Keys = append(out.Keys, k)
			out.Elems = append(out.Elems, val)
		}
		return out
	case typ.Kind == types.KindContainer && typ.Name == "Set":
		out := &Value{Type: typ}
		for _, e := range v.Entries {
			val := c.eval(e.Value)
			if val == nil {
				return nil
			}
			out.Elems = append(out.Elems, val)
		}
		return out
	case d != nil:
		out := &Value{Type: typ}
		for _, e := range v.Entries {
			id, ok := e.Key.(*ast.Ident)
			if !ok {
				return nil
			}
			val := c.eval(e.Value)
			if val == nil {
				return nil
			}
			out.Fields = append(out.Fields, NamedValue{Name: id.Name, Value: val})
		}
		return out
	}
	return nil
}

// evalSelector evaluates a field access on a constant struct.
func (c *checker) evalSelector(v *ast.SelectorExpr) *Value {
	recv := c.eval(v.X)
	if recv == nil {
		return nil
	}
	for _, f := range recv.Fields {
		if f.Name == v.Name.Name {
			return f.Value
		}
	}
	return nil
}

// evalTupleIndex evaluates `t.0` on a constant tuple.
func (c *checker) evalTupleIndex(v *ast.TupleIndexExpr) *Value {
	recv := c.eval(v.X)
	if recv == nil || v.Index < 0 || v.Index >= len(recv.Elems) {
		return nil
	}
	return recv.Elems[v.Index]
}

// evalIndex evaluates `a[i]` on a constant tuple, array, or text.
func (c *checker) evalIndex(v *ast.IndexExpr) *Value {
	recv := c.eval(v.X)
	idx := c.eval(v.Index)
	if recv == nil || idx == nil || !idx.Type.IsInteger() {
		return nil
	}
	if recv.Type != nil && recv.Type.IsStr() {
		if idx.Int >= uint64(len(recv.Str)) {
			return nil
		}
		return intValue(types.Uint8, uint64(recv.Str[idx.Int]))
	}
	if idx.Int >= uint64(len(recv.Elems)) {
		return nil
	}
	return recv.Elems[idx.Int]
}

// evalUnary evaluates a prefix operator on a constant.
func (c *checker) evalUnary(v *ast.UnaryExpr) *Value {
	x := c.eval(v.X)
	if x == nil || x.Type == nil {
		return nil
	}
	switch v.Op {
	case token.Minus:
		switch {
		case x.Type.IsSigned():
			return intValue(x.Type, uint64(-int64(x.Int)))
		case x.Type.IsFloat():
			return &Value{Type: x.Type, Float: -x.Float}
		case x.Type.Kind == types.KindDuration:
			return intValue(x.Type, uint64(-int64(x.Int)))
		}
	case token.Bang:
		if x.Type.IsBool() {
			return boolValue(!x.Bool)
		}
	case token.Tilde:
		if x.Type.IsInteger() {
			return intValue(x.Type, ^x.Int)
		}
	}
	return nil
}

// evalBinary evaluates a binary operator on two constants.
func (c *checker) evalBinary(op token.Kind, l, r *Value, sp source.Span) *Value {
	if l == nil || r == nil || l.Type == nil || r.Type == nil {
		return nil
	}
	switch {
	case l.Type.IsInteger() || l.Type.IsChar():
		return c.evalIntBinary(op, l, r, sp)
	case l.Type.IsFloat():
		return evalFloatBinary(op, l, r)
	case l.Type.IsStr():
		return evalStrBinary(op, l, r)
	case l.Type.IsBool():
		return evalBoolBinary(op, l, r)
	}
	return nil
}

// evalIntBinary evaluates an operator on two integer or char values.
func (c *checker) evalIntBinary(op token.Kind, l, r *Value, sp source.Span) *Value {
	width := bitWidth(l.Type)
	if op == token.Shl || op == token.Shr {
		if r.Int >= width {
			c.errorHelp(diag.CodeComptimeRequired, sp,
				"shift by less than the width of the type",
				"a shift of %d bits is out of range for %s", r.Int, l.Type)
			return nil
		}
	}
	if l.Type.IsSigned() {
		a, b := int64(l.Int), int64(r.Int)
		switch op {
		case token.Plus:
			return intValue(l.Type, uint64(a+b))
		case token.Minus:
			return intValue(l.Type, uint64(a-b))
		case token.Star:
			return intValue(l.Type, uint64(a*b))
		case token.Slash, token.Percent:
			if b == 0 {
				c.errorHelp(diag.CodeComptimeRequired, sp,
					"guard the divisor", "division by zero in a constant")
				return nil
			}
			if op == token.Slash {
				return intValue(l.Type, uint64(a/b))
			}
			return intValue(l.Type, uint64(a%b))
		case token.Shl:
			return intValue(l.Type, uint64(a<<r.Int))
		case token.Shr:
			return intValue(l.Type, uint64(a>>r.Int))
		case token.Amp:
			return intValue(l.Type, uint64(a&b))
		case token.Caret:
			return intValue(l.Type, uint64(a^b))
		case token.Pipe:
			return intValue(l.Type, uint64(a|b))
		case token.Eq:
			return boolValue(a == b)
		case token.Ne:
			return boolValue(a != b)
		case token.Lt:
			return boolValue(a < b)
		case token.Le:
			return boolValue(a <= b)
		case token.Gt:
			return boolValue(a > b)
		case token.Ge:
			return boolValue(a >= b)
		}
		return nil
	}
	a, b := l.Int, r.Int
	switch op {
	case token.Plus:
		return intValue(l.Type, a+b)
	case token.Minus:
		return intValue(l.Type, a-b)
	case token.Star:
		return intValue(l.Type, a*b)
	case token.Slash, token.Percent:
		if b == 0 {
			c.errorHelp(diag.CodeComptimeRequired, sp,
				"guard the divisor", "division by zero in a constant")
			return nil
		}
		if op == token.Slash {
			return intValue(l.Type, a/b)
		}
		return intValue(l.Type, a%b)
	case token.Shl:
		return intValue(l.Type, a<<r.Int)
	case token.Shr:
		return intValue(l.Type, a>>r.Int)
	case token.Amp:
		return intValue(l.Type, a&b)
	case token.Caret:
		return intValue(l.Type, a^b)
	case token.Pipe:
		return intValue(l.Type, a|b)
	case token.Eq:
		return boolValue(a == b)
	case token.Ne:
		return boolValue(a != b)
	case token.Lt:
		return boolValue(a < b)
	case token.Le:
		return boolValue(a <= b)
	case token.Gt:
		return boolValue(a > b)
	case token.Ge:
		return boolValue(a >= b)
	}
	return nil
}

// evalFloatBinary evaluates an operator on two floating-point values.
func evalFloatBinary(op token.Kind, l, r *Value) *Value {
	a, b := l.Float, r.Float
	switch op {
	case token.Plus:
		return &Value{Type: l.Type, Float: a + b}
	case token.Minus:
		return &Value{Type: l.Type, Float: a - b}
	case token.Star:
		return &Value{Type: l.Type, Float: a * b}
	case token.Slash:
		return &Value{Type: l.Type, Float: a / b}
	case token.Eq:
		return boolValue(a == b)
	case token.Ne:
		return boolValue(a != b)
	case token.Lt:
		return boolValue(a < b)
	case token.Le:
		return boolValue(a <= b)
	case token.Gt:
		return boolValue(a > b)
	case token.Ge:
		return boolValue(a >= b)
	}
	return nil
}

// evalStrBinary evaluates an operator on two text values.
func evalStrBinary(op token.Kind, l, r *Value) *Value {
	switch op {
	case token.Plus:
		return strValue(l.Str + r.Str)
	case token.Eq:
		return boolValue(l.Str == r.Str)
	case token.Ne:
		return boolValue(l.Str != r.Str)
	}
	return nil
}

// evalBoolBinary evaluates an operator on two boolean values.
func evalBoolBinary(op token.Kind, l, r *Value) *Value {
	switch op {
	case token.AndAnd:
		return boolValue(l.Bool && r.Bool)
	case token.OrOr:
		return boolValue(l.Bool || r.Bool)
	case token.Eq:
		return boolValue(l.Bool == r.Bool)
	case token.Ne:
		return boolValue(l.Bool != r.Bool)
	}
	return nil
}

// bitWidth returns the width in bits of an integer or char type.
func bitWidth(t *types.Type) uint64 {
	switch t.Kind {
	case types.KindChar:
		return 32
	case types.KindInt128, types.KindUint128:
		return 128
	case types.KindInt64, types.KindUint64, types.KindUsize, types.KindDuration:
		return 64
	case types.KindInt32, types.KindUint32:
		return 32
	case types.KindInt16, types.KindUint16:
		return 16
	}
	return 8
}

// evalCall evaluates a conversion, a dur constructor, or a meta query.
func (c *checker) evalCall(v *ast.CallExpr) *Value {
	if id, ok := v.Fn.(*ast.Ident); ok {
		if isTypeSymbol(c.symOf(id)) {
			return c.evalConvert(c.out.Nodes[v], v.Args)
		}
	}
	if sel, ok := v.Fn.(*ast.SelectorExpr); ok {
		if id, ok := sel.X.(*ast.Ident); ok {
			if isNamespaceName(id.Name) {
				return c.evalNamespace(sel, v, id.Name)
			}
			if isTypeSymbol(c.symOf(id)) {
				return c.evalVariant(c.out.Nodes[v], sel.Name.Name, v.Args)
			}
		}
		recv := c.eval(sel.X)
		if recv == nil {
			return nil
		}
		args := make([]*Value, 0, len(v.Args))
		for _, a := range v.Args {
			arg := c.eval(a)
			if arg == nil {
				return nil
			}
			args = append(args, arg)
		}
		return c.evalMethod(recv, sel.Name.Name, args)
	}
	return nil
}

// evalConvert evaluates an explicit conversion.
func (c *checker) evalConvert(target *types.Type, args []ast.Expr) *Value {
	if target == nil || len(args) != 1 {
		return nil
	}
	x := c.eval(args[0])
	if x == nil || x.Type == nil {
		return nil
	}
	switch {
	case target.IsInteger() || target.Kind == types.KindChar:
		switch {
		case x.Type.IsInteger() || x.Type.Kind == types.KindChar:
			return intValue(target, x.Int)
		case x.Type.IsFloat():
			return intValue(target, uint64(int64(x.Float)))
		}
	case target.IsFloat():
		switch {
		case x.Type.IsFloat():
			return &Value{Type: target, Float: x.Float}
		case x.Type.IsInteger():
			return &Value{Type: target, Float: float64(int64(x.Int))}
		}
	case target.IsRaw() && x.Type.IsInteger():
		return intValue(target, x.Int)
	case target.IsInteger() && x.Type.IsRaw():
		return intValue(target, x.Int)
	}
	return nil
}

// evalVariant evaluates `Enum.Variant(args)`.
func (c *checker) evalVariant(enum *types.Type, name string, args []ast.Expr) *Value {
	if enum == nil {
		return nil
	}
	d := c.declOf(enum)
	if d == nil {
		return nil
	}
	variant := d.Variant(name)
	if variant == nil {
		return nil
	}
	out := &Value{Type: enum, Str: name}
	for _, a := range args {
		v := c.eval(a)
		if v == nil {
			return nil
		}
		out.Elems = append(out.Elems, v)
	}
	return out
}

// evalNamespace evaluates a dur constructor or a meta query.
func (c *checker) evalNamespace(sel *ast.SelectorExpr, v *ast.CallExpr, ns string) *Value {
	if ns == "dur" {
		if len(v.Args) != 1 {
			return nil
		}
		n := c.eval(v.Args[0])
		if n == nil || !n.Type.IsInteger() {
			return nil
		}
		return intValue(types.Duration, n.Int)
	}
	return c.evalMeta(sel, v)
}

// evalMethod evaluates a built-in method on a constant value, which is how
// a compile-time expression reads a container or an Option.
func (c *checker) evalMethod(recv *Value, name string, args []*Value) *Value {
	if recv.Type == nil {
		return nil
	}
	switch name {
	case "Len":
		if recv.Type.IsStr() {
			return intValue(types.Usize, uint64(len(recv.Str)))
		}
		if recv.Type.Kind == types.KindArray {
			return intValue(types.Usize, recv.Type.Len)
		}
		return intValue(types.Usize, uint64(len(recv.Elems)))
	case "Keys":
		return &Value{Type: types.SliceOf(arg(recv.Type, 0)), Elems: recv.Keys}
	case "Values":
		return &Value{Type: types.SliceOf(arg(recv.Type, 1)), Elems: recv.Elems}
	case "Get":
		if len(args) != 1 {
			return nil
		}
		if recv.Keys != nil {
			for i, k := range recv.Keys {
				if equalValue(k, args[0]) {
					return someValue(recv.Elems[i])
				}
			}
			return noneValue(arg(recv.Type, 1))
		}
		if args[0].Int < uint64(len(recv.Elems)) {
			return someValue(recv.Elems[args[0].Int])
		}
		return noneValue(elementOf(recv.Type))
	case "Contains":
		if len(args) != 1 {
			return nil
		}
		return boolValue(c.contains(recv, args[0]))
	case "IsSome":
		return boolValue(recv.Str == "Some")
	case "IsNone":
		return boolValue(recv.Str == "None")
	case "IsOk":
		return boolValue(recv.Str == "Ok")
	case "IsErr":
		return boolValue(recv.Str == "Err")
	case "Unwrap":
		if len(recv.Elems) == 0 {
			return nil
		}
		return recv.Elems[0]
	case "UnwrapOr":
		if len(recv.Elems) > 0 {
			return recv.Elems[0]
		}
		if len(args) == 1 {
			return args[0]
		}
		return nil
	}
	return nil
}

// contains reports whether a constant container holds a value.
func (c *checker) contains(recv, want *Value) bool {
	if recv.Keys != nil {
		for _, k := range recv.Keys {
			if equalValue(k, want) {
				return true
			}
		}
		return false
	}
	for _, e := range recv.Elems {
		if equalValue(e, want) {
			return true
		}
	}
	return false
}

// equalValue reports whether two constant values are equal.
func equalValue(a, b *Value) bool {
	if a == nil || b == nil || !types.Identical(a.Type, b.Type) {
		return false
	}
	switch {
	case a.Type.IsInteger() || a.Type.IsChar():
		return a.Int == b.Int
	case a.Type.IsFloat():
		return a.Float == b.Float
	case a.Type.IsBool():
		return a.Bool == b.Bool
	case a.Type.IsStr():
		return a.Str == b.Str
	}
	return false
}

// someValue returns an Option holding v.
func someValue(v *Value) *Value {
	return &Value{Type: types.Option.Instantiate(v.Type), Str: "Some", Elems: []*Value{v}}
}

// noneValue returns an empty Option of an element type.
func noneValue(elem *types.Type) *Value {
	if elem == nil {
		return &Value{Type: types.Option, Str: "None"}
	}
	return &Value{Type: types.Option.Instantiate(elem), Str: "None"}
}

// elementOf returns the element type of a container or a view.
func elementOf(t *types.Type) *types.Type {
	if t == nil {
		return nil
	}
	switch t.Kind {
	case types.KindArray, types.KindSlice:
		return t.Elem
	}
	return arg(t, 0)
}

// evalMeta evaluates a query of the meta namespace. The layout queries run
// the same algorithm the backend uses, so the two agree by construction.
func (c *checker) evalMeta(sel *ast.SelectorExpr, v *ast.CallExpr) *Value {
	switch sel.Name.Name {
	case "typeof":
		if len(v.Args) != 1 {
			return nil
		}
		t := c.out.Nodes[v.Args[0]]
		if t == nil {
			return nil
		}
		return &Value{Type: types.TypeValue, TypeVal: t}
	case "name":
		t := c.argType(v, 0)
		if t == nil {
			return nil
		}
		return strValue(t.String())
	case "target":
		return strValue(c.target.Triple)
	case "size_of":
		return c.metaSize(v, func(t *types.Type) (uint64, error) {
			return layout.Size(t, c.provider(), c.layoutTarget())
		})
	case "align_of":
		return c.metaSize(v, func(t *types.Type) (uint64, error) {
			return layout.Align(t, c.provider(), c.layoutTarget())
		})
	case "field_count":
		return c.metaSize(v, func(t *types.Type) (uint64, error) {
			placed, err := layout.Fields(t, c.provider(), c.layoutTarget())
			return uint64(len(placed)), err
		})
	case "field_name":
		return c.metaFieldName(v)
	case "field_type":
		return c.metaFieldType(v)
	case "offset_of":
		return c.metaOffset(v)
	case "enum_count":
		d := c.declOf(c.argType(v, 0))
		if d == nil {
			return nil
		}
		return intValue(types.Usize, uint64(len(d.Variants)))
	case "enum_name":
		if len(v.Args) != 1 {
			return nil
		}
		val := c.eval(v.Args[0])
		if val == nil {
			return nil
		}
		return strValue(val.Str)
	case "has_field":
		return c.metaHasField(v)
	case "implements":
		if len(v.Args) != 2 {
			return nil
		}
		return boolValue(c.implements(c.argType(v, 0), c.argType(v, 1)))
	case "attr":
		return c.metaAttr(v)
	}
	return nil
}

// argType returns the type an argument of a meta query names.
func (c *checker) argType(v *ast.CallExpr, i int) *types.Type {
	if i >= len(v.Args) {
		return nil
	}
	return c.buildType(typeExprOf(v.Args[i]))
}

// metaSize evaluates a layout query that yields a size or a count.
func (c *checker) metaSize(v *ast.CallExpr, query func(*types.Type) (uint64, error)) *Value {
	t := c.argType(v, 0)
	if t == nil {
		return nil
	}
	n, err := query(t)
	if err != nil {
		c.reportLayout(err, v)
		return nil
	}
	return intValue(types.Usize, n)
}

// metaFieldName evaluates `meta.field_name(T, i)`, which names the i-th
// field in the final layout order.
func (c *checker) metaFieldName(v *ast.CallExpr) *Value {
	if len(v.Args) != 2 {
		return nil
	}
	t := c.argType(v, 0)
	i := c.eval(v.Args[1])
	if t == nil || i == nil || !i.Type.IsInteger() {
		return nil
	}
	placed, err := layout.Fields(t, c.provider(), c.layoutTarget())
	if err != nil {
		c.reportLayout(err, v)
		return nil
	}
	if i.Int >= uint64(len(placed)) {
		c.errorHelp(diag.CodeMetaName, v.Args[1].Span(),
			"use a position the type has",
			"%s has %d fields", t, len(placed))
		return nil
	}
	return strValue(placed[i.Int].Name)
}

// metaFieldType evaluates `meta.field_type(T, i)`.
func (c *checker) metaFieldType(v *ast.CallExpr) *Value {
	if len(v.Args) != 2 {
		return nil
	}
	t := c.argType(v, 0)
	i := c.eval(v.Args[1])
	if t == nil || i == nil || !i.Type.IsInteger() {
		return nil
	}
	placed, err := layout.Fields(t, c.provider(), c.layoutTarget())
	if err != nil {
		c.reportLayout(err, v)
		return nil
	}
	if i.Int >= uint64(len(placed)) {
		c.errorHelp(diag.CodeMetaName, v.Args[1].Span(),
			"use a position the type has",
			"%s has %d fields", t, len(placed))
		return nil
	}
	return &Value{Type: types.TypeValue, TypeVal: placed[i.Int].Type}
}

// metaOffset evaluates `meta.offset_of(T, "field")`.
func (c *checker) metaOffset(v *ast.CallExpr) *Value {
	if len(v.Args) != 2 {
		return nil
	}
	t := c.argType(v, 0)
	name := c.eval(v.Args[1])
	if t == nil || name == nil || !name.Type.IsStr() {
		return nil
	}
	off, err := layout.Offset(t, name.Str, c.provider(), c.layoutTarget())
	if err != nil {
		c.reportLayout(err, v)
		return nil
	}
	return intValue(types.Usize, off)
}

// metaHasField evaluates `meta.has_field(T, "field")`.
func (c *checker) metaHasField(v *ast.CallExpr) *Value {
	if len(v.Args) != 2 {
		return nil
	}
	t := c.argType(v, 0)
	name := c.eval(v.Args[1])
	if t == nil || name == nil || !name.Type.IsStr() {
		return nil
	}
	d := c.declOf(t)
	if d == nil {
		return nil
	}
	return boolValue(d.Field(name.Str) != nil)
}

// metaAttr evaluates `meta.attr(T, "name")`, which reads a constant
// declared in the type body.
func (c *checker) metaAttr(v *ast.CallExpr) *Value {
	if len(v.Args) != 2 {
		return nil
	}
	t := c.argType(v, 0)
	name := c.eval(v.Args[1])
	if t == nil || name == nil || !name.Type.IsStr() {
		return nil
	}
	d := c.declOf(t)
	if d == nil {
		return nil
	}
	con, ok := d.Consts[name.Str]
	if !ok {
		c.errorHelp(diag.CodeMetaName, v.Args[1].Span(),
			"check the attribute name",
			"%s has no attribute %q", t, name.Str)
		return nil
	}
	return c.constValue(con.Sym)
}

// reportLayout reports a layout query that failed. A query that depends on
// a type parameter is not an error: the backend settles it once the type is
// instantiated, and the node keeps a mono-rule:layout.
func (c *checker) reportLayout(err error, v *ast.CallExpr) {
	if errors.Is(err, layout.ErrUnknown) {
		return
	}
	c.errorHelp(diag.CodeLayout, v.Sp, "break the cycle of value types", "%v", err)
}

// layoutTarget returns the layout properties of the target.
func (c *checker) layoutTarget() layout.Target { return c.target.Layout() }

// provider returns the layout provider of this package.
func (c *checker) provider() layout.Provider { return layoutProvider{c: c} }

// layoutProvider adapts the checker's declarations to the layout package.
type layoutProvider struct {
	c *checker
}

// Fields returns the fields of a named struct, extern struct, or union.
func (p layoutProvider) Fields(t *types.Type) ([]layout.Field, bool) {
	d := p.c.declOf(t)
	if d == nil {
		return nil, false
	}
	out := make([]layout.Field, 0, len(d.Fields))
	for _, f := range d.Fields {
		out = append(out, layout.Field{Name: f.Name, Type: f.Type})
	}
	return out, true
}

// Variants returns the variants of a named enum.
func (p layoutProvider) Variants(t *types.Type) ([]layout.Variant, bool) {
	d := p.c.declOf(t)
	if d == nil {
		return nil, false
	}
	out := make([]layout.Variant, 0, len(d.Variants))
	for _, v := range d.Variants {
		out = append(out, layout.Variant{Name: v.Name, Payload: v.Payload})
	}
	return out, true
}

// requireConst evaluates an expression that must be a compile-time
// constant and reports one that is not.
func (c *checker) requireConst(e ast.Expr, what string) *Value {
	v := c.eval(e)
	if v == nil {
		c.errorHelp(diag.CodeComptimeRequired, spanOf(e),
			"use a literal, a constant, or a compile-time operation",
			"%s must be a compile-time constant", what)
	}
	return v
}
