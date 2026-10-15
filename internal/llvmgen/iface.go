// Interfaces: the method table a concrete type presents for an interface, the
// pair an interface value is made of, and the check that asks which type a
// value carries. The table gives the type its identity, so the check is a
// pointer comparison rather than a scan.

package llvmgen

import (
	"fmt"
	"strings"

	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// ifaceTable returns the name of the method table a concrete type presents
// for an interface, emitting the table on first use.
func (g *generator) ifaceTable(iface, concrete *types.Type) string {
	key := concrete.ID + "|" + iface.ID
	if name, ok := g.m.ifaceTables[key]; ok {
		return name
	}
	if !byAddress(concrete) {
		// The table passes the value it carries as the receiver, which is the
		// address of the object. A value that fits in a register would need a
		// thunk of its own.
		g.fail(fmt.Errorf("llvmgen: %s crosses an interface as a register, which is not lowered", concrete))
		return ""
	}
	name := fmt.Sprintf("@zyx.itable.%d", len(g.m.ifaceTables))
	g.m.ifaceTables[key] = name
	sigs := g.ifaceSigs(iface)
	entries := make([]string, 0, len(sigs))
	for _, sig := range sigs {
		method := g.implMethod(concrete, iface, sig.Payload[0])
		if method == "" {
			g.fail(fmt.Errorf("llvmgen: %s does not implement %s.%s", concrete, iface, sig.Payload[0]))
			return ""
		}
		entries = append(entries, "ptr @"+llvmName(method))
	}
	if len(entries) == 0 {
		g.fail(fmt.Errorf("llvmgen: %s declares no method", iface))
		return ""
	}
	g.m.ifaceDefs = append(g.m.ifaceDefs,
		fmt.Sprintf("%s = internal constant [%d x ptr] [%s]\n", name, len(entries), strings.Join(entries, ", ")))
	return name
}

// ifaceSigs returns the signatures of an interface, in declaration order,
// which is the order its method table follows.
func (g *generator) ifaceSigs(iface *types.Type) []*zyxc.Node {
	node := g.tab.decl(iface.ID)
	if node == nil {
		return nil
	}
	var out []*zyxc.Node
	for _, child := range node.Children {
		if child.Kind == "sig" {
			out = append(out, child)
		}
	}
	return out
}

// implMethod returns the symbol of the method a type's impl block declares for
// one signature of an interface.
func (g *generator) implMethod(concrete, iface *types.Type, name string) string {
	node := g.tab.decl(concrete.ID)
	if node == nil {
		return ""
	}
	for _, child := range node.Children {
		if child.Kind != "impl" || !g.implIs(child, iface) {
			continue
		}
		for _, fn := range child.Children {
			if fn.Kind == "func" && len(fn.Payload) >= 3 && fn.Payload[0] == name {
				return fn.Payload[2]
			}
		}
	}
	return ""
}

// implIs reports whether an impl block names an interface. The block writes
// the interface's symbol, so the two are compared as symbols rather than by
// spelling.
func (g *generator) implIs(impl *zyxc.Node, iface *types.Type) bool {
	return len(impl.Payload) > 0 && impl.Payload[0] == iface.ID
}

// ifaceValue builds the pair an interface value is made of: the method table
// of the concrete type and the value itself. A value that lives in a register
// is copied into the region, because the interface value outlives the
// expression that produced it.
func (g *generator) ifaceValue(iface *types.Type, v *value) *value {
	if v.typ.Kind == types.KindInterface {
		return v
	}
	table := g.ifaceTable(iface, v.typ)
	if table == "" {
		return nil
	}
	data := g.addrText(v)
	if !byAddress(v.typ) {
		slot := g.allocate(v.typ)
		g.storeInto(slot, v.typ, v, false)
		data = slot
	}
	first := g.newReg()
	g.emit("  %s = insertvalue { ptr, ptr } undef, ptr %s, 0", first, table)
	out := g.newReg()
	g.emit("  %s = insertvalue { ptr, ptr } %s, ptr %s, 1", out, first, data)
	return &value{text: out, ll: "{ ptr, ptr }", typ: iface}
}

// isCheck lowers `x is T`: the value carries the method table that T presents
// for the interface x has.
func (g *generator) isCheck(module string, n *zyxc.Node) *value {
	operand := g.load(g.expr(module, g.wrapped(n, "operand"), nil))
	if operand == nil {
		return nil
	}
	if operand.typ.Kind != types.KindInterface {
		g.fail(fmt.Errorf("llvmgen: is needs an interface value, got %s", operand.typ))
		return nil
	}
	if len(n.Payload) == 0 {
		g.fail(fmt.Errorf("llvmgen: is needs a type"))
		return nil
	}
	target, err := g.tab.parse(module, n.Payload[0])
	if err != nil {
		g.fail(err)
		return nil
	}
	table := g.ifaceTable(operand.typ, target)
	if table == "" {
		return nil
	}
	carried := g.newReg()
	g.emit("  %s = extractvalue { ptr, ptr } %s, 0", carried, operand.text)
	reg := g.newReg()
	g.emit("  %s = icmp eq ptr %s, %s", reg, carried, table)
	return &value{text: reg, ll: "i1", typ: types.Bool}
}

// ifaceCall lowers a call through an interface. The method's position in the
// interface selects an entry of the table, and the value the interface carries
// becomes the receiver, which the signature writes as its first parameter.
func (g *generator) ifaceCall(module string, n *zyxc.Node, recv *value, name string) *value {
	sigs := g.ifaceSigs(recv.typ)
	index := -1
	for i, sig := range sigs {
		if len(sig.Payload) > 0 && sig.Payload[0] == name {
			index = i
		}
	}
	if index < 0 {
		g.fail(fmt.Errorf("llvmgen: %s has no method %s", recv.typ, name))
		return nil
	}
	sig := g.typeOfNode(n.First("callee-type"), module)
	if sig == nil || sig.Kind != types.KindFunc {
		g.fail(fmt.Errorf("llvmgen: the signature of %s is not settled", name))
		return nil
	}
	recv = g.load(recv)
	table := g.newReg()
	g.emit("  %s = extractvalue { ptr, ptr } %s, 0", table, recv.text)
	data := g.newReg()
	g.emit("  %s = extractvalue { ptr, ptr } %s, 1", data, recv.text)
	slot := g.newReg()
	g.emit("  %s = getelementptr inbounds ptr, ptr %s, i64 %d", slot, table, index)
	fn := g.newReg()
	g.emit("  %s = load ptr, ptr %s", fn, slot)

	parts := []string{"ptr " + data}
	args := g.argNodes(n)
	for i, pt := range sig.Elems {
		if i >= len(args) {
			g.fail(fmt.Errorf("llvmgen: %s takes %d arguments, %d given", name, len(sig.Elems), len(args)))
			return nil
		}
		v := g.load(g.expr(module, args[i], pt))
		if v == nil {
			return nil
		}
		parts = append(parts, g.llPass(pt)+" "+v.text)
	}
	if sig.Result == nil {
		g.emit("  call void %s(%s)", fn, strings.Join(parts, ", "))
		return &value{ll: "void"}
	}
	retLL := g.llPass(sig.Result)
	reg := g.newReg()
	g.emit("  %s = call %s %s(%s)", reg, retLL, fn, strings.Join(parts, ", "))
	return &value{text: reg, ll: retLL, typ: sig.Result}
}
