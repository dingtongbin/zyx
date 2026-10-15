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
	case "func-ref":
		return g.funcRef(n)
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
	base := g.allocateIn(n, typ)
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
