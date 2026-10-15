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
//
// inline marks an lvalue whose storage is the object itself rather than a
// reference to it: the field of a struct and the element of an array hold
// their object, while a local of the same type holds a reference.
type value struct {
	text   string
	ll     string
	typ    *types.Type
	lvalue bool
	inline bool
}

// load returns the value of an lvalue.
func (g *generator) load(v *value) *value {
	if v == nil || !v.lvalue {
		return v
	}
	if byAddress(v.typ) {
		if v.inline {
			// The storage is the object, so its address is the value.
			return &value{text: v.text, ll: "ptr", typ: v.typ}
		}
		reg := g.newReg()
		g.emit("  %s = load ptr, ptr %s", reg, v.text)
		return &value{text: reg, ll: "ptr", typ: v.typ}
	}
	reg := g.newReg()
	g.emit("  %s = load %s, ptr %s", reg, v.ll, v.text)
	return &value{text: reg, ll: v.ll, typ: v.typ}
}

// addrText returns the address of the object a value names, materializing
// one when the value lives in a register.
func (g *generator) addrText(v *value) string {
	if v.lvalue {
		if byAddress(v.typ) && !v.inline {
			// The storage holds a reference to the object.
			reg := g.newReg()
			g.emit("  %s = load ptr, ptr %s", reg, v.text)
			return reg
		}
		return v.text
	}
	if byAddress(v.typ) {
		return v.text
	}
	slot := g.alloca(v.typ)
	g.emit("  store %s %s, ptr %s", v.ll, v.text, slot)
	return slot
}

// storeInto writes a value into storage. inline marks a destination whose
// storage is the object itself, which a reference type copies rather than
// points at.
func (g *generator) storeInto(addr string, typ *types.Type, v *value, inline bool) {
	if byAddress(typ) {
		if inline {
			g.copyObject(addr, g.addrText(v), typ)
			return
		}
		// The object is shared: the destination keeps a reference to it.
		g.emit("  store ptr %s, ptr %s", g.addrText(v), addr)
		return
	}
	r := g.load(v)
	g.emit("  store %s %s, ptr %s", r.ll, r.text, addr)
}

// copyObject copies one object onto another, which is what assigning a
// reference type into inline storage does.
func (g *generator) copyObject(dst, src string, typ *types.Type) {
	size, err := layout.Size(typ, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("llvmgen: the size of %s: %w", typ, err))
		return
	}
	intrinsic := fmt.Sprintf("llvm.memcpy.p0.p0.%s", g.ptrInt())
	g.declareRuntime(intrinsic, "void", "ptr", "ptr", g.ptrInt(), "i1")
	g.emit("  call void @%s(ptr %s, ptr %s, %s %d, i1 false)", intrinsic, dst, src, g.ptrInt(), size)
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

