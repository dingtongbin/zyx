// Package mono monomorphizes generic definitions with runtime instances.
// It reads the intermediate representation of a whole program, gives every
// use of a generic definition a concrete instance of its own, and rewrites
// every reference to name that instance. The backend then sees only
// concrete declarations.
package mono

import (
	"fmt"
	"strings"

	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// A Program is the input and the output of monomorphization: the trees of
// every package in dependency order, with the instances appended to the
// packages that declare them.
type Program struct {
	// Target is the target triple, which takes part in an instance
	// signature.
	Target string
	// Entry is the import path the build started from.
	Entry string
	// Order lists the import paths, dependencies before dependents.
	Order []string
	// Files maps an import path to its tree.
	Files map[string]*zyxc.File
}

// Instantiate rewrites a program so that every generic definition has one
// instance per use, and returns how many instances it created.
func Instantiate(p *Program) (int, error) {
	m := &instantiator{
		p:       p,
		imports: map[string]string{},
		decls:   map[string]*zyxc.Node{},
		modules: map[string]string{},
		queued:  map[string]bool{},
	}
	m.index()
	for _, path := range p.Order {
		file := p.Files[path]
		if file == nil {
			continue
		}
		// A generic declaration is lowered once per instance, so its own
		// text waits for the instance to substitute and it leaves the tree.
		kept := file.Decls[:0]
		for _, decl := range file.Decls {
			if generic(decl) {
				continue
			}
			m.rewrite(decl, path)
			kept = append(kept, decl)
		}
		file.Decls = kept
		file.Exports = zyxc.ExportsHash(file.Decls)
	}
	for len(m.pending) > 0 {
		req := m.pending[0]
		m.pending = m.pending[1:]
		if err := m.instantiate(req); err != nil {
			return m.count, err
		}
	}
	return m.count, nil
}

// instantiator carries the state of one program's monomorphization.
type instantiator struct {
	p *Program
	// imports maps a package qualifier onto its import path.
	imports map[string]string
	// decls maps a symbol id onto its declaration node.
	decls map[string]*zyxc.Node
	// modules maps a symbol id onto the package that declares it.
	modules map[string]string
	// pending holds the instances still to build, in request order.
	pending []request
	// queued records the instance ids already requested or built.
	queued map[string]bool
	count  int
}

// A request is one instance to build: a generic declaration and the type
// arguments one use of it supplies.
type request struct {
	decl   *zyxc.Node
	module string
	id     string
	args   []*types.Type
}

// index records every declaration and every import of the program.
func (m *instantiator) index() {
	for _, path := range m.p.Order {
		file := m.p.Files[path]
		if file == nil {
			continue
		}
		for _, decl := range file.Decls {
			if decl.Kind == "import" {
				m.imports[zyxc.PackageQualifier(decl.Payload[0])] = decl.Payload[0]
			}
		}
		for _, decl := range file.Decls {
			m.bind(decl, path)
		}
	}
}

// bind records one declaration and its members.
func (m *instantiator) bind(n *zyxc.Node, module string) {
	switch n.Kind {
	case "struct", "extern-struct", "enum", "interface", "alias":
		m.record(payloadAt(n, 1), n, module)
	case "extern-union":
		m.record(module+"."+payloadAt(n, 0), n, module)
	case "func":
		m.record(payloadAt(n, 2), n, module)
	case "const":
		m.record(payloadAt(n, 1), n, module)
	case "var":
		for i := 0; i+1 < len(n.Payload); i += 2 {
			m.record(n.Payload[i+1], n, module)
		}
	}
	for _, child := range n.Children {
		switch child.Kind {
		case "impl", "struct", "extern-struct", "enum", "func", "const":
			m.bind(child, module)
		}
	}
}

// record stores one declaration.
func (m *instantiator) record(id string, n *zyxc.Node, module string) {
	if id == "" {
		return
	}
	m.decls[id] = n
	m.modules[id] = module
}

// payloadAt returns a payload token, or the empty string.
func payloadAt(n *zyxc.Node, i int) string {
	if i < len(n.Payload) {
		return n.Payload[i]
	}
	return ""
}

// ImportPath returns the import path a package qualifier names.
func (m *instantiator) ImportPath(qual string) (string, bool) {
	path, ok := m.imports[qual]
	return path, ok
}

// KindOf returns the kind of type a declaration introduces.
func (m *instantiator) KindOf(id string) (types.Kind, error) {
	node := m.decls[id]
	if node == nil {
		return types.Invalid, fmt.Errorf("mono: no declaration for type %s", id)
	}
	switch node.Kind {
	case "struct", "alias":
		return types.KindStruct, nil
	case "extern-struct":
		return types.KindExternStruct, nil
	case "extern-union":
		return types.KindUnion, nil
	case "enum":
		return types.KindEnum, nil
	case "interface":
		return types.KindInterface, nil
	}
	return types.Invalid, fmt.Errorf("mono: %s is not a type declaration", node.Kind)
}

// generic reports whether a declaration carries type parameters.
func generic(n *zyxc.Node) bool {
	return len(typeParamsOf(n)) != 0
}

// typeParamsOf returns the type parameters of a declaration, in order. A
// type parameter is its own kind, so a function parameter can never be
// mistaken for one.
func typeParamsOf(n *zyxc.Node) []*zyxc.Node {
	var out []*zyxc.Node
	for _, child := range n.Children {
		if child.Kind != "type-param" {
			break
		}
		out = append(out, child)
	}
	return out
}

// isTypeParam reports whether a node declares a generic parameter.
func isTypeParam(n *zyxc.Node) bool { return n.Kind == "type-param" }

// paramScope returns the type parameters of a declaration as the scope a
// type text of that declaration is read in.
func paramScope(n *zyxc.Node) map[string]*types.Type {
	scope := map[string]*types.Type{}
	for _, tp := range typeParamsOf(n) {
		scope[tp.Payload[0]] = types.Param(tp.Payload[1], tp.Payload[0])
	}
	return scope
}

// rewrite rewrites one declaration: its references and its type texts.
// References come first, because a reference reads the annotation of its
// receiver to find the instance it names, and that annotation is still the
// written type at this point.
func (m *instantiator) rewrite(n *zyxc.Node, module string) {
	if n == nil {
		return
	}
	m.rewriteRefs(n, module)
	zyxc.EachType(n, func(text string) string { return m.rewriteTypeText(module, text) })
}

// rewriteRefs rewrites the symbol ids a subtree references.
func (m *instantiator) rewriteRefs(n *zyxc.Node, module string) {
	if n == nil {
		return
	}
	// A call settles the type arguments of its callee from its own
	// arguments, so the callee is rewritten with the call in hand. The
	// callee is visited again below, where the second visit finds nothing
	// left to do.
	if callee := n.First("callee"); callee != nil {
		inner := callee
		if len(callee.Children) == 1 {
			inner = callee.Children[0]
		}
		m.rewriteOne(module, n, inner)
	}
	if slot, ok := zyxc.SymbolSlot(n.Kind); ok && slot < len(n.Payload) {
		m.rewriteOne(module, n, n)
	}
	for _, child := range n.Children {
		m.rewriteRefs(child, module)
	}
}

// rewriteOne rewrites the reference a node holds, reading the type
// arguments from call when the node is that call's callee.
func (m *instantiator) rewriteOne(module string, call, n *zyxc.Node) {
	slot, ok := zyxc.SymbolSlot(n.Kind)
	if !ok || slot >= len(n.Payload) {
		return
	}
	if id := m.reference(module, call, n, n.Payload[slot]); id != "" {
		n.Payload[slot] = id
	}
}

// rewriteTypeText rewrites one type text. A text the build cannot settle,
// such as the bare parameter of a generic declaration, is left alone.
func (m *instantiator) rewriteTypeText(module, text string) string {
	t, err := zyxc.ParseType(module, text, m)
	if err != nil {
		return ""
	}
	return zyxc.TypeText(m.instantiateType(t), module)
}

// instantiateType returns a type in which every generic use is replaced by
// the symbol of the instance that serves it.
func (m *instantiator) instantiateType(t *types.Type) *types.Type {
	if t == nil {
		return nil
	}
	switch t.Kind {
	case types.KindTuple, types.KindFunc:
		out := *t
		out.Elems = make([]*types.Type, len(t.Elems))
		for i, e := range t.Elems {
			out.Elems[i] = m.instantiateType(e)
		}
		out.Result = m.instantiateType(t.Result)
		return &out
	case types.KindArray, types.KindSlice, types.KindChan:
		out := *t
		out.Elem = m.instantiateType(t.Elem)
		return &out
	}
	if !t.IsNamed() || len(t.Args) == 0 {
		return t
	}
	args := make([]*types.Type, len(t.Args))
	for i, a := range t.Args {
		args[i] = m.instantiateType(a)
	}
	decl := m.decls[t.ID]
	if decl == nil || !generic(decl) {
		out := *t
		out.Args = args
		return &out
	}
	kind, err := m.KindOf(t.ID)
	if err != nil {
		kind = types.KindStruct
	}
	id := m.request(decl, t.Pkg, t.Name, args)
	return types.Named(kind, t.Pkg, "", memberOf(id), 0)
}

// request records that an instance is needed and returns its symbol id.
func (m *instantiator) request(decl *zyxc.Node, pkg, name string, args []*types.Type) string {
	id := pkg + "." + instanceName(name, args, m.p.Target)
	if !m.queued[id] {
		m.queued[id] = true
		m.pending = append(m.pending, request{decl: decl, module: pkg, id: id, args: args})
	}
	return id
}

// memberOf returns the last segment of a symbol id, which is the name a
// declaration is written by inside its package.
func memberOf(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 {
		return id[i+1:]
	}
	return id
}

// ownerOf returns the symbol id of the declaration a member belongs to, or
// the empty string when the id names a top-level declaration.
func (m *instantiator) ownerOf(id string) string {
	for i := strings.LastIndex(id, "."); i >= 0; i = strings.LastIndex(id[:i], ".") {
		owner := id[:i]
		if node := m.decls[owner]; node != nil && len(node.Payload) >= 1 {
			switch node.Kind {
			case "struct", "extern-struct", "enum":
				return owner
			}
		}
	}
	return ""
}

// reference returns the symbol id a reference resolves to, or the empty
// string when the reference needs no rewrite. call is the call the node is
// the callee of, or the node itself.
func (m *instantiator) reference(module string, call, n *zyxc.Node, id string) string {
	decl := m.decls[id]
	if decl == nil {
		return ""
	}
	if generic(decl) {
		args, ok := m.typeArguments(module, call, n, decl)
		if !ok {
			return ""
		}
		return m.request(decl, m.modules[id], decl.Payload[0], args)
	}
	owner := m.ownerOf(id)
	if owner == "" || !generic(m.decls[owner]) {
		return ""
	}
	// A method of a generic type follows the instance its receiver names.
	recv := m.receiverType(module, n)
	if recv == nil {
		return ""
	}
	return m.instantiateType(recv).ID + "." + memberOf(id)
}

// receiverType returns the annotated type of a call's receiver, or nil when
// the node has no receiver.
func (m *instantiator) receiverType(module string, n *zyxc.Node) *types.Type {
	recv := n.First("recv")
	if recv == nil {
		return nil
	}
	inner := recv
	if len(recv.Children) == 1 {
		inner = recv.Children[0]
	}
	return m.parseAnnotated(module, inner)
}

// parseAnnotated reads the type a node is annotated with, or returns nil.
func (m *instantiator) parseAnnotated(module string, n *zyxc.Node) *types.Type {
	if n == nil {
		return nil
	}
	t := n.First("type")
	if t == nil || len(t.Payload) == 0 {
		return nil
	}
	parsed, err := zyxc.ParseType(module, t.Payload[0], m)
	if err != nil {
		return nil
	}
	return parsed
}

// typeArguments returns the type arguments one use of a generic declaration
// supplies. The callee of a call to a generic function carries the generic
// signature rather than the instantiated one, so the arguments follow from
// the annotated types of the arguments as well.
func (m *instantiator) typeArguments(module string, call, n *zyxc.Node, decl *zyxc.Node) ([]*types.Type, bool) {
	if actual := m.calleeType(module, n); actual != nil {
		if declared, ok := m.declaredType(decl); ok {
			if bindings, ok := types.Bindings(declared, actual); ok {
				if args, ok := m.arguments(decl, bindings); ok {
					return args, true
				}
			}
		}
	}
	return m.argumentsFromArgs(module, call, decl)
}

// argumentsFromArgs binds the type parameters of a declaration against the
// annotated types of a call's arguments.
func (m *instantiator) argumentsFromArgs(module string, call *zyxc.Node, decl *zyxc.Node) ([]*types.Type, bool) {
	declModule := m.modules[symIDOf(decl)]
	scope := paramScope(decl)
	bindings := map[string]*types.Type{}
	var params, args []*zyxc.Node
	for _, child := range decl.Children {
		if child.Kind == "param" && !isTypeParam(child) {
			params = append(params, child)
		}
	}
	for _, child := range call.Children {
		if child.Kind == "arg" {
			args = append(args, child)
		}
	}
	for i, param := range params {
		if i >= len(args) {
			break
		}
		want, err := zyxc.ParseTypeWith(declModule, payloadAt(param, 1), m, scope)
		if err != nil {
			return nil, false
		}
		actual := m.argumentType(module, args[i])
		if actual == nil {
			continue
		}
		partial, ok := types.Bindings(want, actual)
		if !ok {
			return nil, false
		}
		for id, bound := range partial {
			bindings[id] = bound
		}
	}
	return m.arguments(decl, bindings)
}

// argumentType returns the annotated type of a call argument.
func (m *instantiator) argumentType(module string, arg *zyxc.Node) *types.Type {
	inner := arg
	if len(arg.Children) == 1 {
		inner = arg.Children[0]
	}
	return m.parseAnnotated(module, inner)
}

// arguments returns the type arguments of a declaration from a binding of
// its parameters.
func (m *instantiator) arguments(decl *zyxc.Node, bindings map[string]*types.Type) ([]*types.Type, bool) {
	var args []*types.Type
	for _, tp := range typeParamsOf(decl) {
		arg, ok := bindings[tp.Payload[1]]
		if !ok {
			return nil, false
		}
		args = append(args, arg)
	}
	return args, true
}

// calleeType returns the instantiated type of a call's callee. A reference
// node carries the callee's own type, and a call that reaches through a
// qualifier or a receiver carries it in the `callee-type` child the front
// end recorded.
func (m *instantiator) calleeType(module string, n *zyxc.Node) *types.Type {
	if t := n.First("callee-type"); t != nil && len(t.Payload) > 0 {
		if parsed, err := zyxc.ParseType(module, t.Payload[0], m); err == nil {
			return parsed
		}
	}
	if own := m.parseAnnotated(module, n); own != nil && own.Kind == types.KindFunc {
		return own
	}
	if callee := n.First("callee"); callee != nil {
		inner := callee
		if len(callee.Children) == 1 {
			inner = callee.Children[0]
		}
		if t := m.parseAnnotated(module, inner); t != nil {
			return t
		}
	}
	return nil
}

// declaredType returns the type a function declaration writes, with its
// type parameters left in place.
func (m *instantiator) declaredType(decl *zyxc.Node) (*types.Type, bool) {
	module := m.modules[symIDOf(decl)]
	scope := paramScope(decl)
	var params []*types.Type
	for _, child := range decl.Children {
		if child.Kind != "param" || isTypeParam(child) {
			continue
		}
		t, err := zyxc.ParseTypeWith(module, payloadAt(child, 1), m, scope)
		if err != nil {
			return nil, false
		}
		params = append(params, t)
	}
	var result *types.Type
	if r := decl.First("result"); r != nil && len(r.Payload) > 0 {
		t, err := zyxc.ParseTypeWith(module, r.Payload[0], m, scope)
		if err != nil {
			return nil, false
		}
		result = t
	}
	return types.FuncOf(result, params...), true
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
