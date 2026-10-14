// Size and alignment computation.

package layout

import (
	"fmt"

	"zyx/internal/types"
)

// scalarSize gives the size and alignment of the basic value types. A bool
// is one byte holding 0 or 1, and char holds one Unicode scalar value.
var scalarSize = map[types.Kind]uint64{
	types.KindBool:     1,
	types.KindInt8:     1,
	types.KindUint8:    1,
	types.KindInt16:    2,
	types.KindUint16:   2,
	types.KindInt32:    4,
	types.KindUint32:   4,
	types.KindInt64:    8,
	types.KindUint64:   8,
	types.KindInt128:   16,
	types.KindUint128:  16,
	types.KindFloat32:  4,
	types.KindFloat64:  8,
	types.KindChar:     4,
	types.KindDuration: 8,
}

// sizeOf returns the size of a type in bytes. seen holds the named types on
// the current path, so a value type that contains itself reports an error
// instead of recursing forever.
func sizeOf(t *types.Type, p Provider, target Target, seen map[string]bool) (uint64, error) {
	if t == nil {
		return 0, fmt.Errorf("layout: no type")
	}
	if t.Kind == types.KindTypeParam {
		return 0, ErrUnknown
	}
	if n, ok := scalarSize[t.Kind]; ok {
		return n, nil
	}
	switch t.Kind {
	case types.KindUsize, types.KindRaw, types.KindFunc:
		return target.PtrSize, nil
	case types.KindStr, types.KindSlice:
		// A fat pointer: {ptr, len}.
		return 2 * target.PtrSize, nil
	case types.KindInterface:
		// A fat pointer: {method table, data}.
		return 2 * target.PtrSize, nil
	case types.KindChan, types.KindContainer:
		// An opaque runtime handle.
		return target.PtrSize, nil
	case types.KindArray:
		elem, err := sizeOf(t.Elem, p, target, seen)
		if err != nil {
			return 0, err
		}
		return elem * t.Len, nil
	case types.KindTuple:
		size, _, err := structSize(fieldsOfTypes(t.Elems), p, target, seen, false, false)
		return size, err
	case types.KindStruct, types.KindExternStruct, types.KindUnion, types.KindEnum:
		return namedSize(t, p, target, seen)
	case types.KindTypeValue:
		return 0, fmt.Errorf("layout: a type value has no size")
	}
	return 0, fmt.Errorf("layout: %s has no size", t)
}

// alignOf returns the alignment of a type in bytes.
func alignOf(t *types.Type, p Provider, target Target, seen map[string]bool) (uint64, error) {
	if t == nil {
		return 0, fmt.Errorf("layout: no type")
	}
	if t.Kind == types.KindTypeParam {
		return 0, ErrUnknown
	}
	if n, ok := scalarSize[t.Kind]; ok {
		return n, nil
	}
	switch t.Kind {
	case types.KindUsize, types.KindRaw, types.KindFunc:
		return target.PtrAlign, nil
	case types.KindStr, types.KindSlice, types.KindInterface:
		return target.PtrAlign, nil
	case types.KindChan, types.KindContainer:
		return target.PtrAlign, nil
	case types.KindArray:
		return alignOf(t.Elem, p, target, seen)
	case types.KindTuple:
		_, align, err := structSize(fieldsOfTypes(t.Elems), p, target, seen, false, false)
		return align, err
	case types.KindStruct, types.KindExternStruct, types.KindUnion, types.KindEnum:
		_, align, err := namedLayout(t, p, target, seen)
		return align, err
	case types.KindTypeValue:
		return 0, fmt.Errorf("layout: a type value has no alignment")
	}
	return 0, fmt.Errorf("layout: %s has no alignment", t)
}

// fieldsOfTypes wraps a type list as the elements of a payload tuple.
func fieldsOfTypes(list []*types.Type) []Field {
	out := make([]Field, 0, len(list))
	for _, t := range list {
		out = append(out, Field{Type: t})
	}
	return out
}

// namedSize returns the size of a named aggregate type.
func namedSize(t *types.Type, p Provider, target Target, seen map[string]bool) (uint64, error) {
	size, _, err := namedLayout(t, p, target, seen)
	return size, err
}

// namedLayout returns the size and alignment of a named aggregate type.
func namedLayout(t *types.Type, p Provider, target Target, seen map[string]bool) (uint64, uint64, error) {
	if seen[t.ID] {
		return 0, 0, fmt.Errorf("layout: %s contains itself by value", t.ID)
	}
	seen[t.ID] = true
	defer delete(seen, t.ID)

	switch t.Kind {
	case types.KindEnum:
		return enumLayout(t, p, target, seen)
	case types.KindUnion:
		fields, ok := p.Fields(t)
		if !ok {
			return 0, 0, ErrUnknown
		}
		return unionLayout(fields, p, target, seen)
	case types.KindExternStruct:
		fields, ok := p.Fields(t)
		if !ok {
			return 0, 0, ErrUnknown
		}
		// C layout keeps the declared order.
		return structSize(fields, p, target, seen, false, true)
	case types.KindStruct:
		fields, ok := p.Fields(t)
		if !ok {
			return 0, 0, ErrUnknown
		}
		// A reference struct reorders its fields to minimize padding.
		return structSize(fields, p, target, seen, true, true)
	}
	return 0, 0, fmt.Errorf("layout: %s is not an aggregate", t)
}