// expr lowers one expression. The tree records the static type of every
// expression, so the annotation wins over want, which only settles a node
// the backend synthesized itself.
func (g *generator) expr(module string, n *zyxc.Node, want *types.Type) *value {
	if n == nil {
		g.fail(fmt.Errorf("llvmgen: a missing expression"))
		return nil
	}
	if t := g.typeOfNode(n.First("type"), module); t != nil {
		want = t
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
	case "variant-lit":
		return g.variantLit(module, n)
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
	case "meta":
		return g.metaCall(module, n)
	case "dur":
		return g.durCall(module, n)
	case "panic":
		return g.panicCall(module, n)
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

// bindingType returns the declared type of a top-level binding. The tree
// annotates every binding, so no initializer is ever inspected.
func (g *generator) bindingType(module string, decl *zyxc.Node, id string) *types.Type {
	if decl.Kind == "func" {
		return nil
	}
	return g.typeOfNode(decl.First("type"), g.tab.module(id))
}

// parts returns the number of children of a node other than its type
// annotation.
func (g *generator) parts(n *zyxc.Node) int {
	count := 0
	for _, child := range n.Children {
		if child.Kind != "type" {
			count++
		}
	}
	return count
}

// literal lowers a literal. The kind the format wrote settles which literal
// form it is; the annotated type settles only the width of a number.
func (g *generator) literal(module string, n *zyxc.Node, want *types.Type) *value {
	if len(n.Payload) < 2 {
		g.fail(fmt.Errorf("llvmgen: a literal needs a kind and a value"))
		return nil
	}
	kind, text := n.Payload[0], n.Payload[1]
	if kind == "fstring" {
		g.fail(fmt.Errorf("llvmgen: an interpolated string is not lowered"))
		return nil
	}
	typ := want
	if typ == nil {
		g.fail(fmt.Errorf("llvmgen: the type of literal %q is not settled", text))
		return nil
	}
	switch kind {
	case "bool":
		v := "0"
		if text == "true" {
			v = "1"
		}
		return &value{text: v, ll: "i8", typ: typ}
	case "str":
		global := g.m.stringGlobal(text)
		ptr := g.newReg()
		g.emit("  %s = getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 0", ptr, len(text), global)
		lenReg := g.newReg()
		g.emit("  %s = insertvalue { ptr, %s } undef, ptr %s, 0", lenReg, g.ptrInt(), ptr)
		out := g.newReg()
		g.emit("  %s = insertvalue { ptr, %s } %s, %s %d, 1", out, g.ptrInt(), lenReg, g.ptrInt(), len(text))
		return &value{text: out, ll: "{ ptr, " + g.ptrInt() + " }", typ: typ}
	case "char":
		runes := []rune(text)
		if len(runes) != 1 {
			g.fail(fmt.Errorf("llvmgen: %q is not one character", text))
			return nil
		}
		return &value{text: strconv.FormatInt(int64(runes[0]), 10), ll: "i32", typ: typ}
	case "int":
		v, err := parseIntLiteral(text)
		if err != nil {
			g.fail(fmt.Errorf("llvmgen: %q is not an integer", text))
			return nil
		}
		return &value{text: strconv.FormatUint(v, 10), ll: g.llType(typ), typ: typ}
	case "float":
		v, err := strconv.ParseFloat(text, 64)
		if err != nil {
			g.fail(fmt.Errorf("llvmgen: %q is not a number", text))
			return nil
		}
		ll := g.llType(typ)
		out := strconv.FormatFloat(v, 'g', -1, 64)
		if !strings.ContainsAny(out, ".eEni") {
			out += ".0"
		}
		return &value{text: out, ll: ll, typ: typ}
	}
	g.fail(fmt.Errorf("llvmgen: a %s literal is not lowered", kind))
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
		return g.compareStrings(op, a, b)
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

// compareStrings lowers an equality test of two strings, which compares the
// bytes rather than the two fat pointers. Only equality is defined on str.
func (g *generator) compareStrings(op string, a, b *value) *value {
	if op != "==" && op != "!=" {
		g.fail(fmt.Errorf("llvmgen: only == and != compare strings, not %s", op))
		return nil
	}
	g.declareRuntime("zyxrt_str_eq", "i8", "ptr", g.ptrInt(), "ptr", g.ptrInt())
	ap, al := g.splitFat(a)
	bp, bl := g.splitFat(b)
	call := g.newReg()
	g.emit("  %s = call i8 @zyxrt_str_eq(ptr %s, %s %s, ptr %s, %s %s)",
		call, ap, g.ptrInt(), al, bp, g.ptrInt(), bl)
	// The runtime answers one when the two strings are equal.
	pred := "ne"
	if op == "!=" {
		pred = "eq"
	}
	reg := g.newReg()
	g.emit("  %s = icmp %s i8 %s, 0", reg, pred, call)
	return &value{text: reg, ll: "i1", typ: types.Bool}
}

// splitFat returns the pointer and the length of a fat value.
func (g *generator) splitFat(v *value) (string, string) {
	ptr := g.newReg()
	g.emit("  %s = extractvalue %s %s, 0", ptr, v.ll, v.text)
	length := g.newReg()
	g.emit("  %s = extractvalue %s %s, 1", length, v.ll, v.text)
	return ptr, length
}

// equality returns the condition that two values of one type are equal.
func (g *generator) equality(a, b *value) string {
	v := g.compare("==", a, b)
	if v == nil {
		return ""
	}
	return v.text
}

// order returns the condition an ordered comparison of two values selects.
func (g *generator) order(op string, a, b *value) string {
	v := g.compare(op, a, b)
	if v == nil {
		return ""
	}
	return v.text
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
		return &value{text: reg, ll: g.llPass(f.Type), typ: f.Type, lvalue: true, inline: true}
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
		return &value{text: reg, ll: g.llPass(recv.typ.Elem), typ: recv.typ.Elem, lvalue: true, inline: true}
	case types.KindSlice:
		ptrReg := g.newReg()
		g.emit("  %s = extractvalue %s %s, 0", ptrReg, recv.ll, recv.text)
		reg := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, i64 %s",
			reg, g.llType(recv.typ.Elem), ptrReg, idx.text)
		return &value{text: reg, ll: g.llPass(recv.typ.Elem), typ: recv.typ.Elem, lvalue: true, inline: true}
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
		g.storeInto(reg, fieldType, v, true)
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
		g.storeInto(reg, elem, v, true)
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
