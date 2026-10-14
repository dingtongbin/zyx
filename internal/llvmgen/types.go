// Type resolution and layout for the backend.

package llvmgen

import (
	"fmt"
	"strconv"
	"strings"

	"zyx/internal/layout"
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// A Program is the input of the backend: the trees of every package that
// takes part in the build, in dependency order. The backend reads only the
// trees and the runtime header; it never reads source and never resolves a
// name.
type Program struct {
	// Entry is the import path of the package that holds main.
	Entry string
	// Order lists the import paths, dependencies before dependents.
	Order []string
	// Files maps an import path to its tree.
	Files map[string]*zyxc.File
}

// A typeTable resolves the type text of the intermediate representation and
// answers the layout questions the backend asks. Every fact comes from the
// tree.
type typeTable struct {
	program *Program
	// quals maps a package qualifier, such as server, onto its import
	// path.
	quals map[string]string
	// decls maps a symbol id onto its declaration node.
	decls map[string]*zyxc.Node
	// modules maps a symbol id onto the import path that declares it.
	modules map[string]string
	// cache holds the named types already resolved, by symbol id.
	cache map[string]*types.Type
}

// newTypeTable indexes the declarations of every tree.
func newTypeTable(p *Program) *typeTable {
	t := &typeTable{
		program: p,
		quals:   map[string]string{},
		decls:   map[string]*zyxc.Node{},
		modules: map[string]string{},
		cache:   map[string]*types.Type{},
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
func qualifier(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

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

// moduleOf returns the import path that declares a symbol id.
func moduleOf(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 {
		return id[:i]
	}
	return ""
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
		if child.Kind != "field" || len(child.Payload) < 2 {
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
			list, err := t.parseTupleText(module, typesNode.Payload[0])
			if err != nil {
				return nil, false
			}
			v.Payload = list
		}
		out = append(out, v)
	}
	return out, true
}

// parseTupleText parses the payload text of a variant, which the format
// writes as a parenthesized list.
func (t *typeTable) parseTupleText(module, text string) ([]*types.Type, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "(") || !strings.HasSuffix(text, ")") {
		return nil, fmt.Errorf("llvmgen: %q is not a payload list", text)
	}
	inner := strings.TrimSpace(text[1 : len(text)-1])
	if inner == "" {
		return nil, nil
	}
	p := &typeParser{t: t, module: module, s: inner}
	var out []*types.Type
	for {
		ty, err := p.parseType()
		if err != nil {
			return nil, err
		}
		out = append(out, ty)
		p.skipSpace()
		if p.peek() != ',' {
			break
		}
		p.i++
	}
	return out, nil
}

// parse turns the canonical type text back into a type.
func (t *typeTable) parse(module, text string) (*types.Type, error) {
	p := &typeParser{t: t, module: module, s: text}
	ty, err := p.parseType()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("llvmgen: trailing text in type %q", text)
	}
	return ty, nil
}

// resolve returns the named type of a symbol id, caching the result.
func (t *typeTable) resolve(id, qual, name string, args []*types.Type) (*types.Type, error) {
	base, ok := t.cache[id]
	if !ok {
		node := t.decls[id]
		if node == nil {
			return nil, fmt.Errorf("llvmgen: no declaration for type %s", id)
		}
		kind, err := t.kindOf(node)
		if err != nil {
			return nil, err
		}
		base = types.Named(kind, moduleOf(id), qual, name, arityOf(node), args...)
		t.cache[id] = base
	}
	if len(args) == 0 {
		return base, nil
	}
	return base.Instantiate(args...), nil
}

// kindOf returns the type kind a declaration node introduces.
func (t *typeTable) kindOf(node *zyxc.Node) (types.Kind, error) {
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
			module := t.modules[firstSymID(node)]
			inner, err := t.parse(module, child.Payload[0])
			if err != nil {
				return types.Invalid, err
			}
			return inner.Kind, nil
		}
		return types.Invalid, fmt.Errorf("llvmgen: alias %s has no target", node.Payload[0])
	}
	return types.Invalid, fmt.Errorf("llvmgen: %s is not a type declaration", node.Kind)
}

// firstSymID returns the symbol id a type declaration carries.
func firstSymID(node *zyxc.Node) string {
	if len(node.Payload) >= 2 {
		return node.Payload[1]
	}
	return ""
}

