// The layout provider the emitter asks for the settled layout of a type.
// The layout algorithm of the layout package is the only one the project
// defines, so a comptime query and the emitted layout agree by construction.

package check

import (
	"zyx/internal/layout"
	"zyx/internal/types"
)

// Fields returns the fields of a struct, extern struct, or union in
// declaration order, which is what the layout algorithm reorders.
func (p *Package) Fields(t *types.Type) ([]layout.Field, bool) {
	d := p.Decl(t)
	if d == nil {
		return nil, false
	}
	if len(d.Fields) == 0 {
		return nil, true
	}
	out := make([]layout.Field, 0, len(d.Fields))
	for _, f := range d.Fields {
		out = append(out, layout.Field{Name: f.Name, Type: f.Type})
	}
	return out, true
}

// Variants returns the variants of an enum in declaration order.
func (p *Package) Variants(t *types.Type) ([]layout.Variant, bool) {
	d := p.Decl(t)
	if d == nil {
		return nil, false
	}
	if len(d.Variants) == 0 {
		return nil, true
	}
	out := make([]layout.Variant, 0, len(d.Variants))
	for _, v := range d.Variants {
		out = append(out, layout.Variant{Name: v.Name, Payload: v.Payload})
	}
	return out, true
}
