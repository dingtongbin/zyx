// Call lowering and C ABI boundaries.

package llvmgen

import (
	"fmt"
	"strings"

	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// call lowers a call whose callee is an expression.
func (g *generator) call(module string, n *zyxc.Node) *value {
	callee := g.wrapped(n, "callee")
	if callee == nil {
		g.fail(fmt.Errorf("llvmgen: a call needs a callee"))
		return nil
	}
	sym := g.calleeSymbol(callee)
	if sym == "" {
		// The callee is not a named reference, so it is a value the call
		// reaches through: a lambda, or a parameter of function type.
		return g.indirectCall(module, n, callee)
	}
	if decl := g.tab.decl(sym); decl != nil && decl.Kind == "func" {
		return g.directCall(module, n, sym, nil)
	}
	if _, ok := g.fn.vars[sym]; ok {
		return g.indirectCall(module, n, callee)
	}
	g.fail(fmt.Errorf("llvmgen: %s: %s is not a function", n.Pos, sym))
	return nil
}

// indirectCall lowers a call through a function value. The signature the call
// records settles the parameter and result types, so the call needs no
// inference.
func (g *generator) indirectCall(module string, n *zyxc.Node, callee *zyxc.Node) *value {
	// A callee that is not a child of the call carries its own signature; a
	// call that reaches through a qualifier or a receiver records it in the
	// callee-type child instead.
	sig := g.typeOfNode(callee.First("type"), module)
	if sig == nil || sig.Kind != types.KindFunc {
		sig = g.typeOfNode(n.First("callee-type"), module)
	}
	if sig == nil || sig.Kind != types.KindFunc {
		g.fail(fmt.Errorf("llvmgen: the signature of the call to %s is not settled", callee.Kind))
		return nil
	}
	fn := g.load(g.expr(module, callee, nil))
	if fn == nil {
		return nil
	}
	args := g.argNodes(n)
	if len(args) != len(sig.Elems) {
		g.fail(fmt.Errorf("llvmgen: the call takes %d arguments, %d given", len(sig.Elems), len(args)))
		return nil
	}
	// A function value carries the function and a receiver, so the call
	// reaches the function with that receiver as its own first parameter.
	code := g.newReg()
	g.emit("  %s = extractvalue %s %s, 0", code, fn.ll, fn.text)
	recv := g.newReg()
	g.emit("  %s = extractvalue %s %s, 1", recv, fn.ll, fn.text)
	calleeText := code
	parts := []string{"ptr " + recv}
	for i, pt := range sig.Elems {
		v := g.conform(g.load(g.expr(module, args[i], pt)), pt)
		if v == nil {
			return nil
		}
		parts = append(parts, g.llPass(pt)+" "+v.text)
	}
	if sig.Result == nil {
		g.emit("  call void %s(%s)", calleeText, strings.Join(parts, ", "))
		return &value{ll: "void"}
	}
	retLL := g.llPass(sig.Result)
	reg := g.newReg()
	g.emit("  %s = call %s %s(%s)", reg, retLL, calleeText, strings.Join(parts, ", "))
	return &value{text: reg, ll: retLL, typ: sig.Result}
}

// calleeSymbol returns the symbol a callee expression names.
func (g *generator) calleeSymbol(callee *zyxc.Node) string {
	switch callee.Kind {
	case "name":
		if len(callee.Payload) >= 2 {
			return callee.Payload[1]
		}
	case "qual":
		if len(callee.Payload) >= 3 {
			return callee.Payload[2]
		}
	case "func-ref":
		if len(callee.Payload) >= 1 {
			return callee.Payload[0]
		}
	}
	return ""
}

// methodCall lowers a method call. The receiver arrives as the first
// parameter of the method.
func (g *generator) methodCall(module string, n *zyxc.Node) *value {
	if len(n.Payload) < 2 {
		g.fail(fmt.Errorf("llvmgen: a method call needs a symbol"))
		return nil
	}
	sym := n.Payload[1]
	recv := g.expr(module, g.wrapped(n, "recv"), nil)
	if recv == nil {
		return nil
	}
	if recv.typ.Kind == types.KindChan {
		// A channel is an opaque runtime handle: its operations have no
		// declaration in any tree.
		return g.chanCall(module, n, g.load(recv), n.Payload[0])
	}
	if recv.typ.Kind == types.KindContainer {
		// A container is an opaque runtime handle: its operations have no
		// declaration in any tree.
		return g.containerCall(module, n, g.load(recv), n.Payload[0])
	}
	if recv.typ.Kind == types.KindInterface {
		// An interface method is reached through the method table the value
		// carries.
		return g.ifaceCall(module, n, recv, n.Payload[0])
	}
	if n.Payload[0] == "Show" && g.builtinShow(recv.typ) {
		// The language gives a primitive or a str its Show from the fixed
		// table the backend owns, so the call appends the value to the
		// buffer it names rather than reaching a declaration.
		return g.builtinShowCall(module, n, g.load(recv))
	}
	if n.Payload[0] == "Eq" && equatable(recv.typ) {
		return g.builtinEqCall(module, n, g.load(recv))
	}
	if n.Payload[0] == "Hash" && hashable(recv.typ) {
		return g.builtinHashCall(module, n, g.load(recv))
	}
	if v, ok := g.intBuiltin(module, n, g.load(recv), n.Payload[0]); ok {
		return v
	}
	decl := g.tab.decl(sym)
	if decl == nil || decl.Kind != "func" {
		g.fail(fmt.Errorf("llvmgen: the method %s has no declaration (%s)", n.Payload[0], sym))
		return nil
	}
	return g.directCall(module, n, sym, recv)
}

// qualified lowers a reference to a declaration of another package, which
// is a call when it carries arguments and a global otherwise.
func (g *generator) qualified(module string, n *zyxc.Node) *value {
	if len(n.Payload) < 3 {
		g.fail(fmt.Errorf("llvmgen: a qualified name needs a package and a name"))
		return nil
	}
	sym := n.Payload[2]
	decl := g.tab.decl(sym)
	if decl == nil {
		g.fail(fmt.Errorf("llvmgen: no declaration for %s.%s", n.Payload[0], n.Payload[1]))
		return nil
	}
	if decl.Kind == "func" {
		return g.directCall(module, n, sym, nil)
	}
	typ := g.bindingType(module, decl, sym)
	if typ == nil {
		g.fail(fmt.Errorf("llvmgen: the type of %s.%s is not settled", n.Payload[0], n.Payload[1]))
		return nil
	}
	return &value{text: "@" + llvmName(sym), ll: g.llType(typ), typ: typ, lvalue: true}
}

// directCall lowers a call to a named function.
func (g *generator) directCall(module string, n *zyxc.Node, sym string, recv *value) *value {
	decl := g.tab.decl(sym)
	dm := g.tab.module(sym)
	qual := decl.Payload[1]
	cABI := qual == "extern" || qual == "c"

	var ptypes []*types.Type
	if recv := g.receiverType(decl); recv != nil {
		ptypes = append(ptypes, recv)
	}
	for _, child := range decl.Children {
		if child.Kind == "param" && !isTypeParam(child) {
			ptypes = append(ptypes, g.paramType(child, dm))
		}
	}
	var result *types.Type
	if r := decl.First("result"); r != nil {
		result = g.typeOfNode(r, dm)
	}

	var args []*value
	if recv != nil {
		args = append(args, recv)
	}
	for _, child := range n.Children {
		if child.Kind != "arg" {
			continue
		}
		var want *types.Type
		if len(args) < len(ptypes) {
			want = ptypes[len(args)]
		}
		v := g.expr(module, g.sole(child), want)
		if v == nil {
			return nil
		}
		if want != nil && want.Kind == types.KindInterface && v.typ.Kind != types.KindInterface {
			// A concrete argument becomes an interface value at the call.
			if v = g.ifaceValue(want, v); v == nil {
				return nil
			}
		}
		args = append(args, v)
	}
	if len(args) != len(ptypes) {
		g.fail(fmt.Errorf("llvmgen: %s takes %d arguments, %d given", sym, len(ptypes), len(args)))
		return nil
	}

	symbol := sym
	if qual == "c" {
		symbol = decl.Payload[0]
	}
	if qual == "extern" {
		params, res, retLL := g.signature(dm, decl)
		g.declare(symbol, params, res, retLL)
	}

	retLL := "void"
	if result != nil {
		retLL = g.llPass(result)
	}
	var parts []string
	for _, a := range args {
		if cABI {
			parts = append(parts, g.cArguments(a)...)
			continue
		}
		v := g.conform(g.load(a), a.typ)
		if v == nil {
			return nil
		}
		parts = append(parts, g.llPass(a.typ)+" "+v.text)
	}
	if result == nil {
		g.emit("  call void @%s(%s)", llvmName(symbol), strings.Join(parts, ", "))
		return &value{ll: "void"}
	}
	reg := g.newReg()
	g.emit("  %s = call %s @%s(%s)", reg, retLL, llvmName(symbol), strings.Join(parts, ", "))
	return &value{text: reg, ll: retLL, typ: result}
}

// cArguments renders one argument in the form the C ABI uses: a fat pointer
// crosses as its address and its length.
func (g *generator) cArguments(a *value) []string {
	v := g.load(a)
	lls := g.abiArgs(a.typ)
	if len(lls) == 1 {
		return []string{lls[0] + " " + v.text}
	}
	parts := make([]string, 0, len(lls))
	for i, ll := range lls {
		reg := g.newReg()
		g.emit("  %s = extractvalue %s %s, %d", reg, v.ll, v.text, i)
		parts = append(parts, ll+" "+reg)
	}
	return parts
}

// cCall lowers a call into the C namespace. The C namespace carries no
// declaration, so the result is the void of a call whose value the program
// does not use.
func (g *generator) cCall(module string, n *zyxc.Node) *value {
	if len(n.Payload) == 0 || n.Payload[0] == "" {
		g.fail(fmt.Errorf("llvmgen: a C call needs a name"))
		return nil
	}
	symbol := n.Payload[0]
	var parts []string
	var types []string
	for _, child := range n.Children {
		if child.Kind != "arg" {
			continue
		}
		v := g.expr(module, g.sole(child), nil)
		if v == nil {
			return nil
		}
		args := g.cArguments(v)
		parts = append(parts, args...)
		for _, a := range args {
			ll, _, _ := strings.Cut(a, " ")
			types = append(types, ll)
		}
	}
	result := g.typeOfNode(n.First("type"), module)
	ret := "void"
	if result != nil {
		var ok bool
		ret, ok = g.cReturn(result)
		if !ok {
			return nil
		}
	}
	if !g.m.declared[symbol] {
		// The C namespace carries no declaration, so the first call of a
		// symbol settles the shape of the one declaration the module
		// writes for it. The check holds every call to the header's own
		// signature, so the calls agree on the shape.
		g.m.declared[symbol] = true
		fmt.Fprintf(&g.m.funcs, "declare %s @%s(%s)\n", ret, llvmName(symbol), strings.Join(types, ", "))
	}
	if result == nil {
		g.emit("  call void @%s(%s)", llvmName(symbol), strings.Join(parts, ", "))
		return &value{ll: "void"}
	}
	reg := g.newReg()
	g.emit("  %s = call %s @%s(%s)", reg, ret, llvmName(symbol), strings.Join(parts, ", "))
	return &value{text: reg, ll: ret, typ: result}
}

// cReturn returns the LLVM type of the value a C function answers with. A
// scalar and an address cross as themselves; a record by value needs the C
// ABI of the target, which is not lowered.
func (g *generator) cReturn(t *types.Type) (string, bool) {
	switch t.Kind {
	case types.KindBool, types.KindChar, types.KindRaw, types.KindFunc:
		return g.llType(t), true
	}
	if t.IsNumeric() {
		return g.llType(t), true
	}
	g.fail(fmt.Errorf("llvmgen: a C function that answers with %s is not lowered", t))
	return "", false
}

// panicCall lowers the built-in panic.
func (g *generator) panicCall(module string, n *zyxc.Node) *value {
	g.declareRuntime("zyxrt_panic", "void", "ptr", "i64")
	message := g.wrapped(n, "msg")
	if message == nil {
		g.fail(fmt.Errorf("llvmgen: panic needs a message"))
		return nil
	}
	v := g.expr(module, message, types.Str)
	if v == nil {
		return nil
	}
	args := g.cArguments(v)
	if len(args) != 2 {
		g.fail(fmt.Errorf("llvmgen: panic takes one message"))
		return nil
	}
	g.emit("  call void @zyxrt_panic(%s)", strings.Join(args, ", "))
	g.emit("  unreachable")
	g.fn.terminated = true
	return &value{ll: "void"}
}
