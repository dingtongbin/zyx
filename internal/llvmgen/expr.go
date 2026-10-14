// Expression lowering.

package llvmgen

import (
	"fmt"
	"strconv"
	"strings"

	"zyx/internal/layout"
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// A value is one emitted operand together with its language type. An lvalue
// names the storage of a variable; an rvalue carries the value itself. A
// value whose type lives at an address, such as a reference struct or a
// fixed-size array, carries the address of its object.
type value struct {
	text   string
	ll     string
	typ    *types.Type
	lvalue bool
}

// load returns the value of an lvalue.
func (g *generator) load(v *value) *value {
	if v == nil || !v.lvalue {
		return v
	}
	reg := g.newReg()
	if byAddress(v.typ) {
		g.emit("  %s = load ptr, ptr %s", reg, v.text)
		return &value{text: reg, ll: "ptr", typ: v.typ}
	}
	g.emit("  %s = load %s, ptr %s", reg, v.ll, v.text)
	return &value{text: reg, ll: v.ll, typ: v.typ}
}

// addrText returns the address of a value, materializing one when the value
// lives in a register.
func (g *generator) addrText(v *value) string {
	if v.lvalue {
		return v.text
	}
	if byAddress(v.typ) {
		return v.text
	}
	slot := g.alloca(v.typ)
	g.emit("  store %s %s, ptr %s", v.ll, v.text, slot)
	return slot
}

// storeInto writes a value into storage.
func (g *generator) storeInto(addr string, typ *types.Type, v *value) {
	if byAddress(typ) {
		// The object is shared: the destination keeps a reference to it.
		g.emit("  store ptr %s, ptr %s", g.addrText(v), addr)
		return
	}
	r := g.load(v)
	g.emit("  store %s %s, ptr %s", r.ll, r.text, addr)
}

// toBool converts a value into the i1 a branch takes.
func (g *generator) toBool(v *value) string {
	r := g.load(v)
	if r.ll == "i1" {
		return r.text
	}
	reg := g.newReg()
	g.emit("  %s = icmp ne %s %s, 0", reg, r.ll, r.text)
	return reg
}

// expr lowers one expression. want is the type the context expects, which
// settles the type of a literal and of an untyped aggregate; it may be nil.
func (g *generator) expr(module string, n *zyxc.Node, want *types.Type) *value {
	if n == nil {
		g.fail(fmt.Errorf("llvmgen: a missing expression"))
		return nil
	}
	switch n.Kind {
	case "name":
		return g.name(module, n)
	case "lit":
		return g.literal(module, n, want)
	case "binary":
		return g.binary(module, n)
	case "unary":
		return g.unary(module, n)
	case "convert":
		return g.convert(module, n)
	case "field":
		return g.field(module, n)
	case "index":
		return g.index(module, n)
	case "tindex":
		return g.tupleIndex(module, n)
	case "struct-lit":
		return g.structLit(module, n)
	case "array-lit":
		return g.arrayLit(module, n)
	case "tuple-lit":
		return g.tupleLit(module, n)
	case "call":
		return g.call(module, n)
	case "mcall":
		return g.methodCall(module, n)
	case "qual":
		return g.qualified(module, n)
	case "c-call":
		return g.cCall(module, n)
	case "func-ref":
		return g.funcRef(n)
	case "addr":
		return g.addressOf(module, n)
	case "array-repeat":
		return g.arrayRepeat(module, n)
	case "spawn":
		return g.spawn(module, n)
	}
	g.fail(fmt.Errorf("llvmgen: expression %s is not lowered", n.Kind))
	return nil
}

// name lowers a reference to a local, a global, or a function.
func (g *generator) name(module string, n *zyxc.Node) *value {
	if len(n.Payload) < 2 {
		g.fail(fmt.Errorf("llvmgen: a name needs an identifier"))
		return nil
	}
	id := n.Payload[1]
	if slot, ok := g.fn.vars[id]; ok {
		return &value{text: slot.alloca, ll: g.llPass(slot.typ), typ: slot.typ, lvalue: true}
	}
	if decl := g.tab.decl(id); decl != nil {
		typ := g.bindingType(module, decl, id)
		if typ == nil {
			g.fail(fmt.Errorf("llvmgen: the type of %s is not settled", n.Payload[0]))
			return nil
		}
		return &value{text: "@" + llvmName(id), ll: g.llType(typ), typ: typ, lvalue: true}
	}
	g.fail(fmt.Errorf("llvmgen: %s is not bound", n.Payload[0]))
	return nil
}

// bindingType returns the declared type of a top-level binding.
func (g *generator) bindingType(module string, decl *zyxc.Node, id string) *types.Type {
	if decl.Kind == "func" {
		return nil
	}
	if t := g.typeOfNode(decl.First("type"), g.tab.module(id)); t != nil {
		return t
	}
	if decl.Kind == "var" {
		// The initializer settles the type of an unannotated binding.
		init := g.initChild(decl)
		if init != nil {
			v := g.constType(g.tab.module(id), init)
			if v != nil {
				return v
			}
		}
	}
	return nil
}

// constType infers the type of a constant initializer without emitting it.
func (g *generator) constType(module string, n *zyxc.Node) *types.Type {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case "lit":
		return g.literalType(n)
	case "unary":
		return g.constType(module, g.wrapped(n, "operand"))
	case "binary":
		return g.constType(module, g.wrapped(n, "lhs"))
	case "name":
		if len(n.Payload) >= 2 {
			if slot, ok := g.fn.vars[n.Payload[1]]; ok {
				return slot.typ
			}
			if decl := g.tab.decl(n.Payload[1]); decl != nil {
				return g.bindingType(module, decl, n.Payload[1])
			}
		}
	case "struct-lit":
		if len(n.Payload) > 0 {
			t, err := g.tab.parse(module, n.Payload[0])
			if err == nil {
				return t
			}
		}
	}
	return nil
}

