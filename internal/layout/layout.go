// Package layout computes the static layout of the language's types: sizes,
// alignments, and field offsets. This is the only layout algorithm the
// project defines, so a comptime query in the front end and the backend's
// final layout agree by construction.
package layout

import (
	"errors"
	"fmt"

	"zyx/internal/types"
)

// ErrUnknown reports a type whose layout is not settled, because it
// mentions a type parameter or a declaration the provider does not know.
// The caller keeps a mono-rule:layout for it and the backend settles it
// once the type is instantiated.
var ErrUnknown = errors.New("layout: size is not settled")

// A Target gives the layout properties of a target.
type Target struct {
	// PtrSize is the size of a pointer, of usize, and of raw, in bytes.
	PtrSize uint64
	// PtrAlign is the alignment of a pointer and of usize, in bytes.
	PtrAlign uint64
}

// A Field is one member of a struct or union.
type Field struct {
	// Name is the field name; empty for the elements of a payload tuple.
	Name string
	// Type is the field type.
	Type *types.Type
}

// A Variant is one member of an enum.
type Variant struct {
	// Name is the variant name.
	Name string
	// Payload holds the payload types, laid out as a tuple.
	Payload []*types.Type
}

// A Provider supplies the members of a named type, so the layout algorithm
// reaches struct fields and enum variants without depending on the checker.
// A provider that does not know a type reports false, and the layout stays
// unsettled.
type Provider interface {
	// Fields returns the fields of a struct, extern struct, or union in
	// declaration order.
	Fields(t *types.Type) ([]Field, bool)
	// Variants returns the variants of an enum in declaration order.
	Variants(t *types.Type) ([]Variant, bool)
}

// A Placed is one field with its offset in the final layout.
type Placed struct {
	Field
	// Offset is the byte offset from the start of the object.
	Offset uint64
}

// Size returns the size of a type in bytes.
func Size(t *types.Type, p Provider, target Target) (uint64, error) {
	return sizeOf(t, p, target, map[string]bool{})
}

// Align returns the alignment of a type in bytes.
func Align(t *types.Type, p Provider, target Target) (uint64, error) {
	return alignOf(t, p, target, map[string]bool{})
}

// Fields returns the fields of a struct or union in final layout order,
// with their offsets. The order is the reordered one for a struct, and the
// declared one for an extern struct or union.
func Fields(t *types.Type, p Provider, target Target) ([]Placed, error) {
	if t == nil {
		return nil, fmt.Errorf("layout: no type")
	}
	declared, ok := p.Fields(t)
	if !ok {
		return nil, ErrUnknown
	}
	info, _, err := place(declared, p, target, map[string]bool{}, t.Kind == types.KindStruct)
	if err != nil {
		return nil, err
	}
	if t.Kind == types.KindUnion {
		// Every member of a union starts at offset 0.
		for i := range info {
			info[i].Offset = 0
		}
	}
	out := make([]Placed, 0, len(info))
	for _, f := range info {
		out = append(out, Placed{Field: f.Field, Offset: f.Offset})
	}
	return out, nil
}

// Offset returns the byte offset of one field in the final layout.
func Offset(t *types.Type, name string, p Provider, target Target) (uint64, error) {
	placed, err := Fields(t, p, target)
	if err != nil {
		return 0, err
	}
	for _, f := range placed {
		if f.Name == name {
			return f.Offset, nil
		}
	}
	return 0, fmt.Errorf("layout: %s has no field %q", t, name)
}

// alignUp rounds n up to the next multiple of a, which must be a power of
// two.
func alignUp(n, a uint64) uint64 {
	if a <= 1 {
		return n
	}
	return (n + a - 1) / a * a
}

// maxAlign returns the larger of two alignments.
func maxAlign(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}
