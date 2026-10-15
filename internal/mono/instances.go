// Instance collection and instantiation.

package mono

import (
	"fmt"
	"strings"

	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// instantiate builds one instance of a generic declaration, appends it to
// the package that declares it, and rewrites it, which collects the further
// instances it needs.
func (m *instantiator) instantiate(req request) error {
	if len(req.args) != len(typeParamsOf(req.decl)) {
		return fmt.Errorf("mono: %s takes %d type arguments, %d given",
			req.decl.Payload[0], len(typeParamsOf(req.decl)), len(req.args))
	}
	instance := clone(req.decl)
	dropTypeParams(instance)
	rename(instance, req.id)
	m.substitute(instance, req.decl, req.args, req.module, req.id)
	m.bind(instance, req.module)
	m.rewrite(instance, req.module)
	m.appendDecl(req.module, instance)
	m.count++
	return nil
}

// appendDecl adds an instance to its package's tree.
func (m *instantiator) appendDecl(module string, n *zyxc.Node) {
	file := m.p.Files[module]
	if file == nil {
		// The instance belongs to a package the build did not load, which
		// only happens when a tree was dropped.
		file = &zyxc.File{Target: m.p.Target, Module: module}
		m.p.Files[module] = file
		m.p.Order = append(m.p.Order, module)
	}
	file.Decls = append(file.Decls, n)
}

// clone returns a deep copy of a node.
func clone(n *zyxc.Node) *zyxc.Node {
	if n == nil {
		return nil
	}
	out := &zyxc.Node{
		Kind:    n.Kind,
		Payload: append([]string(nil), n.Payload...),
		Pos:     n.Pos,
		Marks:   append([]zyxc.Mark(nil), n.Marks...),
	}
	for _, child := range n.Children {
		out.Children = append(out.Children, clone(child))
	}
	return out
}

// dropTypeParams removes the type parameters of an instance, which is a
// concrete declaration.
func dropTypeParams(n *zyxc.Node) {
	var kept []*zyxc.Node
	for _, child := range n.Children {
		if child.Kind == "param" && isTypeParam(child) {
			continue
		}
		kept = append(kept, child)
	}
	n.Children = kept
}

// rename gives an instance its own symbol id, and its members theirs.
func rename(n *zyxc.Node, id string) {
	switch n.Kind {
	case "func":
		if len(n.Payload) >= 3 {
			n.Payload[2] = id
		}
	case "extern-union":
		if len(n.Payload) >= 1 {
			n.Payload[0] = memberOf(id)
		}
	default:
		if len(n.Payload) >= 2 {
			n.Payload[1] = id
		}
	}
	// A member of an instance names the instance.
	for _, child := range n.Children {
		switch child.Kind {
		case "func":
			if len(child.Payload) >= 3 {
				child.Payload[2] = id + "." + child.Payload[0]
			}
		case "const":
			if len(child.Payload) >= 2 {
				child.Payload[1] = id + "." + child.Payload[0]
			}
		case "impl":
			for _, f := range child.Children {
				if f.Kind == "func" && len(f.Payload) >= 3 {
					f.Payload[2] = id + "." + f.Payload[0]
				}
			}
		}
	}
}

// substitute replaces the type parameters of a generic declaration with the
// type arguments of one instance, and gives the declaration's own name the
// name of the instance. The receiver of a method of a generic type is
// written with the generic name, which inside an instance names the
// instance.
func (m *instantiator) substitute(instance, decl *zyxc.Node, args []*types.Type, module, instanceID string) {
	scope := paramScope(decl)
	bindings := map[string]*types.Type{}
	for i, tp := range typeParamsOf(decl) {
		if i < len(args) {
			bindings[tp.Payload[1]] = args[i]
		}
	}
	selfID := symIDOf(decl)
	renameLocals(instance, memberSymbols(decl, instanceID))
	zyxc.EachType(instance, func(text string) string {
		t, err := zyxc.ParseTypeWith(module, text, m, scope)
		if err != nil {
			return ""
		}
		return zyxc.TypeText(renameSelf(types.Substitute(t, bindings), selfID, instanceID), module)
	})
}

// memberSymbols pairs the symbol id of a declaration and of each of its
// members with the id it takes in an instance.
func memberSymbols(decl *zyxc.Node, instanceID string) map[string]string {
	out := map[string]string{symIDOf(decl): instanceID}
	add := func(n *zyxc.Node) {
		if id := symIDOf(n); id != "" && len(n.Payload) >= 1 {
			out[id] = instanceID + "." + n.Payload[0]
		}
	}
	for _, child := range decl.Children {
		switch child.Kind {
		case "func", "const":
			add(child)
		case "impl":
			for _, f := range child.Children {
				if f.Kind == "func" {
					add(f)
				}
			}
		}
	}
	return out
}

// renameLocals gives the symbols of an instance the instance's name. A
// local symbol is the symbol of its function and the position of the
// declaration, so a method of an instance binds its own receiver.
func renameLocals(n *zyxc.Node, mapping map[string]string) {
	if n == nil {
		return
	}
	for i, token := range n.Payload {
		if to, ok := instanceSymbol(token, mapping); ok {
			n.Payload[i] = to
		}
	}
	for i := range n.Marks {
		if to, ok := instanceSymbol(n.Marks[i].Arg, mapping); ok {
			n.Marks[i].Arg = to
		}
	}
	for _, child := range n.Children {
		renameLocals(child, mapping)
	}
}

// instanceSymbol returns the symbol an instance declares for one of a
// declaration's symbols, whether the symbol names the declaration itself or
// a local of one of its functions.
func instanceSymbol(token string, mapping map[string]string) (string, bool) {
	if to, ok := mapping[token]; ok {
		return to, true
	}
	for from, to := range mapping {
		if strings.HasPrefix(token, from+"#") {
			return to + token[len(from):], true
		}
	}
	return "", false
}

// renameSelf returns a type in which the declaration's own name is the name
// of its instance.
func renameSelf(t *types.Type, selfID, instanceID string) *types.Type {
	if t == nil {
		return nil
	}
	switch t.Kind {
	case types.KindTuple, types.KindFunc:
		out := *t
		out.Elems = make([]*types.Type, len(t.Elems))
		for i, e := range t.Elems {
			out.Elems[i] = renameSelf(e, selfID, instanceID)
		}
		out.Result = renameSelf(t.Result, selfID, instanceID)
		return &out
	case types.KindArray, types.KindSlice, types.KindChan:
		out := *t
		out.Elem = renameSelf(t.Elem, selfID, instanceID)
		return &out
	}
	if !t.IsNamed() || t.ID != selfID {
		return t
	}
	return types.Named(t.Kind, t.Pkg, t.Qual, memberOf(instanceID), 0)
}