// literalType returns the type a literal carries with no context.
func (g *generator) literalType(n *zyxc.Node) *types.Type {
	if len(n.Payload) == 0 {
		return nil
	}
	text := n.Payload[0]
	if text == "true" || text == "false" {
		return types.Bool
	}
	if _, err := parseIntLiteral(text); err == nil {
		return types.Int64
	}
	if _, err := strconv.ParseFloat(text, 64); err == nil {
		return types.Float64
	}
	return types.Str
}

// literal lowers a literal.
func (g *generator) literal(module string, n *zyxc.Node, want *types.Type) *value {
	if len(n.Payload) == 0 {
		g.fail(fmt.Errorf("llvmgen: a literal needs a value"))
		return nil
	}
	if len(n.Children) != 0 {
		g.fail(fmt.Errorf("llvmgen: an interpolated string is not lowered"))
		return nil
	}
	text := n.Payload[0]
	typ := want
	if typ == nil {
		typ = g.literalType(n)
	}
	if typ == nil {
		g.fail(fmt.Errorf("llvmgen: the type of literal %q is not settled", text))
		return nil
	}
	switch {
	case typ.IsBool():
		v := "0"
		if text == "true" {
			v = "1"
		}
		return &value{text: v, ll: "i8", typ: typ}
	case typ.IsStr():
		global := g.m.stringGlobal(text)
		ptr := g.newReg()
		g.emit("  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 0", ptr, len(text), global)
		lenReg := g.newReg()
		g.emit("  %s = insertvalue { ptr, %s } undef, ptr %s, 0", lenReg, g.ptrInt(), ptr)
		out := g.newReg()
		g.emit("  %s = insertvalue { ptr, %s } %s, %s %d, 1", out, g.ptrInt(), lenReg, g.ptrInt(), len(text))
		return &value{text: out, ll: "{ ptr, " + g.ptrInt() + " }", typ: typ}
	case typ.IsChar():
		runes := []rune(text)
		if len(runes) != 1 {
			g.fail(fmt.Errorf("llvmgen: %q is not one character", text))
			return nil
		}
		return &value{text: strconv.FormatInt(int64(runes[0]), 10), ll: "i32", typ: typ}
	case typ.IsInteger():
		v, err := parseIntLiteral(text)
		if err != nil {
			g.fail(fmt.Errorf("llvmgen: %q is not an integer", text))
			return nil
		}
		return &value{text: strconv.FormatUint(v, 10), ll: g.llType(typ), typ: typ}
	case typ.IsFloat():
		v, err := strconv.ParseFloat(text, 64)
		if err != nil {
			g.fail(fmt.Errorf("llvmgen: %q is not a number", text))
			return nil
		}
		ll := g.llType(typ)
		text := strconv.FormatFloat(v, 'g', -1, 64)
		if !strings.ContainsAny(text, ".eEni") {
			text += ".0"
		}
		return &value{text: text, ll: ll, typ: typ}
	}
	g.fail(fmt.Errorf("llvmgen: a %s literal is not lowered", typ))
	return nil
}

