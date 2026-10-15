// Enums: the tagged union, variant construction, and match.

package llvmgen

import (
	"fmt"

	"zyx/internal/layout"
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// enumBody renders an enum as its tag followed by the union of the variant
// payloads, which is the shape the layout algorithm computes.
func (g *generator) enumBody(t *types.Type) string {
	variants, ok := g.tab.Variants(t)
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no variants", t))
		return "{ i8 }"
	}
	shape, err := layout.Enum(t, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("layout of %s: %w", t, err))
		return "{ i8 }"
	}
	tag := g.tagType(shape.TagSize)
	if shape.PayloadSize == 0 {
		return "{" + tag + "}"
	}
	return "{" + tag + ", " + g.payloadUnion(variants, shape) + "}"
}

// tagType returns the LLVM integer type of a tag of the given size.
func (g *generator) tagType(size uint64) string {
	switch size {
	case 1:
		return "i8"
	case 2:
		return "i16"
	case 4:
		return "i32"
	}
	return "i64"
}

// payloadUnion renders the union of the variant payloads as its widest-aligned
// member followed by the padding that brings it to the union's size. The
// widest-aligned member is what gives the union its alignment, which is the
// alignment the layout algorithm used to place the union after the tag.
func (g *generator) payloadUnion(variants []layout.Variant, shape layout.EnumShape) string {
	best, bestAlign := -1, uint64(0)
	for i, v := range variants {
		payload := types.TupleOf(v.Payload...)
		align, err := layout.Align(payload, g.tab, g.target.Layout())
		if err != nil {
			g.fail(fmt.Errorf("llvmgen: the alignment of %s: %w", payload, err))
			return fmt.Sprintf("[%d x i8]", shape.PayloadSize)
		}
		if align > bestAlign {
			best, bestAlign = i, align
		}
	}
	if best < 0 {
		return fmt.Sprintf("[%d x i8]", shape.PayloadSize)
	}
	payload := types.TupleOf(variants[best].Payload...)
	member, err := layout.Size(payload, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("llvmgen: the size of %s: %w", payload, err))
		return fmt.Sprintf("[%d x i8]", shape.PayloadSize)
	}
	if pad := shape.PayloadSize - member; pad != 0 {
		return fmt.Sprintf("{%s, [%d x i8]}", g.llType(payload), pad)
	}
	return "{" + g.llType(payload) + "}"
}

// payloadValue returns one element of a variant payload, read out of the
// object.
func (g *generator) payloadValue(base string, typ *types.Type, elem *types.Type, index int) *value {
	addr := g.payloadElement(base, typ, []*types.Type{elem}, index)
	return g.load(&value{text: addr, ll: g.llPass(elem), typ: elem, lvalue: true, inline: true})
}

// variantOf returns the position and payload of one variant of an enum.
func (g *generator) variantOf(t *types.Type, name string) (int, []*types.Type, bool) {
	variants, ok := g.tab.Variants(t)
	if !ok {
		return 0, nil, false
	}
	for i, v := range variants {
		if v.Name == name {
			return i, v.Payload, true
		}
	}
	return 0, nil, false
}

// variantLit lowers a variant construction. The object is created in the
// region of the running coroutine, its tag is written, and its payload
// elements are written into the union.
func (g *generator) variantLit(module string, n *zyxc.Node) *value {
	if len(n.Payload) < 2 {
		g.fail(fmt.Errorf("llvmgen: a variant literal needs an enum and a variant"))
		return nil
	}
	typ, err := g.tab.parse(module, n.Payload[0])
	if err != nil {
		g.fail(err)
		return nil
	}
	name := n.Payload[1]
	_, payload, ok := g.variantOf(typ, name)
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no variant %s", typ, name))
		return nil
	}
	args := g.argNodes(n)
	values := make([]*value, 0, len(payload))
	for i, pt := range payload {
		if i >= len(args) {
			g.fail(fmt.Errorf("llvmgen: %s.%s takes %d values, got %d", typ, name, len(payload), len(args)))
			return nil
		}
		v := g.expr(module, args[i], pt)
		if v == nil {
			return nil
		}
		values = append(values, v)
	}
	base := g.buildVariant(n, typ, name, values)
	if base == "" {
		return nil
	}
	return &value{text: base, ll: "ptr", typ: typ}
}

// buildVariant creates an enum object for one variant and returns its address.
// The tag is written first and the payload elements follow it, each stored the
// way a struct field is: an object of a reference type lies inline.
func (g *generator) buildVariant(n *zyxc.Node, typ *types.Type, name string, args []*value) string {
	index, payload, ok := g.variantOf(typ, name)
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no variant %s", typ, name))
		return ""
	}
	base := g.allocateIn(n, typ)
	g.emit("  store %s %d, ptr %s", g.tagType(g.tagSize(typ)), index, base)
	for i, pt := range payload {
		if i >= len(args) {
			g.fail(fmt.Errorf("llvmgen: %s.%s takes %d values, got %d", typ, name, len(payload), len(args)))
			return ""
		}
		g.storeInto(g.payloadElement(base, typ, payload, i), pt, args[i], true)
	}
	return base
}

