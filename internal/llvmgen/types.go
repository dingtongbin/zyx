// Type resolution and layout for the backend.

package llvmgen

import (
	"fmt"
	"strings"

	"zyx/internal/layout"
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// A Program is the input of the backend: the trees of every package that
// takes part in the build, in dependency order, after monomorphization. The
// backend reads only the trees and the runtime header; it never reads
// source and never resolves a name.
type Program struct {
	// Entry is the import path of the package that holds main.
	Entry string
	// Order lists the import paths, dependencies before dependents.
	Order []string
	// Files maps an import path to its tree.
	Files map[string]*zyxc.File
}

// A typeTable answers the layout questions the backend asks and resolves
// the type text of the intermediate representation. Every fact comes from
// the tree.
type typeTable struct {
	program *Program
	// quals maps a package qualifier, such as server, onto its import
	// path.
	quals map[string]string
	// decls maps a symbol id onto its declaration node.
	decls map[string]*zyxc.Node
	// modules maps a symbol id onto the import path that declares it.
	modules map[string]string
}

// newTypeTable indexes the declarations of every tree.
func newTypeTable(p *Program) *typeTable {
	t := &typeTable{
		program: p,
		quals:   map[string]string{},
		decls:   map[string]*zyxc.Node{},
		modules: map[string]string{},
	}
	for _, path := range p.Order {
		file := p.Files[path]
		if file == nil {
			continue
		}
		for _, decl := range file.Decls {
			if decl.Kind == "import" {
				t.quals[qualifier(decl.Payload[0])] = decl.Payload[0]
			}
		}
		for _, decl := range file.Decls {
			t.index(decl, path)
		}
	}
	return t
}

// qualifier returns the package qualifier of an import path, which is its
// last segment.
func qualifier(path string) string { return zyxc.PackageQualifier(path) }

// index records the declarations of one node and of its children.
func (t *typeTable) index(n *zyxc.Node, module string) {
	switch n.Kind {
	case "struct", "extern-struct", "enum", "interface", "alias":
		if len(n.Payload) >= 2 {
			t.bind(n.Payload[1], n, module)
		}
	case "extern-union":
		if len(n.Payload) >= 1 {
			t.bind(module+"."+n.Payload[0], n, module)
		}
	case "func":
		if len(n.Payload) >= 3 {
			t.bind(n.Payload[2], n, module)
		}
	case "const":
		if len(n.Payload) >= 2 {
			t.bind(n.Payload[1], n, module)
		}
	case "var":
		for i := 0; i+1 < len(n.Payload); i += 2 {
			t.bind(n.Payload[i+1], n, module)
		}
	}
	for _, child := range n.Children {
		switch child.Kind {
		case "impl", "struct", "extern-struct", "enum", "func", "const":
			t.index(child, module)
		}
	}
}

// bind records one declaration.
func (t *typeTable) bind(id string, n *zyxc.Node, module string) {
	if id == "" {
		return
	}
	t.decls[id] = n
	t.modules[id] = module
}

// ImportPath returns the import path a package qualifier names.
func (t *typeTable) ImportPath(qual string) (string, bool) {
	path, ok := t.quals[qual]
	return path, ok
}

// KindOf returns the kind of type a declaration introduces.
func (t *typeTable) KindOf(id string) (types.Kind, error) {
	node := t.decls[id]
	if node == nil {
		return types.Invalid, fmt.Errorf("llvmgen: no declaration for type %s", id)
	}
	switch node.Kind {
	case "struct":
		return types.KindStruct, nil
	case "extern-struct":
		return types.KindExternStruct, nil
	case "extern-union":
		return types.KindUnion, nil
	case "enum":
		return types.KindEnum, nil
	case "interface":
		return types.KindInterface, nil
	case "alias":
		if child := node.First("type"); child != nil && len(child.Payload) > 0 {
			inner, err := t.parse(t.modules[firstSymID(node)], child.Payload[0])
			if err != nil {
				return types.Invalid, err
			}
			return inner.Kind, nil
		}
		return types.Invalid, fmt.Errorf("llvmgen: alias %s has no target", node.Payload[0])
	}
	return types.Invalid, fmt.Errorf("llvmgen: %s is not a type declaration", node.Kind)
}

// Fields returns the fields of a struct, extern struct, or union, which the
// layout algorithm needs.
func (t *typeTable) Fields(ty *types.Type) ([]layout.Field, bool) {
	node := t.decls[ty.ID]
	if node == nil {
		return nil, false
	}
	switch node.Kind {
	case "struct", "extern-struct", "extern-union":
	default:
		return nil, false
	}
	module := t.modules[ty.ID]
	var out []layout.Field
	for _, child := range node.Children {
		if child.Kind != "field-decl" || len(child.Payload) < 2 {
			continue
		}
		ft, err := t.parse(module, child.Payload[1])
		if err != nil {
			return nil, false
		}
		out = append(out, layout.Field{Name: child.Payload[0], Type: ft})
	}
	return out, true
}

// Variants returns the variants of an enum, which the layout algorithm
// needs.
func (t *typeTable) Variants(ty *types.Type) ([]layout.Variant, bool) {
	node := t.decls[ty.ID]
	if node == nil || node.Kind != "enum" {
		return nil, false
	}
	module := t.modules[ty.ID]
	var out []layout.Variant
	for _, child := range node.Children {
		if child.Kind != "variant" {
			continue
		}
		v := layout.Variant{Name: child.Payload[0]}
		if typesNode := child.First("type"); typesNode != nil && len(typesNode.Payload) > 0 {
			payload, err := t.parse(module, typesNode.Payload[0])
			if err != nil || payload.Kind != types.KindTuple {
				return nil, false
			}
			v.Payload = payload.Elems
		}
		out = append(out, v)
	}
	return out, true
}

// parse turns the canonical type text back into a type.
func (t *typeTable) parse(module, text string) (*types.Type, error) {
	return zyxc.ParseType(module, text, t)
}

// firstSymID returns the symbol id a type declaration carries.
func firstSymID(node *zyxc.Node) string {
	if len(node.Payload) >= 2 {
		return node.Payload[1]
	}
	return ""
}

// Alias returns the type an alias declaration stands for, or nil when the
// declaration is not an alias.
func (t *typeTable) Alias(ty *types.Type) *types.Type {
	node := t.decls[ty.ID]
	if node == nil || node.Kind != "alias" {
		return nil
	}
	child := node.First("type")
	if child == nil || len(child.Payload) == 0 {
		return nil
	}
	inner, err := t.parse(t.modules[ty.ID], child.Payload[0])
	if err != nil {
		return nil
	}
	return inner
}

// decl returns the declaration node of a symbol id.
func (t *typeTable) decl(id string) *zyxc.Node { return t.decls[id] }

// module returns the import path that declares a symbol id.
func (t *typeTable) module(id string) string { return t.modules[id] }

// typeParamsOf returns the generic parameters of a declaration, in order.
// A type parameter is its own kind, so a function parameter can never be
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

// isTypeParam reports whether a node declares a generic parameter. It is its
// own kind, so a function parameter can never be mistaken for one.
func isTypeParam(n *zyxc.Node) bool { return n.Kind == "type-param" }

// moduleOf returns the symbol id of the declaration that owns a member
// symbol, which is everything before its last dot.
func moduleOf(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 {
		return id[:i]
	}
	return ""
}

// named returns the named type of a symbol id.
func (t *typeTable) named(id, qual, name string) (*types.Type, error) {
	kind, err := t.KindOf(id)
	if err != nil {
		return nil, err
	}
	return types.Named(kind, moduleOf(id), qual, name, 0), nil
}