// parseIntLiteral parses the source text of an integer literal.
func parseIntLiteral(text string) (uint64, error) {
	clean := strings.ReplaceAll(text, "_", "")
	base := 10
	if len(clean) > 2 && clean[0] == '0' {
		switch clean[1] {
		case 'x', 'X':
			base, clean = 16, clean[2:]
		case 'b', 'B':
			base, clean = 2, clean[2:]
		case 'o', 'O':
			base, clean = 8, clean[2:]
		}
	}
	return strconv.ParseUint(clean, base, 64)
}

// binary lowers an operator application.
func (g *generator) binary(module string, n *zyxc.Node) *value {
	op := ""
	if len(n.Payload) > 0 {
		op = n.Payload[0]
	}
	lhsNode := g.wrapped(n, "lhs")
	if op == "&&" || op == "||" {
		return g.logical(module, op, lhsNode, g.wrapped(n, "rhs"))
	}
	lhs := g.expr(module, lhsNode, nil)
	rhs := g.expr(module, g.wrapped(n, "rhs"), nil)
	if lhs == nil || rhs == nil {
		return nil
	}
	if isComparison(op) {
		return g.compare(op, lhs, rhs)
	}
	return g.arith(op, lhs, rhs)
}

// logical lowers the short-circuit operators.
func (g *generator) logical(module, op string, lhsNode, rhsNode *zyxc.Node) *value {
	slot := g.alloca(types.Bool)
	lhs := g.expr(module, lhsNode, types.Bool)
	if lhs == nil {
		return nil
	}
	rhsLabel := g.newLabel("logical.rhs")
	shortLabel := g.newLabel("logical.short")
	endLabel := g.newLabel("logical.end")
	short := "0"
	if op == "||" {
		short = "1"
	}
	if op == "&&" {
		g.terminator("  br i1 %s, label %%%s, label %%%s", g.toBool(lhs), rhsLabel, shortLabel)
	} else {
		g.terminator("  br i1 %s, label %%%s, label %%%s", g.toBool(lhs), shortLabel, rhsLabel)
	}
	// The left operand settled the answer.
	g.label(shortLabel)
	g.emit("  store i8 %s, ptr %s", short, slot)
	g.terminator("  br label %%%s", endLabel)

	g.label(rhsLabel)
	rhs := g.expr(module, rhsNode, types.Bool)
	if rhs != nil {
		reg := g.newReg()
		g.emit("  %s = zext i1 %s to i8", reg, g.toBool(rhs))
		g.emit("  store i8 %s, ptr %s", reg, slot)
	}
	g.terminator("  br label %%%s", endLabel)

	g.label(endLabel)
	return &value{text: slot, ll: "i8", typ: types.Bool, lvalue: true}
}

// isComparison reports whether an operator yields a boolean.
func isComparison(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	}
	return false
}

// compare lowers a comparison.
func (g *generator) compare(op string, a, b *value) *value {
	a, b = g.load(a), g.load(b)
	if a.typ.IsStr() || b.typ.IsStr() {
		g.fail(fmt.Errorf("llvmgen: comparing strings is not lowered"))
		return nil
	}
	pred := map[string]string{
		"==": "eq", "!=": "ne", "<": "lt", "<=": "le", ">": "gt", ">=": "ge",
	}[op]
	ll := a.ll
	var instr string
	switch {
	case a.typ.IsFloat():
		instr = "fcmp o" + pred
	case pred == "eq" || pred == "ne":
		// Equality carries no signedness.
		instr = "icmp " + pred
	case a.typ.IsSigned() || a.typ.IsChar():
		instr = "icmp s" + pred
	default:
		instr = "icmp u" + pred
	}
	reg := g.newReg()
	g.emit("  %s = %s %s %s, %s", reg, instr, ll, a.text, b.text)
	return &value{text: reg, ll: "i1", typ: types.Bool}
}

