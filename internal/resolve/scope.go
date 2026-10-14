// Scopes, symbols, and the symbol identifiers the .zyxc layer carries.

package resolve

import (
	"sort"
	"strconv"

	"zyx/internal/ast"
)

// SymbolKind classifies a named entity introduced by a declaration or a
// predeclared name.
type SymbolKind uint8

const (
	// KindBuiltin is a predeclared name from the language universe.
	KindBuiltin SymbolKind = iota
	// KindPackage is an imported package qualifier.
	KindPackage
	// KindType is a struct, extern struct, extern union, enum, interface,
	// or type alias.
	KindType
	// KindFunc is a top-level function.
	KindFunc
	// KindMethod is a method declared in a type body.
	KindMethod
	// KindField is a struct or union field.
	KindField
	// KindVariant is an enum variant.
	KindVariant
	// KindConst is a constant.
	KindConst
	// KindVar is a top-level variable.
	KindVar
	// KindParam is a function or lambda parameter.
	KindParam
	// KindLocal is a local binding.
	KindLocal
	// KindTypeParam is a generic type parameter.
	KindTypeParam
	// KindLabel is a loop label.
	KindLabel
	// KindCSymbol is a name that comes from an imported C header.
	KindCSymbol
)

// String returns the kind name used in diagnostics.
func (k SymbolKind) String() string {
	switch k {
	case KindBuiltin:
		return "predeclared name"
	case KindPackage:
		return "imported package"
	case KindType:
		return "type"
	case KindFunc:
		return "function"
	case KindMethod:
		return "method"
	case KindField:
		return "field"
	case KindVariant:
		return "enum variant"
	case KindConst:
		return "constant"
	case KindVar:
		return "variable"
	case KindParam:
		return "parameter"
	case KindLocal:
		return "local variable"
	case KindTypeParam:
		return "type parameter"
	case KindLabel:
		return "label"
	case KindCSymbol:
		return "C symbol"
	}
	return "name"
}

// Symbol is a named entity: a declaration, a binding, or a reference
// target. IDs follow the front-end scheme of the toolchain document: a
// top-level declaration is <import path>.<name>, a method or field is
// <type symbol>.<name>, a local or parameter is <function symbol>#<n> with
// n counting from one in declaration order, and a type parameter is
// <declaration symbol>$<name>.
type Symbol struct {
	// ID is the stable symbol id the .zyxc layer carries.
	ID string
	// Name is the source spelling.
	Name string
	// Kind classifies the entity.
	Kind SymbolKind
	// Public reports whether the name is visible outside its package.
	Public bool
	// Decl is the declaring node; nil for predeclared names.
	Decl ast.Node
	// Owner is the enclosing type for members, the enclosing function for
	// locals and parameters, and nil at package level.
	Owner *Symbol
	// Members holds the members of a type symbol in source order.
	Members []*Symbol

	member map[string]*Symbol
}

// Member returns the member of a type symbol with the given name, or nil.
func (s *Symbol) Member(name string) *Symbol {
	if s == nil {
		return nil
	}
	return s.member[name]
}

// insertMember adds a member to a type symbol and returns the member it
// displaced, or nil when the name was free.
func (s *Symbol) insertMember(sym *Symbol) *Symbol {
	if s.member == nil {
		s.member = map[string]*Symbol{}
	}
	if old, ok := s.member[sym.Name]; ok {
		return old
	}
	s.member[sym.Name] = sym
	s.Members = append(s.Members, sym)
	return nil
}

// topID returns the symbol id of a top-level declaration.
func topID(path, name string) string { return path + "." + name }

// localID returns the symbol id of a local, a parameter, or a lambda.
func localID(owner *Symbol, n int) string {
	if owner == nil {
		return "#" + strconv.Itoa(n)
	}
	return owner.ID + "#" + strconv.Itoa(n)
}

// typeParamID returns the symbol id of a generic parameter.
func typeParamID(decl *Symbol, name string) string {
	if decl == nil {
		return "$" + name
	}
	return decl.ID + "$" + name
}

// scopeKind discriminates the lexical levels of a package.
type scopeKind uint8

const (
	scopeUniverse scopeKind = iota
	scopePackage
	scopeFile
	scopeType
	scopeFunc
	scopeBlock
)

// scope is one lexical level. Every level can hold labels; a label is only
// reachable while its loop encloses the statement that names it.
type scope struct {
	parent *scope
	kind   scopeKind
	names  map[string]*Symbol
	labels map[string]*Symbol
}

// newScope returns a scope nested in parent.
func newScope(parent *scope, kind scopeKind) *scope {
	return &scope{parent: parent, kind: kind, names: map[string]*Symbol{}}
}

// lookup returns the innermost symbol named name, searching outward.
func (s *scope) lookup(name string) *Symbol {
	for cur := s; cur != nil; cur = cur.parent {
		if sym, ok := cur.names[name]; ok {
			return sym
		}
	}
	return nil
}

// insert binds sym in this scope and returns the symbol it displaced, or
// nil when the name was free.
func (s *scope) insert(sym *Symbol) *Symbol {
	if old, ok := s.names[sym.Name]; ok {
		return old
	}
	s.names[sym.Name] = sym
	return nil
}

// insertLabel binds a loop label in this scope and returns the label it
// displaced, or nil when the name was free.
func (s *scope) insertLabel(sym *Symbol) *Symbol {
	if s.labels == nil {
		s.labels = map[string]*Symbol{}
	}
	if old, ok := s.labels[sym.Name]; ok {
		return old
	}
	s.labels[sym.Name] = sym
	return nil
}

// lookupLabel returns the label named name in this scope or an enclosing
// one. Only labels of loops that enclose the reference are reachable.
func (s *scope) lookupLabel(name string) *Symbol {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.labels == nil {
			continue
		}
		if sym, ok := cur.labels[name]; ok {
			return sym
		}
	}
	return nil
}

// scopeNames returns the names bound directly in s, in sorted order.
func scopeNames(s *scope) []string {
	names := make([]string, 0, len(s.names))
	for name := range s.names {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// sameNameSet reports whether a and b hold the same names.
func sameNameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, name := range a {
		set[name] = true
	}
	for _, name := range b {
		if !set[name] {
			return false
		}
	}
	return true
}
