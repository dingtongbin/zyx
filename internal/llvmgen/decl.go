// Declaration lowering.

package llvmgen

import (
	"fmt"
	"strings"

	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// A paramInfo describes one parameter of a function.
type paramInfo struct {
	name string
	typ  *types.Type
	// lls holds the LLVM types the parameter occupies: one for a language
	// parameter, and several where the C ABI splits a fat pointer.
	lls []string
	// regs holds the LLVM registers the parameter arrives in, parallel to
	// lls.
	regs []string
}

// global emits a top-level constant or variable.
func (g *generator) global(module string, n *zyxc.Node) {
	if n.Kind == "var" {
		for i := 0; i+1 < len(n.Payload); i += 2 {
			g.globalValue(module, n, n.Payload[i+1], n.Payload[i])
		}
		return
	}
	if len(n.Payload) < 2 {
		g.fail(fmt.Errorf("llvmgen: a top-level binding needs a name"))
		return
	}
	g.globalValue(module, n, n.Payload[1], n.Payload[0])
}

// globalValue emits one top-level binding. Its initializer is evaluated in
// a function of its own, which keeps a computed initializer out of the
// module's global section.
func (g *generator) globalValue(module string, n *zyxc.Node, id, name string) {
	typ := g.typeOfNode(n.First("type"), module)
	init := g.initChild(n)
	if init == nil {
		if typ == nil {
			g.fail(fmt.Errorf("llvmgen: the type of global %s is not settled", name))
			return
		}
		fmt.Fprintf(&g.m.funcs, "%s = internal global %s %s\n", llvmName(id), g.llType(typ), g.zero(typ))
		return
	}
	if typ == nil {
		g.fail(fmt.Errorf("llvmgen: the type of global %s is not settled", name))
		return
	}
	fmt.Fprintf(&g.m.funcs, "%s = internal global %s %s\n", llvmName(id), g.llType(typ), g.zero(typ))
	g.m.globalInits = append(g.m.globalInits, globalInit{module: module, id: id, typ: typ, init: init})
}

// A globalInit is a top-level initializer that runs before main.
type globalInit struct {
	module string
	id     string
	typ    *types.Type
	init   *zyxc.Node
}

// emitGlobalInits defines the function that initializes the top-level
// bindings whose initializer is not a constant. The entry point calls it
// before the language main runs.
func (g *generator) emitGlobalInits() {
	g.fn = &funcState{symbol: "@zyx.globals", vars: map[string]*varSlot{}}
	g.fn.entry = g.newLabel("globals")
	g.fn.ret = g.newLabel("globals.done")
	for _, init := range g.m.globalInits {
		v := g.expr(init.module, init.init, init.typ)
		if v == nil {
			continue
		}
		g.storeInto("@"+llvmName(init.id), init.typ, v)
	}
	g.terminator("  ret void")
	var out strings.Builder
	out.WriteString("define internal void @zyx.globals() {\n")
	out.WriteString(g.fn.entry + ":\n")
	out.WriteString(g.fn.allocas.String())
	out.WriteString(g.fn.body.String())
	out.WriteString("}\n")
	g.m.funcs.WriteString(out.String())
	g.fn = nil
}

// entryPoint emits the C main that runs the language main.
func (g *generator) entryPoint() {
	g.emitGlobalInits()
	file := g.prog.Files[g.prog.Entry]
	if file == nil {
		g.fail(fmt.Errorf("llvmgen: the entry package %s is missing", g.prog.Entry))
		return
	}
	var main *zyxc.Node
	for _, decl := range file.Decls {
		if decl.Kind != "func" {
			continue
		}
		for _, mark := range decl.Marks {
			if mark.Name == "entry" {
				main = decl
			}
		}
	}
	if main == nil {
		g.fail(fmt.Errorf("llvmgen: package %s has no main function", g.prog.Entry))
		return
	}
	if len(main.Payload) < 3 {
		g.fail(fmt.Errorf("llvmgen: the main function has no symbol"))
		return
	}
	if main.Payload[1] == "comptime" || main.Payload[1] == "extern" {
		g.fail(fmt.Errorf("llvmgen: main must be an ordinary function"))
		return
	}
	symbol := llvmName(main.Payload[2])
	g.declareRuntime("zyxrt_run_main", "i32", "ptr", "ptr")
	// The runtime runs the language main as the root coroutine, so it
	// reaches the program through a thunk of the runtime's own signature.
	fmt.Fprintf(&g.m.funcs, "define internal void @zyx.main.thunk(ptr %%arg) {\n")
	fmt.Fprintf(&g.m.funcs, "  call void @zyx.globals()\n")
	fmt.Fprintf(&g.m.funcs, "  call void @%s()\n", symbol)
	fmt.Fprintf(&g.m.funcs, "  ret void\n}\n")
	fmt.Fprintf(&g.m.funcs, "define i32 @main() {\n")
	fmt.Fprintf(&g.m.funcs, "  call i32 @zyxrt_run_main(ptr @zyx.main.thunk, ptr null)\n")
	fmt.Fprintf(&g.m.funcs, "  ret i32 0\n}\n")
}

// paramType returns the declared type of a function parameter, whose
// payload holds the name and then the type.
func (g *generator) paramType(n *zyxc.Node, module string) *types.Type {
	if len(n.Payload) < 2 {
		g.fail(fmt.Errorf("llvmgen: a parameter needs a type"))
		return nil
	}
	t, err := g.tab.parse(module, n.Payload[1])
	if err != nil {
		g.fail(err)
		return nil
	}
	return t
}

// receiverType returns the type of a method's receiver. A member's symbol
// id carries the symbol id of the type that declares it, so the declaring
// type is reachable from the member alone.
func (g *generator) receiverType(n *zyxc.Node) *types.Type {
	if len(n.Payload) < 3 {
		return nil
	}
	sym := n.Payload[2]
	owner := moduleOf(sym)
	if owner == "" || owner == sym {
		return nil
	}
	node := g.tab.decl(owner)
	if node == nil || len(node.Payload) < 1 {
		return nil
	}
	switch node.Kind {
	case "struct", "extern-struct", "enum":
	default:
		return nil
	}
	t, err := g.tab.resolve(owner, "", node.Payload[0], nil)
	if err != nil {
		return nil
	}
	return t
}

// signature returns the parameters and the result of a function, and the
// LLVM type of its result. A method's receiver is its first parameter.
func (g *generator) signature(module string, n *zyxc.Node) ([]paramInfo, *types.Type, string) {
	qual := n.Payload[1]
	cABI := qual == "extern" || qual == "c"
	var params []paramInfo
	if recv := g.receiverType(n); recv != nil {
		params = append(params, paramInfo{name: "self", typ: recv, lls: []string{g.llPass(recv)}})
	}
	for _, child := range n.Children {
		if child.Kind != "param" || isTypeParam(child) {
			continue
		}
		typ := g.paramType(child, module)
		if typ == nil {
			continue
		}
		name := "_"
		if len(child.Payload) > 0 {
			name = child.Payload[0]
		}
		p := paramInfo{name: name, typ: typ}
		if cABI {
			p.lls = g.abiArgs(typ)
		} else {
			p.lls = []string{g.llPass(typ)}
		}
		params = append(params, p)
	}
	var result *types.Type
	if r := n.First("result"); r != nil {
		result = g.typeOfNode(r, module)
	}
	retLL := "void"
	if result != nil {
		retLL = g.llPass(result)
	}
	return params, result, retLL
}

// isTypeParam reports whether a `param` node declares a generic parameter
// rather than a function parameter. A type parameter carries a symbol id,
// which holds the `$` that separates it from its declaration.
func isTypeParam(n *zyxc.Node) bool {
	return len(n.Payload) >= 2 && strings.Contains(n.Payload[1], "$")
}

// abiArgs returns the LLVM types the C ABI uses for one argument.
func (g *generator) abiArgs(t *types.Type) []string {
	switch t.Kind {
	case types.KindStr, types.KindSlice:
		return []string{"ptr", g.ptrInt()}
	case types.KindInterface:
		return []string{"ptr", "ptr"}
	}
	return []string{g.llPass(t)}
}

// declare emits an external function declaration.
func (g *generator) declare(symbol string, params []paramInfo, result *types.Type, retLL string) {
	if g.m.declared[symbol] {
		return
	}
	g.m.declared[symbol] = true
	var parts []string
	for _, p := range params {
		for _, ll := range p.lls {
			parts = append(parts, ll)
		}
	}
	fmt.Fprintf(&g.m.funcs, "declare %s @%s(%s)\n", retLL, llvmName(symbol), strings.Join(parts, ", "))
}

// function emits one function definition or declaration.
func (g *generator) function(module string, n *zyxc.Node) {
	if len(n.Payload) < 3 {
		return
	}
	name, qual, sym := n.Payload[0], n.Payload[1], n.Payload[2]
	if qual == "comptime" {
		// A compile-time function never reaches the object code; the front
		// end folds every call it can.
		return
	}
	symbol := sym
	if qual == "c" {
		// A function with the C ABI is reachable from C by its own name.
		symbol = name
	}
	params, result, retLL := g.signature(module, n)
	if qual == "extern" {
		g.declare(symbol, params, result, retLL)
		return
	}
	body := n.First("block")
	if body == nil {
		bodyExpr := funcBodyExpr(n)
		if bodyExpr == nil {
			g.declare(symbol, params, result, retLL)
			return
		}
		body = zyxc.New("block").Child(zyxc.New("return").Child(bodyExpr))
	}
	if g.m.defined[symbol] {
		return
	}
	g.m.defined[symbol] = true

	g.fn = &funcState{symbol: symbol, vars: map[string]*varSlot{}, result: result}
	g.fn.entry = g.newLabel("entry")
	g.fn.ret = g.newLabel("ret")

	// The parameters arrive in registers; every one is copied into a local
	// so that assignment and address-taking work uniformly. A method's
	// receiver is its first parameter and binds the name self.
	for i := range params {
		p := &params[i]
		for j := range p.lls {
			p.regs = append(p.regs, fmt.Sprintf("%%p%d_%d", i, j))
		}
		slot := g.alloca(p.typ)
		if len(p.lls) == 1 {
			g.emit("  store %s %s, ptr %s", p.lls[0], p.regs[0], slot)
		} else {
			// The C ABI split a fat pointer into a pointer and a length.
			g.storeFat(slot, p)
		}
		g.fn.vars[fmt.Sprintf("%s#%d", sym, i+1)] = &varSlot{alloca: slot, typ: p.typ}
	}

	g.stmt(module, body)

	if !g.fn.terminated {
		g.terminator("  br label %%%s", g.fn.ret)
	}
	g.label(g.fn.ret)
	if result == nil {
		g.emit("  ret void")
	} else {
		if g.fn.resultSlot == "" {
			g.fn.resultSlot = g.alloca(result)
		}
		reg := g.newReg()
		g.emit("  %s = load %s, ptr %s", reg, g.llPass(result), g.fn.resultSlot)
		g.emit("  ret %s %s", g.llPass(result), reg)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "define %s @%s(", retLL, llvmName(symbol))
	var parts []string
	for i := range params {
		for j, ll := range params[i].lls {
			parts = append(parts, ll+" "+params[i].regs[j])
		}
	}
	out.WriteString(strings.Join(parts, ", "))
	out.WriteString(") {\n")
	out.WriteString(g.fn.entry + ":\n")
	out.WriteString(g.fn.allocas.String())
	out.WriteString(g.fn.body.String())
	out.WriteString("}\n")
	g.m.funcs.WriteString(out.String())
	g.fn = nil
}

// funcBodyExpr returns the single expression body of a `=>` function.
func funcBodyExpr(n *zyxc.Node) *zyxc.Node {
	for _, child := range n.Children {
		switch child.Kind {
		case "param", "result":
			continue
		case "block":
			return nil
		default:
			return child
		}
	}
	return nil
}

// funcType returns the language function type of a function declaration.
func (g *generator) funcType(decl *zyxc.Node) *types.Type {
	module := g.tab.module(symIDOf(decl))
	var params []*types.Type
	for _, child := range decl.Children {
		if child.Kind != "param" || isTypeParam(child) {
			continue
		}
		if t := g.paramType(child, module); t != nil {
			params = append(params, t)
		}
	}
	var result *types.Type
	if r := decl.First("result"); r != nil {
		result = g.typeOfNode(r, module)
	}
	return types.FuncOf(result, params...)
}

// symIDOf returns the symbol id a declaration carries.
func symIDOf(n *zyxc.Node) string {
	if n.Kind == "func" && len(n.Payload) >= 3 {
		return n.Payload[2]
	}
	if len(n.Payload) >= 2 {
		return n.Payload[1]
	}
	return ""
}

// declareRuntime emits a runtime declaration once.
func (g *generator) declareRuntime(symbol, ret string, args ...string) {
	if g.m.declared[symbol] {
		return
	}
	g.m.declared[symbol] = true
	fmt.Fprintf(&g.m.funcs, "declare %s @%s(%s)\n", ret, symbol, strings.Join(args, ", "))
}