// arith lowers an arithmetic or bitwise operator.
func (g *generator) arith(op string, a, b *value) *value {
	a, b = g.load(a), g.load(b)
	if a == nil || b == nil {
		return nil
	}
	typ := a.typ
	ll := a.ll
	var instr string
	switch {
	case typ.IsFloat():
		switch op {
		case "+":
			instr = "fadd"
		case "-":
			instr = "fsub"
		case "*":
			instr = "fmul"
		case "/":
			instr = "fdiv"
		case "%":
			instr = "frem"
		default:
			g.fail(fmt.Errorf("llvmgen: %s is not a floating-point operator", op))
			return nil
		}
	case op == "/" || op == "%":
		if typ.IsSigned() {
			instr = "s" + map[string]string{"/": "div", "%": "rem"}[op]
		} else {
			instr = "u" + map[string]string{"/": "div", "%": "rem"}[op]
		}
	default:
		instr = map[string]string{
			"+": "add", "-": "sub", "*": "mul", "&": "and", "|": "or", "^": "xor",
		}[op]
		if op == "<<" {
			instr = "shl"
		}
		if op == ">>" {
			if typ.IsSigned() {
				instr = "ashr"
			} else {
				instr = "lshr"
			}
		}
	}
	if instr == "" {
		g.fail(fmt.Errorf("llvmgen: %s is not lowered", op))
		return nil
	}
	reg := g.newReg()
	g.emit("  %s = %s %s %s, %s", reg, instr, ll, a.text, b.text)
	return &value{text: reg, ll: ll, typ: typ}
}

// unary lowers a prefix operator.
func (g *generator) unary(module string, n *zyxc.Node) *value {
	op := ""
	if len(n.Payload) > 0 {
		op = n.Payload[0]
	}
	v := g.load(g.expr(module, g.wrapped(n, "operand"), nil))
	if v == nil {
		return nil
	}
	reg := g.newReg()
	switch op {
	case "-":
		if v.typ.IsFloat() {
			g.emit("  %s = fneg %s %s", reg, v.ll, v.text)
		} else {
			g.emit("  %s = sub %s 0, %s", reg, v.ll, v.text)
		}
	case "!":
		g.emit("  %s = xor i8 %s, 1", reg, v.text)
		return &value{text: reg, ll: "i8", typ: types.Bool}
	case "~":
		g.emit("  %s = xor %s %s, -1", reg, v.ll, v.text)
	default:
		g.fail(fmt.Errorf("llvmgen: %s is not lowered", op))
		return nil
	}
	return &value{text: reg, ll: v.ll, typ: v.typ}
}

// convert lowers a numeric conversion.
func (g *generator) convert(module string, n *zyxc.Node) *value {
	if len(n.Payload) == 0 {
		g.fail(fmt.Errorf("llvmgen: a conversion needs a target type"))
		return nil
	}
	dst, err := g.tab.parse(module, n.Payload[0])
	if err != nil {
		g.fail(err)
		return nil
	}
	src := g.load(g.expr(module, g.wrapped(n, "operand"), nil))
	if src == nil {
		return nil
	}
	return g.cast(src, dst)
}

// cast converts a value to another scalar type.
func (g *generator) cast(src *value, dst *types.Type) *value {
	if types.Identical(src.typ, dst) {
		return src
	}
	from, to := g.llType(src.typ), g.llType(dst)
	if from == to {
		return &value{text: src.text, ll: to, typ: dst}
	}
	reg := g.newReg()
	switch {
	case src.typ.IsFloat() && dst.IsFloat():
		op := "fpext"
		if llSize(to) < llSize(from) {
			op = "fptrunc"
		}
		g.emit("  %s = %s %s %s to %s", reg, op, from, src.text, to)
	case src.typ.IsFloat():
		g.emit("  %s = fptosi %s %s to %s", reg, from, src.text, to)
	case dst.IsFloat():
		g.emit("  %s = sitofp %s %s to %s", reg, from, src.text, to)
	case llSize(to) < llSize(from):
		g.emit("  %s = trunc %s %s to %s", reg, from, src.text, to)
	case src.typ.IsSigned() || src.typ.IsChar():
		g.emit("  %s = sext %s %s to %s", reg, from, src.text, to)
	default:
		g.emit("  %s = zext %s %s to %s", reg, from, src.text, to)
	}
	return &value{text: reg, ll: to, typ: dst}
}

