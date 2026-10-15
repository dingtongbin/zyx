// Assignment semantics: copy versus shared reference.

package types

// IsInline reports whether a value of the type is stored inside its
// container rather than behind a handle. Inline types are the scalars, the
// C value types, tuples, and fixed-size arrays.
func (t *Type) IsInline() bool {
	switch t.Kind {
	case KindBool, KindInt8, KindInt16, KindInt32, KindInt64, KindInt128,
		KindUint8, KindUint16, KindUint32, KindUint64, KindUint128, KindUsize,
		KindFloat32, KindFloat64, KindChar, KindRaw, KindTypeValue,
		KindFunc, KindDuration, KindExternStruct, KindUnion,
		KindTuple, KindArray:
		return true
	}
	return false
}

// IsValue reports whether assignment copies the whole value: the scalars,
// the C value types, functions, and tuples or arrays whose every element is
// itself a value type.
func (t *Type) IsValue() bool {
	switch t.Kind {
	case KindTuple:
		for _, e := range t.Elems {
			if !e.IsValue() {
				return false
			}
		}
		return true
	case KindArray:
		return t.Elem.IsValue()
	}
	return t.IsInline()
}

// IsReference reports whether assignment shares one object: str and the
// slices, user types, interface values, channels, and containers.
func (t *Type) IsReference() bool {
	switch t.Kind {
	case KindStr, KindSlice, KindStruct, KindEnum, KindInterface, KindChan, KindContainer:
		return true
	}
	return false
}

// Assignable reports whether a value of type src may be assigned to a
// destination of type dst with no conversion and no interface check. The
// language has no implicit conversions, so the types must be identical.
func Assignable(dst, src *Type) bool { return Identical(dst, src) }

// NeedsImplementation reports whether an assignment or an argument pass
// from src to dst still depends on an interface implementation. The checker
// confirms that src implements dst; this package cannot, because
// implementations live in declarations.
func NeedsImplementation(dst, src *Type) bool {
	return dst != nil && dst.Kind == KindInterface && !Identical(dst, src)
}

// Convertible reports whether the explicit conversion dst(src) is allowed.
// The language permits only numeric conversions, char with u32, and raw
// with usize; bool never converts.
func Convertible(dst, src *Type) bool {
	if dst == nil || src == nil {
		return false
	}
	if dst.IsNumeric() && src.IsNumeric() {
		return true
	}
	switch {
	case dst.Kind == KindChar && src.Kind == KindUint32:
		return true
	case dst.Kind == KindUint32 && src.Kind == KindChar:
		return true
	case dst.Kind == KindRaw && src.Kind == KindUsize:
		return true
	case dst.Kind == KindUsize && src.Kind == KindRaw:
		return true
	}
	return false
}
