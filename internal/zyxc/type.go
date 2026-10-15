// Type text parsing and rendering. The format writes a type as the
// canonical text the language uses, so both the monomorphizer and the
// backend read it back through this file.

package zyxc

import (
	"fmt"
	"strconv"
	"strings"

	"zyx/internal/types"
)

// A Resolver answers the two questions a type text raises: which import
// path a package qualifier names, and what kind of type a declaration
// introduces.
type Resolver interface {
	// ImportPath returns the import path a package qualifier names.
	ImportPath(qual string) (string, bool)
	// KindOf returns the kind of type a declaration introduces.
	KindOf(id string) (types.Kind, error)
}

// ParseType parses the canonical text of a type. A bare name resolves in
// module, and a qualified name through the resolver.
func ParseType(module, text string, r Resolver) (*types.Type, error) {
	return ParseTypeWith(module, text, r, nil)
}

// ParseTypeWith parses the canonical text of a type in the scope of a
// declaration's type parameters. params maps a parameter name onto the type
// that names it, so the text of a generic declaration reads back with its
// parameters intact.
func ParseTypeWith(module, text string, r Resolver, params map[string]*types.Type) (*types.Type, error) {
	p := &typeParser{module: module, r: r, s: text, params: params}
	t, err := p.parseType()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("zyxc: trailing text in type %q", text)
	}
	return t, nil
}

// TypeText renders a type the way the format writes it, giving every named
// type that module does not declare its package qualifier. The text then
// names its package wherever it appears, and a reader resolves it without
// guessing.
func TypeText(t *types.Type, module string) string {
	return qualify(t, module).String()
}

// qualify returns a copy of a type in which every named type outside module
// carries its package qualifier.
func qualify(t *types.Type, module string) *types.Type {
	if t == nil {
		return nil
	}
	switch t.Kind {
	case types.KindTuple, types.KindFunc:
		out := *t
		out.Elems = make([]*types.Type, len(t.Elems))
		for i, e := range t.Elems {
			out.Elems[i] = qualify(e, module)
		}
		out.Result = qualify(t.Result, module)
		return &out
	case types.KindArray, types.KindSlice, types.KindChan:
		out := *t
		out.Elem = qualify(t.Elem, module)
		return &out
	}
	if !t.IsNamed() || t.Pkg == "" || t.Pkg == module || t.Qual != "" {
		return t
	}
	out := *t
	out.Qual = PackageQualifier(t.Pkg)
	if len(t.Args) > 0 {
		out.Args = make([]*types.Type, len(t.Args))
		for i, a := range t.Args {
			out.Args[i] = qualify(a, module)
		}
	}
	return &out
}

// PackageQualifier returns the name a package is written by, which is its
// last path segment.
func PackageQualifier(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// A typeParser reads the canonical type text.
type typeParser struct {
	r      Resolver
	module string
	s      string
	i      int
	// params maps the type-parameter names in scope onto their types.
	params map[string]*types.Type
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
	if len(elems) == 0 {
		return nil, fmt.Errorf("zyxc: a tuple needs an element")
	}
	// A one-element list is a tuple like any other. An enum variant whose
	// payload holds one type writes it this way, so the text has to read
	// back as that tuple rather than as the element alone.
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
			return nil, fmt.Errorf("zyxc: an array length is not a number")
		}
		p.skipSpace()
		if p.peek() != ']' {
			return nil, fmt.Errorf("zyxc: an array type needs a closing bracket")
		}
		p.i++
		return types.ArrayOf(elem, n), nil
	}
	if p.peek() != ']' {
		return nil, fmt.Errorf("zyxc: a slice type needs a closing bracket")
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
		return nil, fmt.Errorf("zyxc: a channel type needs a closing bracket")
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
			return nil, fmt.Errorf("zyxc: a type list needs %q", string(close))
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
		return nil, fmt.Errorf("zyxc: expected a type name in %q", p.s)
	}
	qual, name := "", first
	if p.peek() == '.' {
		p.i++
		name = p.parseIdent()
		if name == "" {
			return nil, fmt.Errorf("zyxc: expected a name after %q", first)
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
	return p.named(qual, name, args)
}

// parseIdent reads one identifier, which may hold hyphens.
func (p *typeParser) parseIdent() string {
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '$' {
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
func (p *typeParser) named(qual, name string, args []*types.Type) (*types.Type, error) {
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
		if param, ok := p.params[name]; ok && len(args) == 0 {
			return param, nil
		}
		return p.resolve(p.module+"."+name, "", name, args)
	}
	path, ok := p.r.ImportPath(qual)
	if !ok {
		return nil, fmt.Errorf("zyxc: unknown package qualifier %q", qual)
	}
	return p.resolve(path+"."+name, qual, name, args)
}

// resolve builds the named type of a symbol id.
func (p *typeParser) resolve(id, qual, name string, args []*types.Type) (*types.Type, error) {
	kind, err := p.r.KindOf(id)
	if err != nil {
		return nil, err
	}
	t := types.Named(kind, moduleOfID(id), qual, name, 0, args...)
	return t, nil
}

// moduleOfID returns the import path that declares a symbol id, which is
// everything before its last dot.
func moduleOfID(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 {
		return id[:i]
	}
	return ""
}