// tagSize returns the size of an enum's tag in bytes.
func (g *generator) tagSize(t *types.Type) uint64 {
	shape, err := layout.Enum(t, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("layout of %s: %w", t, err))
		return 1
	}
	return shape.TagSize
}

// argNodes returns the payload arguments of a construction.
func (g *generator) argNodes(n *zyxc.Node) []*zyxc.Node {
	var out []*zyxc.Node
	for _, child := range n.Children {
		if child.Kind == "arg" {
			out = append(out, g.sole(child))
		}
	}
	return out
}

// matchStmt lowers a match. Each arm tests its pattern, binds the names the
// pattern introduces, and runs its body; the first arm that matches wins.
func (g *generator) matchStmt(module string, n *zyxc.Node) {
	subject := g.expr(module, g.wrapped(n, "subject"), nil)
	if subject == nil {
		return
	}
	end := g.newLabel("match.end")
	var arms []*zyxc.Node
	for _, child := range n.Children {
		if child.Kind == "arm" {
			arms = append(arms, child)
		}
	}
	for _, arm := range arms {
		pat := g.wrapped(arm, "pat")
		cond, binds := g.patternTest(module, pat, subject)
		body := g.wrapped(arm, "body")
		if cond == "" {
			// The pattern matches everything, so this arm runs where it
			// stands and no further arm is reachable.
			g.emitBinds(binds)
			g.stmt(module, body)
			break
		}
		bodyLabel := g.newLabel("match.body")
		nextLabel := g.newLabel("match.next")
		g.terminator("  br i1 %s, label %%%s, label %%%s", cond, bodyLabel, nextLabel)
		g.label(bodyLabel)
		g.emitBinds(binds)
		g.stmt(module, body)
		if !g.fn.terminated {
			g.terminator("  br label %%%s", end)
		}
		g.label(nextLabel)
	}
	if !g.fn.terminated {
		g.terminator("  br label %%%s", end)
	}
	g.label(end)
}

// A binding is one name a pattern introduces: the storage to write and the
// address the value is read from.
type binding struct {
	slot string
	typ  *types.Type
	from string
}

// emitBinds writes the bindings of a matched pattern.
func (g *generator) emitBinds(binds []binding) {
	for _, b := range binds {
		from := &value{text: b.from, ll: g.llPass(b.typ), typ: b.typ, lvalue: true, inline: true}
		g.storeInto(b.slot, b.typ, from, false)
	}
}

// patternTest emits the test for a pattern against a subject and returns the
// condition that selects it, together with the bindings it introduces. An
// empty condition means the pattern matches everything, and the bindings are
// then unconditional.
func (g *generator) patternTest(module string, pat *zyxc.Node, subject *value) (string, []binding) {
	if pat == nil {
		return "", nil
	}
	switch pat.Kind {
	case "pat-wild":
		return "", nil
	case "pat-bind":
		if len(pat.Payload) < 1 {
			g.fail(fmt.Errorf("llvmgen: a binding pattern needs a name"))
			return "", nil
		}
		slot := g.alloca(subject.typ)
		if len(pat.Payload) >= 2 && pat.Payload[1] != "" {
			g.fn.vars[pat.Payload[1]] = &varSlot{alloca: slot, typ: subject.typ}
		}
		return "", []binding{{slot: slot, typ: subject.typ, from: g.addrText(subject)}}
	case "pat-lit":
		return g.patternLit(module, pat, subject), nil
	case "pat-range":
		return g.patternRange(module, pat, subject), nil
	case "pat-or":
		return g.patternOr(module, pat, subject), nil
	case "pat-tuple":
		return g.patternTuple(module, pat, subject)
	case "pat-variant":
		return g.patternVariant(module, pat, subject)
	}
	g.fail(fmt.Errorf("llvmgen: pattern %s is not lowered", pat.Kind))
	return "", nil
}

// patternLit lowers a pattern that matches one literal.
func (g *generator) patternLit(module string, pat *zyxc.Node, subject *value) string {
	if len(pat.Payload) < 1 {
		g.fail(fmt.Errorf("llvmgen: a literal pattern needs a value"))
		return ""
	}
	lit := &zyxc.Node{Kind: "lit", Payload: []string{literalKind(subject.typ), pat.Payload[0]}}
	v := g.literal(module, lit, subject.typ)
	if v == nil {
		return ""
	}
	return g.equality(subject, v)
}

