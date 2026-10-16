// Package types models the language's types. It is the shared vocabulary
// of the front end and the backend: type identity, the canonical text the
// .zyxc layer writes, the arithmetic and ordering rules, and the copy
// versus share classification of assignment.
//
// Declarations are not part of this package. Whether a user type satisfies
// an interface, is SyncSafe, or is usable across the C boundary depends on
// the declaration and belongs to the checker.
package types

import (
	"strconv"
	"strings"
)

// Kind classifies a type.
type Kind uint8

// Type kinds. The zero value is Invalid.
const (
	// Invalid marks a missing or ill-formed type.
	Invalid Kind = iota

	// The basic types.
	KindBool
	KindInt8
	KindInt16
	KindInt32
	KindInt64
	KindInt128
	KindUint8
	KindUint16
	KindUint32
	KindUint64
	KindUint128
	KindUsize
	KindFloat32
	KindFloat64
	KindChar
	KindStr
	KindRaw
	KindTypeValue

	// The composite types.
	KindTuple
	KindArray
	KindSlice
	KindFunc
	KindChan

	// The named types.
	KindStruct
	KindExternStruct
	KindUnion
	KindEnum
	KindInterface
	KindContainer
	KindDuration

	// KindTypeParam is a generic type parameter.
	KindTypeParam
)

// kindNames gives every kind a stable debug and test name.
var kindNames = [KindTypeParam + 1]string{
	Invalid:          "invalid",
	KindBool:         "bool",
	KindInt8:         "i8",
	KindInt16:        "i16",
	KindInt32:        "i32",
	KindInt64:        "i64",
	KindInt128:       "i128",
	KindUint8:        "u8",
	KindUint16:       "u16",
	KindUint32:       "u32",
	KindUint64:       "u64",
	KindUint128:      "u128",
	KindUsize:        "usize",
	KindFloat32:      "f32",
	KindFloat64:      "f64",
	KindChar:         "char",
	KindStr:          "str",
	KindRaw:          "raw",
	KindTypeValue:    "type",
	KindTuple:        "tuple",
	KindArray:        "array",
	KindSlice:        "slice",
	KindFunc:         "func",
	KindChan:         "chan",
	KindStruct:       "struct",
	KindExternStruct: "extern struct",
	KindUnion:        "extern union",
	KindEnum:         "enum",
	KindInterface:    "interface",
	KindContainer:    "container",
	KindDuration:     "Duration",
	KindTypeParam:    "type parameter",
}

// String returns the spelling used in diagnostics.
func (k Kind) String() string {
	if k <= KindTypeParam {
		if s := kindNames[k]; s != "" {
			return s
		}
	}
	return "invalid"
}

// A Type is one language type.
type Type struct {
	// Kind classifies the type.
	Kind Kind

	// Name is the source name of a named type or a type parameter.
	Name string
	// Pkg is the import path of the declaring package of a named type.
	// Together with Name it identifies the declaration.
	Pkg string
	// Qual is the package name written before Name when the type appears
	// in text. It is empty for the package being compiled and for the
	// pre-imported types.
	Qual string
	// ID is the identity key of a named type or a type parameter. Named
	// types use their package path and name; type parameters use the
	// symbol id of their declaration, so two same-named parameters of
	// different declarations stay distinct.
	ID string
	// Arity is the declared type-parameter count of a named type.
	Arity int
	// Args holds the type arguments of a named generic type.
	Args []*Type

	// Elem is the element type of a slice, a fixed-size array, or a
	// channel.
	Elem *Type
	// Len is the element count of a fixed-size array.
	Len uint64
	// Elems holds the element types of a tuple or the parameter types of a
	// function type.
	Elems []*Type
	// Result is the result type of a function type; nil for a result-free
	// function.
	Result *Type

	// Constraints holds the interfaces a type parameter requires. A
	// parameter without constraints has none, and a body that uses the
	// parameter as a value can call only the methods those interfaces
	// declare.
	Constraints []*Type
}

// SliceOf returns the read-only view type [elem].
func SliceOf(elem *Type) *Type { return &Type{Kind: KindSlice, Elem: elem} }

// ArrayOf returns the fixed-size array type [elem; n].
func ArrayOf(elem *Type, n uint64) *Type { return &Type{Kind: KindArray, Elem: elem, Len: n} }

// TupleOf returns the tuple type of the given elements. Tuples always have
// two or more elements.
func TupleOf(elems ...*Type) *Type { return &Type{Kind: KindTuple, Elems: elems} }

// FuncOf returns the function type func(params) -> result. A nil result is
// a result-free function.
func FuncOf(result *Type, params ...*Type) *Type {
	return &Type{Kind: KindFunc, Elems: params, Result: result}
}

// ChanOf returns the channel type chan<elem>.
func ChanOf(elem *Type) *Type { return &Type{Kind: KindChan, Elem: elem} }

// Named returns a named type. pkg is the import path of the declaring
// package, name the declaration name, qual the package name to write before
// the type in text, arity the declared type-parameter count, and args the
// type arguments.
func Named(kind Kind, pkg, qual, name string, arity int, args ...*Type) *Type {
	t := &Type{Kind: kind, Name: name, Pkg: pkg, Qual: qual, Arity: arity, Args: args}
	if pkg == "" {
		t.ID = name
	} else {
		t.ID = pkg + "." + name
	}
	return t
}

// Param returns a generic type parameter. id is the symbol id of the
// parameter's declaration.
func Param(id, name string) *Type { return ParamOf(id, name, nil) }