// llSize returns the bit width of a scalar LLVM type.
func llSize(ll string) int {
	if strings.HasPrefix(ll, "i") {
		n, err := strconv.Atoi(ll[1:])
		if err == nil {
			return n
		}
	}
	switch ll {
	case "float":
		return 32
	case "double":
		return 64
	case "ptr":
		return 64
	}
	return 0
}

// field lowers a field access.
func (g *generator) field(module string, n *zyxc.Node) *value {
	recv := g.expr(module, g.wrapped(n, "recv"), nil)
	if recv == nil {
		return nil
	}
	recv = g.load(recv)
	typ := recv.typ
	base := recv.text
	if alias := g.tab.Alias(typ); alias != nil {
		typ = alias
	}
	name := ""
	if len(n.Payload) > 0 {
		name = n.Payload[0]
	}
	placed, err := layout.Fields(typ, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("llvmgen: the layout of %s: %w", typ, err))
		return nil
	}
	for i, f := range placed {
		if f.Name != name {
			continue
		}
		reg := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, i32 0, i32 %d", reg, g.llType(typ), base, i)
		return &value{text: reg, ll: g.llPass(f.Type), typ: f.Type, lvalue: true}
	}
	g.fail(fmt.Errorf("llvmgen: %s has no field %s", typ, name))
	return nil
}

// index lowers an element access.
func (g *generator) index(module string, n *zyxc.Node) *value {
	recv := g.load(g.expr(module, g.wrapped(n, "recv"), nil))
	idx := g.load(g.expr(module, g.wrapped(n, "idx"), types.Usize))
	if recv == nil || idx == nil {
		return nil
	}
	switch recv.typ.Kind {
	case types.KindArray:
		reg := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, i64 0, i64 %s",
			reg, g.llType(recv.typ), recv.text, idx.text)
		return &value{text: reg, ll: g.llPass(recv.typ.Elem), typ: recv.typ.Elem, lvalue: true}
	case types.KindSlice:
		ptrReg := g.newReg()
		g.emit("  %s = extractvalue %s %s, 0", ptrReg, recv.ll, recv.text)
		reg := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, i64 %s",
			reg, g.llType(recv.typ.Elem), ptrReg, idx.text)
		return &value{text: reg, ll: g.llPass(recv.typ.Elem), typ: recv.typ.Elem, lvalue: true}
	}
	g.fail(fmt.Errorf("llvmgen: indexing %s is not lowered", recv.typ))
	return nil
}

// tupleIndex lowers a tuple element access.
func (g *generator) tupleIndex(module string, n *zyxc.Node) *value {
	recv := g.load(g.expr(module, g.wrapped(n, "recv"), nil))
	if recv == nil {
		return nil
	}
	if recv.typ.Kind != types.KindTuple {
		g.fail(fmt.Errorf("llvmgen: %s is not a tuple", recv.typ))
		return nil
	}
	index, err := strconv.Atoi(n.Payload[0])
	if err != nil || index < 0 || index >= len(recv.typ.Elems) {
		g.fail(fmt.Errorf("llvmgen: tuple index %s is out of range", n.Payload[0]))
		return nil
	}
	reg := g.newReg()
	g.emit("  %s = extractvalue %s %s, %d", reg, g.llType(recv.typ), recv.text, index)
	return &value{text: reg, ll: g.llPass(recv.typ.Elems[index]), typ: recv.typ.Elems[index]}
}

