// struct, extern struct, union, and enum layouts.

package layout

import (
	"sort"

	"zyx/internal/types"
)

// laid is one field with its offset, size, and alignment.
type laid struct {
	Field
	Offset uint64
	Size   uint64
	Align  uint64
}

// structSize lays out a field list and returns the total size and the
// struct's alignment. reorder sorts the fields by descending alignment,
// which minimizes padding in a reference struct; named marks a struct that
// occupies one byte even when it has no fields, so every object has a
// distinct address. A variant payload is not named and may be empty.
func structSize(fields []Field, p Provider, target Target, seen map[string]bool, reorder, named bool) (uint64, uint64, error) {
	info, align, err := place(fields, p, target, seen, reorder)
	if err != nil {
		return 0, 0, err
	}
	if len(info) == 0 {
		if named {
			return 1, 1, nil
		}
		return 0, 1, nil
	}
	last := info[len(info)-1]
	return alignUp(last.Offset+last.Size, align), align, nil
}

// place computes the offset of every field and returns the fields in layout
// order together with the struct's alignment.
func place(fields []Field, p Provider, target Target, seen map[string]bool, reorder bool) ([]laid, uint64, error) {
	info := make([]laid, 0, len(fields))
	for _, f := range fields {
		size, err := sizeOf(f.Type, p, target, seen)
		if err != nil {
			return nil, 0, err
		}
		align, err := alignOf(f.Type, p, target, seen)
		if err != nil {
			return nil, 0, err
		}
		info = append(info, laid{Field: f, Size: size, Align: align})
	}
	if reorder {
		// Descending alignment; fields of equal alignment keep their
		// declaration order.
		sort.SliceStable(info, func(i, j int) bool { return info[i].Align > info[j].Align })
	}
	offset, align := uint64(0), uint64(1)
	for i := range info {
		offset = alignUp(offset, info[i].Align)
		info[i].Offset = offset
		offset += info[i].Size
		align = maxAlign(align, info[i].Align)
	}
	return info, align, nil
}

// unionLayout lays out a C union: every member starts at offset 0, and the
// size is the largest member rounded up to the union's alignment.
func unionLayout(fields []Field, p Provider, target Target, seen map[string]bool) (uint64, uint64, error) {
	info, align, err := place(fields, p, target, seen, false)
	if err != nil {
		return 0, 0, err
	}
	size := uint64(0)
	for _, f := range info {
		if f.Size > size {
			size = f.Size
		}
	}
	return alignUp(size, align), align, nil
}

// enumLayout lays out an enum: a tag that fits the variant count, followed
// by a union of the variant payloads.
func enumLayout(t *types.Type, p Provider, target Target, seen map[string]bool) (uint64, uint64, error) {
	shape, err := Enum(t, p, target)
	if err != nil {
		return 0, 0, err
	}
	align := maxAlign(shape.TagAlign, shape.PayloadAlign)
	size := alignUp(shape.TagSize, shape.PayloadAlign) + shape.PayloadSize
	return alignUp(size, align), align, nil
}

// An EnumShape describes the layout of an enum: a tag that fits the variant
// count, followed by a union of the variant payloads, aligned to the widest
// payload. The tag occupies the object from offset zero, and the payload
// union follows it at the next multiple of PayloadAlign.
type EnumShape struct {
	// TagSize and TagAlign describe the tag.
	TagSize, TagAlign uint64
	// PayloadSize and PayloadAlign describe the union of the payloads. The
	// union is empty, with an alignment of one, when no variant carries a
	// payload.
	PayloadSize, PayloadAlign uint64
}

// Enum returns the layout of an enum. The enum itself stands on the cycle
// path, so a variant whose payload contains the enum by value is reported
// rather than laid out forever.
func Enum(t *types.Type, p Provider, target Target) (EnumShape, error) {
	variants, ok := p.Variants(t)
	if !ok {
		return EnumShape{}, ErrUnknown
	}
	seen := map[string]bool{}
	if t != nil {
		seen[typeKey(t)] = true
	}
	shape := EnumShape{PayloadAlign: 1}
	shape.TagSize, shape.TagAlign = tagLayout(len(variants))
	for _, v := range variants {
		size, align, err := structSize(fieldsOfTypes(v.Payload), p, target, seen, false, false)
		if err != nil {
			return EnumShape{}, err
		}
		if size > shape.PayloadSize {
			shape.PayloadSize = size
		}
		shape.PayloadAlign = maxAlign(shape.PayloadAlign, align)
	}
	return shape, nil
}

// tagLayout returns the size and alignment of the tag of an enum with n
// variants: the smallest unsigned integer that can hold every variant.
func tagLayout(n int) (uint64, uint64) {
	switch {
	case n <= 1<<8:
		return 1, 1
	case n <= 1<<16:
		return 2, 2
	case n <= 1<<32:
		return 4, 4
	}
	return 8, 8
}