// ParamOf returns a generic type parameter with the interfaces it requires.
// A body that uses the parameter as a value may call the methods those
// interfaces declare.
func ParamOf(id, name string, constraints []*Type) *Type {
	return &Type{Kind: KindTypeParam, ID: id, Name: name, Constraints: constraints}
}

// String returns the canonical text of the type, in the form the .zyxc
// layer writes: `[T; 4]`, `(i64, str)`, `func(i64) -> bool`, `Array<u8>`,
// `net.Conn`.
func (t *Type) String() string {
	if t == nil {
		return "<nil>"
	}
	if t.ID == "void" {
		// The result of a function that returns nothing is a type a handle
		// or a container can carry as a type argument, so it has a name.
		return "void"
	}
	switch t.Kind {
	case KindTuple:
		return "(" + joinTypes(t.Elems) + ")"
	case KindArray:
		return "[" + t.Elem.String() + "; " + strconv.FormatUint(t.Len, 10) + "]"
	case KindSlice:
		return "[" + t.Elem.String() + "]"
	case KindFunc:
		s := "func(" + joinTypes(t.Elems) + ")"
		if t.Result != nil {
			s += " -> " + t.Result.String()
		}
		return s
	case KindChan:
		return "chan<" + t.Elem.String() + ">"
	case KindTypeParam:
		return t.Name
	}
	if t.IsNamed() {
		s := t.Name
		if t.Qual != "" {
			s = t.Qual + "." + s
		}
		if len(t.Args) > 0 {
			s += "<" + joinTypes(t.Args) + ">"
		}
		return s
	}
	if t.Kind <= KindTypeValue {
		return kindNames[t.Kind]
	}
	return "invalid"
}

// joinTypes renders a type list with the separator the .zyxc layer uses.
func joinTypes(list []*Type) string {
	parts := make([]string, len(list))
	for i, t := range list {
		parts[i] = t.String()
	}
	return strings.Join(parts, ", ")
}

// Identical reports whether a and b are the same type. Named types compare
// by declaration, so two structs with equal fields stay distinct.
func Identical(a, b *Type) bool {
	if a == nil || b == nil {
		// Two functions that return nothing have no result type to compare,
		// so they match. A function that returns something does not match
		// one that returns nothing.
		return a == b
	}
	if a == b {
		return true
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case KindTuple:
		return identicalList(a.Elems, b.Elems)
	case KindFunc:
		return identicalList(a.Elems, b.Elems) && Identical(a.Result, b.Result)
	case KindArray:
		return a.Len == b.Len && Identical(a.Elem, b.Elem)
	case KindSlice, KindChan:
		return Identical(a.Elem, b.Elem)
	case KindTypeParam:
		return a.ID == b.ID
	}
	if a.IsNamed() {
		return a.ID == b.ID && identicalList(a.Args, b.Args)
	}
	return true
}

// identicalList reports whether two type lists match elementwise.
func identicalList(a, b []*Type) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !Identical(a[i], b[i]) {
			return false
		}
	}
	return true
}

// IsNamed reports whether the type is declared by a name: a user type, a
// pre-imported type, or a container.
func (t *Type) IsNamed() bool {
	switch t.Kind {
	case KindStruct, KindExternStruct, KindUnion, KindEnum, KindInterface, KindContainer, KindDuration:
		return true
	}
	return false
}

// IsBasic reports whether the type is a predeclared scalar.
func (t *Type) IsBasic() bool { return t.Kind <= KindTypeValue }

// IsNumeric reports whether the type is an integer or floating-point type.
func (t *Type) IsNumeric() bool { return t.IsInteger() || t.IsFloat() }

// IsInteger reports whether the type is an integer type.
func (t *Type) IsInteger() bool {
	return t.Kind >= KindInt8 && t.Kind <= KindUsize
}

// IsFloat reports whether the type is a floating-point type.
func (t *Type) IsFloat() bool {
	return t.Kind == KindFloat32 || t.Kind == KindFloat64
}

// IsSigned reports whether the type is a signed integer.
func (t *Type) IsSigned() bool {
	return t.Kind >= KindInt8 && t.Kind <= KindInt128
}

// IsUnsigned reports whether the type is an unsigned integer.
func (t *Type) IsUnsigned() bool {
	return t.Kind >= KindUint8 && t.Kind <= KindUsize
}

// IsBool reports whether the type is bool.
func (t *Type) IsBool() bool { return t.Kind == KindBool }

// IsChar reports whether the type is char.
func (t *Type) IsChar() bool { return t.Kind == KindChar }

// IsStr reports whether the type is str.
func (t *Type) IsStr() bool { return t.Kind == KindStr }

// IsRaw reports whether the type is raw.
func (t *Type) IsRaw() bool { return t.Kind == KindRaw }

// IsTypeValue reports whether the type is the comptime `type` value.
func (t *Type) IsTypeValue() bool { return t.Kind == KindTypeValue }

// IsOrdered reports whether the relational operators apply: the numeric
// types and char.
func (t *Type) IsOrdered() bool { return t.IsNumeric() || t.Kind == KindChar }

// IsContainer reports whether the type is a pre-imported container or a
// runtime handle.
func (t *Type) IsContainer() bool { return t.Kind == KindContainer }

// IsIterable reports whether a for-in loop may range over the type. Only
// the built-in containers, fixed-size arrays, and str are iterable.
func (t *Type) IsIterable() bool {
	switch t.Kind {
	case KindStr, KindArray:
		return true
	case KindContainer:
		return t.ID == Array.ID || t.ID == Set.ID || t.ID == Map.ID
	}
	return false
}
