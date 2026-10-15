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
	variants, ok := p.Variants(t)
	if !ok {
		return 0, 0, ErrUnknown
	}
	tagSize, tagAlign := tagLayout(len(variants))
	payloadSize, payloadAlign := uint64(0), uint64(1)
	for _, v := range variants {
		size, align, err := structSize(fieldsOfTypes(v.Payload), p, target, seen, false, false)
		if err != nil {
			return 0, 0, err
		}
		if size > payloadSize {
			payloadSize = size
		}
		payloadAlign = maxAlign(payloadAlign, align)
	}
	align := maxAlign(tagAlign, payloadAlign)
	size := alignUp(tagSize, payloadAlign) + payloadSize
	return alignUp(size, align), align, nil
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