// arityOf returns the declared type-parameter count of a declaration.
func arityOf(node *zyxc.Node) int {
	n := 0
	for _, child := range node.Children {
		if child.Kind == "param" {
			n++
			continue
		}
		break
	}
	return n
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

// A typeParser reads the canonical type text.
type typeParser struct {
	t      *typeTable
	module string
	s      string
	i      int
}

// skipSpace advances past spaces.
func (p *typeParser) skipSpace() {
	for p.i < len(p.s) && p.s[p.i] == ' ' {
		p.i++
	}
}

// peek returns the next byte, or zero at the end.
func (p *typeParser) peek() byte {
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

// hasPrefix reports whether the rest of the text starts with a word.
func (p *typeParser) hasPrefix(word string) bool {
	return strings.HasPrefix(p.s[p.i:], word)
}

// parseType reads one type.
func (p *typeParser) parseType() (*types.Type, error) {
	p.skipSpace()
	switch {
	case p.peek() == '(':
		return p.parseTuple()
	case p.peek() == '[':
		return p.parseBracket()
	case p.hasPrefix("func("):
		return p.parseFunc()
	case p.hasPrefix("chan<"):
		return p.parseChan()
	}
	return p.parseNamed()
}

// parseTuple reads a parenthesized type list.
func (p *typeParser) parseTuple() (*types.Type, error) {
	p.i++
	elems, err := p.parseList(')')
	if err != nil {
		return nil, err
	}
	if len(elems) < 2 {
		return nil, fmt.Errorf("llvmgen: a tuple needs two elements")
	}
	return types.TupleOf(elems...), nil
}

// parseBracket reads a slice or a fixed-size array.
func (p *typeParser) parseBracket() (*types.Type, error) {
	p.i++
	elem, err := p.parseType()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.peek() == ';' {
		p.i++
		p.skipSpace()
		start := p.i
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
		}
		n, err := strconv.ParseUint(p.s[start:p.i], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("llvmgen: array length is not a number")
		}
		p.skipSpace()
		if p.peek() != ']' {
			return nil, fmt.Errorf("llvmgen: an array type needs a closing bracket")
		}
		p.i++
		return types.ArrayOf(elem, n), nil
	}
	if p.peek() != ']' {
		return nil, fmt.Errorf("llvmgen: a slice type needs a closing bracket")
	}
	p.i++
	return types.SliceOf(elem), nil
}

// parseFunc reads a function type.
func (p *typeParser) parseFunc() (*types.Type, error) {
	p.i += len("func(")
	params, err := p.parseList(')')
	if err != nil {
		return nil, err
	}
	if p.hasPrefix(" -> ") {
		p.i += len(" -> ")
		result, err := p.parseType()
		if err != nil {
			return nil, err
		}
		return types.FuncOf(result, params...), nil
	}
	return types.FuncOf(nil, params...), nil
}

// parseChan reads a channel type.
func (p *typeParser) parseChan() (*types.Type, error) {
	p.i += len("chan<")
	elem, err := p.parseType()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.peek() != '>' {
		return nil, fmt.Errorf("llvmgen: a channel type needs a closing bracket")
	}
	p.i++
	return types.ChanOf(elem), nil
}

// parseList reads a comma-separated type list up to a closing byte.
func (p *typeParser) parseList(close byte) ([]*types.Type, error) {
	var out []*types.Type
	p.skipSpace()
	if p.peek() == close {
		p.i++
		return out, nil
	}
	for {
		ty, err := p.parseType()
		if err != nil {
			return nil, err
		}
		out = append(out, ty)
		p.skipSpace()
		if p.peek() == ',' {
			p.i++
			continue
		}
		if p.peek() != close {
			return nil, fmt.Errorf("llvmgen: a type list needs %q", string(close))
		}
		p.i++
		return out, nil
	}
}

// parseNamed reads a named or basic type.
func (p *typeParser) parseNamed() (*types.Type, error) {
	p.skipSpace()
	first := p.parseIdent()
	if first == "" {
		return nil, fmt.Errorf("llvmgen: expected a type name in %q", p.s)
	}
	qual, name := "", first
	if p.peek() == '.' {
		p.i++
		name = p.parseIdent()
		if name == "" {
			return nil, fmt.Errorf("llvmgen: expected a name after %q", first)
		}
		qual = first
	}
	var args []*types.Type
	if p.peek() == '<' {
		p.i++
		list, err := p.parseList('>')
		if err != nil {
			return nil, err
		}
		args = list
	}
	return p.t.named(p.module, qual, name, args)
}

// parseIdent reads one identifier, which may hold hyphens.
func (p *typeParser) parseIdent() string {
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' {
			p.i++
			continue
		}
		if c == '-' && p.i+1 < len(p.s) && isIdentTail(p.s[p.i+1]) {
			p.i++
			continue
		}
		break
	}
	return p.s[start:p.i]
}

// isIdentTail reports whether a byte may follow a hyphen in an identifier.
func isIdentTail(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// named returns the type a written name stands for.
func (t *typeTable) named(module, qual, name string, args []*types.Type) (*types.Type, error) {
	if qual == "c" {
		// A C type crosses the boundary as an opaque address.
		return types.Raw, nil
	}
	if qual == "" {
		if pre := types.Predeclared(name); pre != nil {
			if len(args) == 0 {
				return pre, nil
			}
			return pre.Instantiate(args...), nil
		}
		return t.resolve(module+"."+name, "", name, args)
	}
	path, ok := t.quals[qual]
	if !ok {
		return nil, fmt.Errorf("llvmgen: unknown package qualifier %q", qual)
	}
	return t.resolve(path+"."+name, qual, name, args)
}
