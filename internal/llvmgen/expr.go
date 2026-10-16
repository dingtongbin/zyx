// Expression lowering.

package llvmgen

import (
	"fmt"
	"math/big"
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
	// sym names the code a function value carries, when the compiler knows
	// it. A runtime callback has no room for a receiver, so it can only be
	// reached through a symbol known while the program is compiled.
	sym string
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

// inlineObject returns the value that names an object lying at addr. The
// storage is the object itself rather than a reference to it, so its type is
// the object's own: a load reads the whole object, and the address is what a
// field access or a store reaches through.
func (g *generator) inlineObject(addr string, typ *types.Type) *value {
	return &value{text: addr, ll: g.llType(typ), typ: typ, lvalue: true, inline: true}
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
	if typ != nil && typ.Kind == types.KindInterface && v.typ != nil && v.typ.Kind != types.KindInterface {
		// A concrete value becomes an interface value where it is stored,
		// which is what an implicit conversion is.
		converted := g.ifaceValue(typ, v)
		if converted == nil {
			return
		}
		v = converted
	}
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

// conform returns a value whose representation matches the type it is passed
// as. A bool is one bit in the result of a comparison and a byte in memory and
// in a parameter, so the two are converted where a value crosses between them.
func (g *generator) conform(v *value, typ *types.Type) *value {
	if v == nil || typ == nil {
		return v
	}
	want := g.llPass(typ)
	if v.ll == want || want == "void" {
		return v
	}
	if v.ll == "i1" && strings.HasPrefix(want, "i") {
		reg := g.newReg()
		g.emit("  %s = zext i1 %s to %s", reg, v.text, want)
		return &value{text: reg, ll: want, typ: typ}
	}
	g.fail(fmt.Errorf("llvmgen: %s is passed as %s, not %s", v.typ, v.ll, want))
	return nil
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
	v := g.exprValue(module, n, want)
	if n != nil && hasMark(n, "static-release") && v != nil && releasable(v.typ) {
		// The mark names the last use, not the last read of the value: the
		// expression that consumes it still runs. The release is therefore
		// held until the statement ends. The address is kept in a slot,
		// because the block that computed it need not dominate the release.
		slot := g.alloca(types.Raw)
		g.emit("  store ptr %s, ptr %s", g.addrText(v), slot)
		g.fn.pending = append(g.fn.pending, pendingRelease{typ: v.typ, slot: slot})
	}
	return v
}

// releasable reports whether the backend can return a value's object to its
// region on its own. A reference object it allocated is its own storage; a
// string or a slice points at a buffer the runtime owns, or at a literal in
// read-only storage, so releasing it needs the runtime that made it.
func releasable(t *types.Type) bool {
	return t != nil && byAddress(t)
}

// exprValue lowers one expression without releasing anything.
func (g *generator) exprValue(module string, n *zyxc.Node, want *types.Type) *value {
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
	case "slice":
		return g.sliceExpr(module, n)
	case "new":
		return g.newExpr(module, n)
	case "struct-lit":
		return g.structLit(module, n)
	case "map-lit":
		return g.mapLit(module, n)
	case "set-lit":
		return g.setLit(module, n)
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
	case "c-global-ref":
		return g.cGlobal(module, n)
	case "func-ref":
		return g.funcRef(n)
	case "method-value":
		return g.methodValue(module, n)
	case "lambda":
		return g.lambda(module, n)
	case "addr":
		return g.addressOf(module, n)
	case "array-repeat":
		return g.arrayRepeat(module, n)
	case "spawn":
		return g.spawn(module, n)
	case "join":
		return g.join(module, n)
	case "try":
		return g.tryCall(module, n)
	case "is":
		return g.isCheck(module, n)
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

// fstring lowers an interpolated string. The pieces are gathered in order
// into one text, so the whole string costs one buffer rather than one copy
// per piece. A value is written the way the language writes it, which is what
// the printing helpers do, so an interpolated number and a printed number
// read the same.
func (g *generator) fstring(module string, n *zyxc.Node) *value {
	g.declareRuntime("zyxrt_buf_new", "ptr", g.ptrInt())
	buf := g.newReg()
	g.emit("  %s = call ptr @zyxrt_buf_new(%s 0)", buf, g.ptrInt())
	for _, child := range n.Children {
		switch child.Kind {
		case "part-text":
			text := ""
			if len(child.Payload) > 0 {
				text = child.Payload[0]
			}
			g.addText(buf, text)
		case "part-expr":
			v := g.load(g.expr(module, g.sole(child), nil))
			if v == nil || !g.addValue(module, buf, v) {
				return nil
			}
		}
	}
	// The buffer owns the text, so the answer is a copy of what it holds.
	length := g.alloca(types.Usize)
	g.declareRuntime("zyxrt_buf_str", "void", "ptr", "ptr", "ptr")
	text := g.alloca(types.Raw)
	g.emit("  call void @zyxrt_buf_str(ptr %s, ptr %s, ptr %s)", buf, text, length)
	ptr := g.newReg()
	g.emit("  %s = load ptr, ptr %s", ptr, text)
	size := g.newReg()
	g.emit("  %s = load %s, ptr %s", size, g.ptrInt(), length)
	ll := g.llType(types.Str)
	first := g.newReg()
	g.emit("  %s = insertvalue %s undef, ptr %s, 0", first, ll, ptr)
	out := g.newReg()
	g.emit("  %s = insertvalue %s %s, %s %s, 1", out, ll, first, g.ptrInt(), size)
	return &value{text: out, ll: ll, typ: types.Str}
}

// addText appends one literal piece to the text being gathered.
func (g *generator) addText(buf, text string) {
	if text == "" {
		return
	}
	global := g.m.stringGlobal(text)
	g.declareRuntime("zyxrt_buf_write", "void", "ptr", "ptr", g.ptrInt())
	g.emit("  call void @zyxrt_buf_write(ptr %s, ptr %s, %s %d)", buf, global, g.ptrInt(), len(text))
}

// addValue appends one value to the text being gathered, in the form the
// language writes that value in.
func (g *generator) addValue(module string, buf string, v *value) bool {
	typ := v.typ
	if typ == nil {
		g.fail(fmt.Errorf("llvmgen: the type of an interpolated value is not settled"))
		return false
	}
	if typ.Kind == types.KindStr {
		ptr, size := g.splitFat(v)
		g.declareRuntime("zyxrt_buf_write", "void", "ptr", "ptr", g.ptrInt())
		g.emit("  call void @zyxrt_buf_write(ptr %s, ptr %s, %s %s)", buf, ptr, g.ptrInt(), size)
		return true
	}
	fn, args, ok := g.bufWriter(typ)
	if !ok {
		return g.addShown(module, buf, v)
	}
	regs := g.bufArgs(typ, v)
	lls := append([]string{"ptr"}, args...)
	vals := append([]string{buf}, regs...)
	g.declareRuntime(fn, "void", lls...)
	g.emit("  call void @%s(%s)", fn, typedArgs(lls, vals))
	return true
}

// bufWriter returns the runtime writer that appends one value to a buffer, in
// the form the language writes that value in, and the LLVM types of the
// arguments it takes after the buffer. The language gives every primitive its
// text from this table rather than from a Show declaration, so an
// interpolated primitive and a Show call on one follow the same rule.
func (g *generator) bufWriter(typ *types.Type) (string, []string, bool) {
	switch {
	case typ.IsFloat():
		return "zyxrt_buf_write_f64", []string{g.llType(typ)}, true
	case typ.Kind == types.KindBool:
		return "zyxrt_buf_write_bool", []string{"i8"}, true
	case typ.Kind == types.KindChar:
		return "zyxrt_buf_write_char", []string{"i32"}, true
	case typ.Kind == types.KindDuration:
		// A duration is a count of nanoseconds and nothing else, so its text
		// is that count.
		return "zyxrt_buf_write_dur", []string{g.ptrInt()}, true
	case typ.Kind == types.KindInt128:
		return "zyxrt_buf_write_i128", []string{g.ptrInt(), g.ptrInt()}, true
	case typ.Kind == types.KindUint128:
		return "zyxrt_buf_write_u128", []string{g.ptrInt(), g.ptrInt()}, true
	case typ.IsSigned():
		return "zyxrt_buf_write_i64", []string{g.ptrInt()}, true
	case typ.IsInteger():
		return "zyxrt_buf_write_u64", []string{g.ptrInt()}, true
	}
	return "", nil, false
}

// bufArgs returns the registers that carry a value to the writer the fixed
// table names. A 128-bit value is handed over as its two halves, because the
// runtime is portable C11 and has no 128-bit type to receive it.
func (g *generator) bufArgs(typ *types.Type, v *value) []string {
	if typ.Kind != types.KindInt128 && typ.Kind != types.KindUint128 {
		return []string{v.text}
	}
	high := g.newReg()
	g.emit("  %s = lshr i128 %s, 64", high, v.text)
	upper := g.newReg()
	g.emit("  %s = trunc i128 %s to %s", upper, high, g.ptrInt())
	lower := g.newReg()
	g.emit("  %s = trunc i128 %s to %s", lower, v.text, g.ptrInt())
	return []string{upper, lower}
}

// typedArgs renders the LLVM types of a call beside the registers that fill
// them.
func typedArgs(lls, regs []string) string {
	parts := make([]string, len(lls))
	for i := range lls {
		parts[i] = lls[i] + " " + regs[i]
	}
	return strings.Join(parts, ", ")
}

// builtinShow reports whether the language gives the type its Show from the
// fixed table rather than from a declaration, which is the case for every
// value that table can write.
func (g *generator) builtinShow(t *types.Type) bool {
	if t == nil {
		return false
	}
	if t.Kind == types.KindStr {
		return true
	}
	_, _, ok := g.bufWriter(t)
	return ok
}

// builtinShowCall lowers a Show call on a value the fixed table writes: the
// value is appended to the buffer the call names, with no declaration and no
// intermediate string.
func (g *generator) builtinShowCall(module string, n *zyxc.Node, v *value) *value {
	arg := g.wrapped(n, "arg")
	if arg == nil {
		g.fail(fmt.Errorf("llvmgen: Show needs the buffer it writes into"))
		return nil
	}
	buf := g.load(g.expr(module, arg, nil))
	if buf == nil {
		return nil
	}
	if !g.addValue(module, buf.text, v) {
		return nil
	}
	return &value{ll: "void"}
}

// equatable reports whether the language gives the type its own equality,
// which the backend lowers without a declaration. It is the set the check
// accepts for Eq, so the two never disagree.
func equatable(t *types.Type) bool {
	if t == nil {
		return false
	}
	return t.IsNumeric() || t.Kind == types.KindBool || t.Kind == types.KindChar ||
		t.Kind == types.KindRaw || t.Kind == types.KindStr || t.Kind == types.KindDuration
}

// hashable reports whether the language gives the type a hash, which the
// backend lowers without a declaration. It is the set the check accepts for
// Hash, so the two never disagree.
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

// builtinEqCall lowers an Eq call on a value the language compares itself. A
// scalar compares with one instruction and a str compares the bytes it names,
// so the call costs no more than the operator the interface stands for.
func (g *generator) builtinEqCall(module string, n *zyxc.Node, v *value) *value {
	other := g.load(g.expr(module, g.wrapped(n, "arg"), v.typ))
	if other == nil {
		return nil
	}
	if v.typ.Kind == types.KindStr {
		ap, an := g.splitFat(v)
		bp, bn := g.splitFat(other)
		g.declareRuntime("zyxrt_str_eq", "i8", "ptr", g.ptrInt(), "ptr", g.ptrInt())
		reg := g.newReg()
		g.emit("  %s = call i8 @zyxrt_str_eq(ptr %s, %s %s, ptr %s, %s %s)",
			reg, ap, g.ptrInt(), an, bp, g.ptrInt(), bn)
		return &value{text: reg, ll: "i8", typ: types.Bool}
	}
	reg := g.newReg()
	if v.typ.IsFloat() {
		g.emit("  %s = fcmp oeq %s %s, %s", reg, v.ll, v.text, other.text)
	} else {
		g.emit("  %s = icmp eq %s %s, %s", reg, v.ll, v.text, other.text)
	}
	// A comparison yields one bit, and the language stores a bool as a byte.
	wide := g.newReg()
	g.emit("  %s = zext i1 %s to i8", wide, reg)
	return &value{text: wide, ll: "i8", typ: types.Bool}
}

// builtinHashCall lowers a Hash call on a value the language hashes itself.
// The hash reads the bytes of the value, which is the rule a container uses
// for a key of the same type, so a Hash call and a key agree.
// ordered reports whether the language orders the type itself, which the
// backend lowers without a declaration. It is the set the check accepts for
// Order, so the two never disagree.
func ordered(t *types.Type) bool {
	if t == nil {
		return false
	}
	return (t.IsNumeric() && !t.IsFloat()) || t.Kind == types.KindBool ||
		t.Kind == types.KindChar || t.Kind == types.KindRaw ||
		t.Kind == types.KindStr || t.Kind == types.KindDuration
}

// builtinCompareCall lowers a Compare call on a value the language orders
// itself. The answer is a negative number, zero, or a positive number, which
// is what a sorted container asks for.
func (g *generator) builtinCompareCall(module string, n *zyxc.Node, v *value) *value {
	other := g.load(g.expr(module, g.wrapped(n, "arg"), v.typ))
	if other == nil {
		return nil
	}
	if v.typ.Kind == types.KindStr {
		ap, an := g.splitFat(v)
		bp, bn := g.splitFat(other)
		g.declareRuntime("zyxrt_str_compare", "i32",
			"ptr", g.ptrInt(), "ptr", g.ptrInt(), "ptr", g.ptrInt(), "ptr", g.ptrInt())
		reg := g.newReg()
		g.emit("  %s = call i32 @zyxrt_str_compare(ptr %s, %s %s, ptr %s, %s %s)",
			reg, ap, g.ptrInt(), an, bp, g.ptrInt(), bn)
		return &value{text: reg, ll: "i32", typ: types.Int32}
	}
	// A value that counts or addresses compares without a sign, and one that
	// measures a distance compares with one.
	less, greater := "slt", "sgt"
	if v.typ.IsUnsigned() || v.typ.Kind == types.KindBool ||
		v.typ.Kind == types.KindChar || v.typ.Kind == types.KindRaw {
		less, greater = "ult", "ugt"
	}
	low := g.newReg()
	g.emit("  %s = icmp %s %s %s, %s", low, less, v.ll, v.text, other.text)
	high := g.newReg()
	g.emit("  %s = icmp %s %s %s, %s", high, greater, v.ll, v.text, other.text)
	pos := g.newReg()
	g.emit("  %s = select i1 %s, i32 1, i32 0", pos, high)
	reg := g.newReg()
	g.emit("  %s = select i1 %s, i32 -1, i32 %s", reg, low, pos)
	return &value{text: reg, ll: "i32", typ: types.Int32}
}

func (g *generator) builtinHashCall(module string, n *zyxc.Node, v *value) *value {
	size, err := layout.Size(v.typ, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("llvmgen: the size of %s: %w", v.typ, err))
		return nil
	}
	slot := g.objectAlloca(v.typ)
	g.storeInto(slot, v.typ, v, true)
	fn := "zyxrt_hash_bytes"
	if v.typ.Kind == types.KindStr {
		// A str is a fat pointer, so its hash reads the bytes it names.
		fn = "zyxrt_hash_str_key"
	}
	g.declareRuntime(fn, g.ptrInt(), "ptr", g.ptrInt())
	reg := g.newReg()
	g.emit("  %s = call %s @%s(ptr %s, %s %d)", reg, g.ptrInt(), fn, slot, g.ptrInt(), size)
	return &value{text: reg, ll: g.ptrInt(), typ: types.Uint64}
}

// foldedNegation returns the value of a negated integer literal, or nil when
// the node is not one. The value is turned at its own width, so a literal
// that names the smallest negative value of its type is written as one.
func (g *generator) foldedNegation(module string, lit *zyxc.Node) *value {
	if lit == nil || lit.Kind != "lit" || len(lit.Payload) < 2 || lit.Payload[0] != "int" {
		return nil
	}
	typ := g.typeOfNode(lit.First("type"), module)
	if typ == nil || !typ.IsInteger() {
		return nil
	}
	v, ok := new(big.Int).SetString(strings.ReplaceAll(lit.Payload[1], "_", ""), 0)
	if !ok {
		return nil
	}
	v.Neg(v)
	return &value{text: v.String(), ll: g.llType(typ), typ: typ}
}

// methodValue lowers a method taken as a value. The value carries the
// method's address and the receiver it was taken from, so a call through it
// supplies that receiver as the method's own first parameter. A value receiver
// is copied, and a reference receiver is shared.
func (g *generator) methodValue(module string, n *zyxc.Node) *value {
	if len(n.Payload) < 2 {
		g.fail(fmt.Errorf("llvmgen: a method value needs a symbol"))
		return nil
	}
	typ := g.typeOfNode(n.First("type"), module)
	if typ == nil {
		g.fail(fmt.Errorf("llvmgen: the type of a method value is not settled"))
		return nil
	}
	recv := g.load(g.expr(module, g.wrapped(n, "recv"), nil))
	if recv == nil {
		return nil
	}
	addr := g.addrText(recv)
	if !byAddress(recv.typ) {
		slot := g.alloca(recv.typ)
		g.storeInto(slot, recv.typ, recv, true)
		addr = slot
	}
	ll := g.llType(typ)
	first := g.newReg()
	g.emit("  %s = insertvalue %s undef, ptr @%s, 0", first, ll, llvmName(n.Payload[1]))
	out := g.newReg()
	g.emit("  %s = insertvalue %s %s, ptr %s, 1", out, ll, first, addr)
	return &value{text: out, ll: ll, typ: typ}
}

// addShown appends a value of a declared type, which writes itself into the
// buffer through its Show. The method takes the value as its receiver and the
// buffer as its argument, so nothing becomes a string in between.
func (g *generator) addShown(module string, buf string, v *value) bool {
	sym := g.implMethod(v.typ, types.Show, "Show")
	if sym == "" {
		g.fail(fmt.Errorf("llvmgen: %s cannot be interpolated: it has no Show", v.typ))
		return false
	}
	g.emit("  call void @%s(ptr %s, ptr %s)", llvmName(sym), g.addrText(v), buf)
	return true
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
		// An interpolated string is a literal whose pieces are children, so
		// it is gathered rather than written out as one text.
		return g.fstring(module, n)
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
		digits, err := intLiteralText(text)
		if err != nil {
			g.fail(fmt.Errorf("llvmgen: %q is not an integer", text))
			return nil
		}
		return &value{text: digits, ll: g.llType(typ), typ: typ}
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

// intLiteralText returns the decimal text of an integer literal, whatever
// base and separators the source wrote it in. A literal may name a value of
// any width, so one that does not fit a machine word is carried at its own
// width rather than truncated.
func intLiteralText(text string) (string, error) {
	digits, base := splitLiteral(text)
	if v, err := strconv.ParseUint(digits, base, 64); err == nil {
		return strconv.FormatUint(v, 10), nil
	}
	v, ok := new(big.Int).SetString(digits, base)
	if !ok {
		return "", fmt.Errorf("an integer literal")
	}
	return v.String(), nil
}

// splitLiteral returns the digits of an integer literal and the base they are
// written in.
func splitLiteral(text string) (string, int) {
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
	return clean, base
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
	// The checks the language defines belong to the operators on integers.
	// A duration and a floating-point value carry their own arithmetic.
	checked := typ.IsInteger()
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
	case checked && (op == "+" || op == "-" || op == "*"):
		return g.checkedArith(op, a, b)
	case op == "/" || op == "%":
		if checked {
			g.guardDivisor(typ, a.text, b.text, ll)
		}
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
			if checked {
				g.guardShift(b.text, ll)
			}
			instr = "shl"
		}
		if op == ">>" {
			if checked {
				g.guardShift(b.text, ll)
			}
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
	operand := g.wrapped(n, "operand")
	// A negation applied to a literal is a constant rather than an operation,
	// so it is folded: the smallest negative value of a type has no positive
	// counterpart to negate, and folding is what makes it writable.
	if op == "-" {
		if folded := g.foldedNegation(module, operand); folded != nil {
			return folded
		}
	}
	v := g.load(g.expr(module, operand, nil))
	if v == nil {
		return nil
	}
	reg := g.newReg()
	switch op {
	case "-":
		if v.typ.IsFloat() {
			g.emit("  %s = fneg %s %s", reg, v.ll, v.text)
		} else if v.typ.IsInteger() && v.typ.IsSigned() {
			// The smallest value has no positive counterpart, so negating it
			// is checked like any other signed operation.
			return g.checkedArith("-", &value{text: "0", ll: v.ll, typ: v.typ}, v)
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
	if src.typ != nil && src.typ.Kind == types.KindEnum && dst.IsInteger() {
		// An enum's tag is an integer, and the object the value names holds
		// the tag first.
		size := g.tagSize(src.typ)
		ll := g.tagType(size)
		reg := g.newReg()
		g.emit("  %s = load %s, ptr %s", reg, ll, src.text)
		src = &value{text: reg, ll: ll, typ: tagInteger(size)}
	}
	return g.cast(src, dst)
}

// tagInteger returns the unsigned integer type of a tag of the given size,
// which is what an enum's tag converts as.
func tagInteger(size uint64) *types.Type {
	switch size {
	case 1:
		return types.Uint8
	case 2:
		return types.Uint16
	case 4:
		return types.Uint32
	}
	return types.Uint64
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
	// A field is reached through the storage of its object, so a value that
	// lies inline keeps the address it lies at.
	base := g.addrText(recv)
	typ := recv.typ
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
		// The members of a union share their offset, so every one of them is
		// reached where the union begins.
		at := i
		if typ.Kind == types.KindUnion {
			at = 0
		}
		reg := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, i32 0, i32 %d", reg, g.llType(typ), base, at)
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
		// An index is checked: the language panics rather than reading past
		// the sequence, which also makes the in-bounds arithmetic below sound.
		g.guardIndex(idx.text, strconv.FormatUint(recv.typ.Len, 10))
		reg := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, i64 0, i64 %s",
			reg, g.llType(recv.typ), recv.text, idx.text)
		return &value{text: reg, ll: g.llPass(recv.typ.Elem), typ: recv.typ.Elem, lvalue: true, inline: true}
	case types.KindSlice:
		ptrReg := g.newReg()
		g.emit("  %s = extractvalue %s %s, 0", ptrReg, recv.ll, recv.text)
		lenReg := g.newReg()
		g.emit("  %s = extractvalue %s %s, 1", lenReg, recv.ll, recv.text)
		g.guardIndex(idx.text, lenReg)
		reg := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, i64 %s",
			reg, g.llType(recv.typ.Elem), ptrReg, idx.text)
		return &value{text: reg, ll: g.llPass(recv.typ.Elem), typ: recv.typ.Elem, lvalue: true, inline: true}
	case types.KindStr:
		// A str is indexed by its bytes, and the index is checked like any
		// other.
		ptrReg := g.newReg()
		g.emit("  %s = extractvalue %s %s, 0", ptrReg, recv.ll, recv.text)
		lenReg := g.newReg()
		g.emit("  %s = extractvalue %s %s, 1", lenReg, recv.ll, recv.text)
		g.guardIndex(idx.text, lenReg)
		reg := g.newReg()
		g.emit("  %s = getelementptr inbounds i8, ptr %s, i64 %s", reg, ptrReg, idx.text)
		return &value{text: reg, ll: "i8", typ: types.Uint8, lvalue: true, inline: true}
	case types.KindContainer:
		switch recv.typ.Name {
		case "Array":
			// The element lives in the buffer the runtime owns, so the access
			// answers its address: an assignment through it writes the element
			// itself rather than a copy of it. The address stays valid until
			// the array is structurally modified, which the language forbids
			// while a borrow is live.
			elem := arg(recv.typ, 0)
			g.declareRuntime("zyxrt_array_len", g.ptrInt(), "ptr")
			length := g.newReg()
			g.emit("  %s = call %s @zyxrt_array_len(ptr %s)", length, g.ptrInt(), recv.text)
			g.guardIndex(idx.text, length)
			g.declareRuntime("zyxrt_array_slot", "ptr", "ptr", g.ptrInt())
			slot := g.newReg()
			g.emit("  %s = call ptr @zyxrt_array_slot(ptr %s, %s %s)", slot, recv.text, idx.ll, idx.text)
			return g.inlineObject(slot, elem)
		case "Map":
			place, ok := g.mapPlaceOf(module, n, recv.typ)
			if !ok {
				return nil
			}
			return g.inlineObject(g.mapSlot(place), arg(recv.typ, 1))
		}
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

// sliceExpr lowers a view of a sequence: the address of the first element it
// keeps and the number of elements.
func (g *generator) sliceExpr(module string, n *zyxc.Node) *value {
	recv := g.load(g.expr(module, g.wrapped(n, "recv"), nil))
	lo := g.load(g.expr(module, g.wrapped(n, "lo"), types.Usize))
	hi := g.load(g.expr(module, g.wrapped(n, "hi"), types.Usize))
	if recv == nil || lo == nil || hi == nil {
		return nil
	}
	if lo.ll != g.ptrInt() || hi.ll != g.ptrInt() {
		g.fail(fmt.Errorf("llvmgen: a view is indexed by %s, want %s", lo.ll, g.ptrInt()))
		return nil
	}
	elem := recv.typ.Elem
	base := g.addrText(recv)
	if recv.typ.Kind == types.KindSlice {
		base, _ = g.splitFat(recv)
	}
	ptr := g.newReg()
	g.emit("  %s = getelementptr inbounds %s, ptr %s, %s %s", ptr, g.llType(elem), base, lo.ll, lo.text)
	count := g.newReg()
	g.emit("  %s = sub %s %s, %s", count, lo.ll, hi.text, lo.text)
	typ := types.SliceOf(elem)
	fat := g.newReg()
	g.emit("  %s = insertvalue %s undef, ptr %s, 0", fat, g.llType(typ), ptr)
	out := g.newReg()
	g.emit("  %s = insertvalue %s %s, %s %s, 1", out, g.llType(typ), fat, g.ptrInt(), count)
	return &value{text: out, ll: g.llType(typ), typ: typ}
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
	var base string
	if byAddress(typ) {
		base = g.allocateIn(n, typ)
	} else {
		// A value type lies in the frame rather than in a region: its bytes
		// travel with the value, which is what lets it cross to C.
		base = g.objectAlloca(typ)
	}
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
		// The members of a union share their offset, so every one of them is
		// written where the union begins.
		if typ.Kind == types.KindUnion {
			index = 0
		}
		reg := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, i32 0, i32 %d", reg, g.llType(typ), base, index)
		v := g.expr(module, init, fieldType)
		if v == nil {
			return nil
		}
		g.storeInto(reg, fieldType, v, true)
	}
	// A value type lies in the frame, so the literal names that storage and
	// a read of it loads the bytes; a reference type names its object.
	if !byAddress(typ) {
		return &value{text: base, ll: g.llType(typ), typ: typ, lvalue: true, inline: true}
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

// arrayRepeat lowers the `[value] * count` form. A fixed-size array is one
// block that lies in a region, so it is allocated and filled in place; a
// growable array is a container, so the value is pushed once per repetition.
func (g *generator) arrayRepeat(module string, n *zyxc.Node) *value {
	typ := g.typeOfNode(n.First("type"), module)
	if typ == nil {
		g.fail(fmt.Errorf("llvmgen: a repeated array needs a type"))
		return nil
	}
	elem := typ.Elem
	if typ.Kind != types.KindArray {
		elem = arg(typ, 0)
	}
	if elem == nil {
		g.fail(fmt.Errorf("llvmgen: a repeated array needs an element type"))
		return nil
	}
	repeated := g.load(g.expr(module, g.wrapped(n, "elem"), elem))
	if repeated == nil {
		return nil
	}
	if typ.Kind == types.KindArray {
		base := g.allocate(typ)
		g.repeatStores(base, typ, elem, repeated)
		return &value{text: base, ll: "ptr", typ: typ}
	}
	// A growable array is a container: the runtime makes one with room for
	// every repetition, and the value is pushed into it.
	request := &zyxc.Node{Kind: "new"}
	request.Child(zyxc.New("type", typ.String()))
	request.Child(zyxc.New("arg").Child(g.wrapped(n, "count")))
	handle := g.newContainer(module, request, typ)
	if handle == nil {
		return nil
	}
	g.repeatPushes(module, n, handle, elem, repeated)
	return handle
}

// repeatStores fills a fixed-size array with one value.
func (g *generator) repeatStores(base string, typ, elem *types.Type, repeated *value) {
	if typ.Len == 0 {
		return
	}
	index := g.alloca(types.Usize)
	g.emit("  store %s 0, ptr %s", g.ptrInt(), index)
	condition := g.newLabel("repeat.cond")
	body := g.newLabel("repeat.body")
	done := g.newLabel("repeat.end")
	g.branch("  br label %%%s", condition)
	g.label(condition)
	at := g.newReg()
	g.emit("  %s = load %s, ptr %s", at, g.ptrInt(), index)
	more := g.newReg()
	g.emit("  %s = icmp ult %s %s, %d", more, g.ptrInt(), at, typ.Len)
	g.branch("  br i1 %s, label %%%s, label %%%s", more, body, done)
	g.label(body)
	slot := g.newReg()
	g.emit("  %s = getelementptr inbounds %s, ptr %s, i64 0, i64 %s", slot, g.llType(typ), base, at)
	g.storeInto(slot, elem, repeated, true)
	next := g.newReg()
	g.emit("  %s = add %s %s, 1", next, g.ptrInt(), at)
	g.emit("  store %s %s, ptr %s", g.ptrInt(), next, index)
	g.branch("  br label %%%s", condition)
	g.label(done)
}

// repeatPushes pushes one value into a growable array once per repetition.
func (g *generator) repeatPushes(module string, n *zyxc.Node, handle *value, elem *types.Type, repeated *value) {
	count := g.load(g.expr(module, g.wrapped(n, "count"), types.Usize))
	if count == nil {
		return
	}
	index := g.alloca(types.Usize)
	g.emit("  store %s 0, ptr %s", g.ptrInt(), index)
	condition := g.newLabel("repeat.cond")
	body := g.newLabel("repeat.body")
	done := g.newLabel("repeat.end")
	g.branch("  br label %%%s", condition)
	g.label(condition)
	at := g.newReg()
	g.emit("  %s = load %s, ptr %s", at, g.ptrInt(), index)
	more := g.newReg()
	g.emit("  %s = icmp ult %s %s, %s", more, g.ptrInt(), at, count.text)
	g.branch("  br i1 %s, label %%%s, label %%%s", more, body, done)
	g.label(body)
	slot := g.objectAlloca(elem)
	g.storeInto(slot, elem, repeated, true)
	g.declareRuntime("zyxrt_array_push", "void", "ptr", "ptr")
	g.emit("  call void @zyxrt_array_push(ptr %s, ptr %s)", handle.text, slot)
	next := g.newReg()
	g.emit("  %s = add %s %s, 1", next, g.ptrInt(), at)
	g.emit("  store %s %s, ptr %s", g.ptrInt(), next, index)
	g.branch("  br label %%%s", condition)
	g.label(done)
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

// linkName returns the name a function carries in the object code. A C ABI
// function keeps its own name, because that is the name the C side defines;
// every other function is named by its symbol id, which carries the package
// and cannot collide.
func (g *generator) linkName(sym string) string {
	decl := g.tab.decl(sym)
	if decl != nil && len(decl.Payload) >= 2 {
		switch decl.Payload[1] {
		case "c", "extern":
			return decl.Payload[0]
		}
	}
	return sym
}

// cABIFunc reports whether a function crosses the C boundary, which is what
// decides how a call to it passes its arguments.
func (g *generator) cABIFunc(sym string) bool {
	decl := g.tab.decl(sym)
	if decl == nil || len(decl.Payload) < 2 {
		return false
	}
	return decl.Payload[1] == "c" || decl.Payload[1] == "extern"
}

// addressOf lowers `&x`, which only appears as a C call argument.
func (g *generator) addressOf(module string, n *zyxc.Node) *value {
	v := g.expr(module, g.wrapped(n, "operand"), nil)
	if v == nil {
		return nil
	}
	return &value{text: g.addrText(v), ll: "ptr", typ: types.Raw}
}

// funcRef lowers a reference to a function. A function value carries the
// function and a receiver, so a plain function is paired with a thunk that
// drops the receiver: a call through a function value then has one shape
// whichever function it reaches.
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
	return g.functionValue(sym, g.funcType(decl))
}

// functionValue builds the value of a function: the thunk that drops the
// receiver a value carries, and no receiver.
func (g *generator) functionValue(sym string, typ *types.Type) *value {
	ll := g.llType(typ)
	code := g.dropReceiver(sym, typ)
	first := g.newReg()
	g.emit("  %s = insertvalue %s undef, ptr %s, 0", first, ll, code)
	out := g.newReg()
	g.emit("  %s = insertvalue %s %s, ptr null, 1", out, ll, first)
	return &value{text: out, ll: ll, typ: typ, sym: code}
}

// dropReceiver returns the name of the thunk that ignores the receiver a
// function value carries and calls the function itself. One thunk serves
// every value of one function.
//
// A C function is called the way C is called: its arguments are placed by the
// C ABI rather than passed as they arrive, and the symbol it is called by is
// the name the C side defines.
func (g *generator) dropReceiver(sym string, typ *types.Type) string {
	if name, ok := g.m.receivers[sym]; ok {
		return "@" + name
	}
	name := fmt.Sprintf("zyx.drop.%d", len(g.m.receivers))
	g.m.receivers[sym] = name

	cABI := g.cABIFunc(sym)

	saved := g.fn
	g.fn = &funcState{symbol: name, vars: map[string]*varSlot{}}
	g.fn.entry = g.newLabel("drop")

	var params, args []string
	for i, pt := range typ.Elems {
		ll := g.llPass(pt)
		arg := fmt.Sprintf("%%p%d_0", i+1)
		params = append(params, fmt.Sprintf("%s %s", ll, arg))
		if !cABI {
			args = append(args, fmt.Sprintf("%s %s", ll, arg))
			continue
		}
		// The C ABI reads an argument from where it lies, so the value is
		// spilled first and then placed the way the target places it.
		slot := g.alloca(pt)
		g.emit("  store %s %s, ptr %s", ll, arg, slot)
		args = append(args, g.cArguments(&value{text: slot, ll: ll, typ: pt, lvalue: true})...)
	}
	ret, callRet := "void", "void"
	if typ.Result != nil {
		ret = g.llPass(typ.Result)
		callRet = ret
		if cABI {
			// A C function answers with what its own ABI returns.
			var ok bool
			callRet, ok = g.cReturn(typ.Result)
			if !ok {
				g.fn = saved
				return "@" + name
			}
		}
	}
	if typ.Result == nil {
		g.terminator("  call void @%s(%s)", llvmName(g.linkName(sym)), strings.Join(args, ", "))
		g.terminator("  ret void")
	} else {
		reg := g.newReg()
		g.emit("  %s = call %s @%s(%s)", reg, callRet, llvmName(g.linkName(sym)), strings.Join(args, ", "))
		if callRet == ret {
			g.terminator("  ret %s %s", ret, reg)
		} else {
			// A result that crosses as something else than it lies is
			// converted back to the value a function value answers with.
			g.terminator("  ret %s %s", ret, g.conform(&value{text: reg, ll: callRet, typ: typ.Result}, typ.Result).text)
		}
	}
	g.emitThunk(name, ret, strings.Join(append([]string{"ptr %p0_0"}, params...), ", "))
	g.fn = saved
	return "@" + name
}