// structLit lowers a struct construction. The object is created in the
// region of the running coroutine, so a reference to it outlives the
// function that made it.
func (g *generator) structLit(module string, n *zyxc.Node) *value {
	if len(n.Payload) == 0 {
		g.fail(fmt.Errorf("llvmgen: a struct literal needs a type"))
		return nil
	}
	typ, err := g.tab.parse(module, n.Payload[0])
	if err != nil {
		g.fail(err)
		return nil
	}
	base := g.allocate(typ)
	placed, err := layout.Fields(typ, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("llvmgen: the layout of %s: %w", typ, err))
		return nil
	}
	for _, child := range n.Children {
		if child.Kind != "field-init" {
			continue
		}
		name := ""
		if len(child.Payload) > 0 {
			name = child.Payload[0]
		}
		init := g.initChild(child)
		index, fieldType := -1, (*types.Type)(nil)
		for i, f := range placed {
			if f.Name == name {
				index, fieldType = i, f.Type
			}
		}
		if index < 0 {
			g.fail(fmt.Errorf("llvmgen: %s has no field %s", typ, name))
			return nil
		}
		reg := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, i32 0, i32 %d", reg, g.llType(typ), base, index)
		v := g.expr(module, init, fieldType)
		if v == nil {
			return nil
		}
		g.storeInto(reg, fieldType, v)
	}
	return &value{text: base, ll: "ptr", typ: typ}
}

// arrayLit lowers an array construction.
func (g *generator) arrayLit(module string, n *zyxc.Node) *value {
	elem := (*types.Type)(nil)
	var nodes []*zyxc.Node
	for _, child := range n.Children {
		if child.Kind == "elem" {
			nodes = append(nodes, g.sole(child))
		}
	}
	typ := types.ArrayOf(types.Int64, uint64(len(nodes)))
	base := g.allocate(typ)
	for i, node := range nodes {
		v := g.expr(module, node, elem)
		if v == nil {
			return nil
		}
		if elem == nil {
			elem = v.typ
			typ = types.ArrayOf(elem, uint64(len(nodes)))
			base = g.allocate(typ)
		}
		reg := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, i64 0, i64 %d", reg, g.llType(typ), base, i)
		g.storeInto(reg, elem, v)
	}
	if elem == nil {
		g.fail(fmt.Errorf("llvmgen: an empty array literal has no element type"))
		return nil
	}
	return &value{text: base, ll: "ptr", typ: typ}
}

// arrayRepeat lowers the `[value] * count` form.
func (g *generator) arrayRepeat(module string, n *zyxc.Node) *value {
	g.fail(fmt.Errorf("llvmgen: a repeated array is not lowered"))
	return nil
}

// tupleLit lowers a tuple construction.
func (g *generator) tupleLit(module string, n *zyxc.Node) *value {
	var nodes []*zyxc.Node
	for _, child := range n.Children {
		if child.Kind == "elem" {
			nodes = append(nodes, g.sole(child))
		}
	}
	vals := make([]*value, 0, len(nodes))
	elems := make([]*types.Type, 0, len(nodes))
	for _, node := range nodes {
		v := g.expr(module, node, nil)
		if v == nil {
			return nil
		}
		vals = append(vals, g.load(v))
		elems = append(elems, vals[len(vals)-1].typ)
	}
	typ := types.TupleOf(elems...)
	ll := g.llType(typ)
	reg := "undef"
	for i, v := range vals {
		next := g.newReg()
		g.emit("  %s = insertvalue %s %s, %s %s, %d", next, ll, reg, v.ll, v.text, i)
		reg = next
	}
	return &value{text: reg, ll: ll, typ: typ}
}

// addressOf lowers `&x`, which only appears as a C call argument.
func (g *generator) addressOf(module string, n *zyxc.Node) *value {
	v := g.expr(module, g.wrapped(n, "operand"), nil)
	if v == nil {
		return nil
	}
	return &value{text: g.addrText(v), ll: "ptr", typ: types.Raw}
}

// funcRef lowers a reference to a function.
func (g *generator) funcRef(n *zyxc.Node) *value {
	if len(n.Payload) == 0 {
		g.fail(fmt.Errorf("llvmgen: a function reference needs a symbol"))
		return nil
	}
	sym := n.Payload[0]
	decl := g.tab.decl(sym)
	if decl == nil {
		g.fail(fmt.Errorf("llvmgen: no declaration for %s", sym))
		return nil
	}
	typ := g.funcType(decl)
	return &value{text: "@" + llvmName(sym), ll: "ptr", typ: typ}
}