// literalKind returns the literal kind a pattern's text is, which the type it
// is matched against settles.
func literalKind(t *types.Type) string {
	switch {
	case t == nil:
		return "int"
	case t.Kind == types.KindBool:
		return "bool"
	case t.Kind == types.KindChar:
		return "char"
	case t.Kind == types.KindStr:
		return "str"
	case t.IsFloat():
		return "float"
	}
	return "int"
}

// patternRange lowers a half-open range pattern, which matches a value that
// is not below the lower bound and below the upper one.
func (g *generator) patternRange(module string, pat *zyxc.Node, subject *value) string {
	lo := g.expr(module, g.wrapped(pat, "lo"), subject.typ)
	hi := g.expr(module, g.wrapped(pat, "hi"), subject.typ)
	if lo == nil || hi == nil {
		return ""
	}
	above := g.order("<=", lo, subject)
	below := g.order("<", subject, hi)
	if above == "" || below == "" {
		return ""
	}
	reg := g.newReg()
	g.emit("  %s = and i1 %s, %s", reg, above, below)
	return reg
}

// patternOr lowers an alternative pattern. Every alternative is tested; a
// binding is not allowed here, so nothing needs to be carried across.
func (g *generator) patternOr(module string, pat *zyxc.Node, subject *value) string {
	cond := ""
	for _, child := range pat.Children {
		sub, binds := g.patternTest(module, child, subject)
		if len(binds) != 0 {
			g.fail(fmt.Errorf("llvmgen: an alternative pattern binds a name"))
			return ""
		}
		if sub == "" {
			// One alternative matches everything, so the whole pattern does.
			return ""
		}
		if cond == "" {
			cond = sub
			continue
		}
		reg := g.newReg()
		g.emit("  %s = or i1 %s, %s", reg, cond, sub)
		cond = reg
	}
	if cond == "" {
		g.fail(fmt.Errorf("llvmgen: an alternative pattern has no alternatives"))
	}
	return cond
}

// patternTuple lowers a pattern over a tuple, which tests every element.
func (g *generator) patternTuple(module string, pat *zyxc.Node, subject *value) (string, []binding) {
	elems := subject.typ.Elems
	base := g.addrText(subject)
	cond := ""
	var binds []binding
	for i, child := range pat.Children {
		if i >= len(elems) {
			g.fail(fmt.Errorf("llvmgen: the tuple pattern has more elements than %s", subject.typ))
			return "", nil
		}
		addr := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, i32 0, i32 %d",
			addr, g.llType(subject.typ), base, i)
		elem := &value{text: addr, ll: g.llPass(elems[i]), typ: elems[i], lvalue: true, inline: true}
		sub, subBinds := g.patternTest(module, child, elem)
		binds = append(binds, subBinds...)
		if sub == "" {
			continue
		}
		if cond == "" {
			cond = sub
			continue
		}
		reg := g.newReg()
		g.emit("  %s = and i1 %s, %s", reg, cond, sub)
		cond = reg
	}
	return cond, binds
}

// patternVariant lowers a pattern over one variant of an enum: the tag must
// select the variant, and every payload element must match its sub-pattern.
func (g *generator) patternVariant(module string, pat *zyxc.Node, subject *value) (string, []binding) {
	if len(pat.Payload) < 1 {
		g.fail(fmt.Errorf("llvmgen: a variant pattern needs a name"))
		return "", nil
	}
	typ := subject.typ
	if alias := g.tab.Alias(typ); alias != nil {
		typ = alias
	}
	name := pat.Payload[0]
	index, payload, ok := g.variantOf(typ, name)
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no variant %s", typ, name))
		return "", nil
	}
	base := g.addrText(subject)
	tag := g.newReg()
	g.emit("  %s = load %s, ptr %s", tag, g.tagType(g.tagSize(typ)), base)
	cond := g.newReg()
	g.emit("  %s = icmp eq %s %s, %d", cond, g.tagType(g.tagSize(typ)), tag, index)

	var binds []binding
	if len(payload) != 0 {
		union := g.newReg()
		g.emit("  %s = getelementptr inbounds %s, ptr %s, i32 0, i32 1", union, g.llType(typ), base)
		tuple := types.TupleOf(payload...)
		for i, pt := range payload {
			if i >= len(pat.Children) {
				g.fail(fmt.Errorf("llvmgen: %s.%s carries %d values, got %d", typ, name, len(payload), len(pat.Children)))
				return "", nil
			}
			elem := g.newReg()
			g.emit("  %s = getelementptr inbounds %s, ptr %s, i32 0, i32 %d", elem, g.llType(tuple), union, i)
			sub := &value{text: elem, ll: g.llPass(pt), typ: pt, lvalue: true, inline: true}
			subCond, subBinds := g.patternTest(module, pat.Children[i], sub)
			binds = append(binds, subBinds...)
			if subCond == "" {
				continue
			}
			next := g.newReg()
			g.emit("  %s = and i1 %s, %s", next, cond, subCond)
			cond = next
		}
	}
	return cond, binds
}
