// LLVM IR text emission helpers.

package llvmgen

import (
	"fmt"
	"strings"

	"zyx/internal/layout"
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// A module accumulates the text of one LLVM module. Definitions are held in
// the order the format requires: the target, the struct types, the string
// globals, and then the functions.
type module struct {
	target string
	// structs holds the named struct definitions in creation order.
	structs []string
	// globals holds the string globals in creation order.
	globals []string
	// funcs holds the function declarations and definitions in emission
	// order.
	funcs strings.Builder
	// names maps a language type id onto its LLVM struct name.
	names map[string]string
	// strings maps a string literal onto its global name.
	strs map[string]string
	// declared records the external symbols already declared.
	declared map[string]bool
	// defined records the symbols already defined.
	defined map[string]bool
	// globalInits holds the top-level initializers that are not constants.
	globalInits []globalInit
	next        int
}

// newModule returns an empty module.
func newModule(target string) *module {
	return &module{
		target:   target,
		names:    map[string]string{},
		strs:     map[string]string{},
		declared: map[string]bool{},
		defined:  map[string]bool{},
	}
}

// text assembles the module.
func (m *module) text() []byte {
	var b strings.Builder
	b.WriteString("; zyx\n")
	b.WriteString("target triple = \"" + m.target + "\"\n")
	for _, def := range m.structs {
		b.WriteString(def)
	}
	for _, g := range m.globals {
		b.WriteString(g)
	}
	b.WriteString(m.funcs.String())
	return []byte(b.String())
}

// llvmName renders a language symbol id as an LLVM global name. A name that
// is not a plain identifier is quoted, which keeps every character of the
// id, including the slash of an import path.
func llvmName(id string) string {
	if id == "" {
		return "anonymous"
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '$' {
			continue
		}
		return `"` + strings.NewReplacer(`\`, `\5C`, `"`, `\22`).Replace(id) + `"`
	}
	return id
}

// escapeBytes renders a byte string as the body of an LLVM c"..." literal.
func escapeBytes(data []byte) string {
	var b strings.Builder
	for _, c := range data {
		if c >= 0x20 && c < 0x7f && c != '"' && c != '\\' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "\\%02X", c)
	}
	return b.String()
}

// stringGlobal interns a string literal and returns its global name.
func (m *module) stringGlobal(s string) string {
	if name, ok := m.strs[s]; ok {
		return name
	}
	name := fmt.Sprintf("@.str.%d", len(m.globals))
	m.strs[s] = name
	data := []byte(s)
	m.globals = append(m.globals,
		fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\"\n", name, len(data), escapeBytes(data)))
	return name
}

// structName returns the LLVM name of a named aggregate type, creating the
// definition on first use.
func (g *generator) structName(t *types.Type) string {
	if name, ok := g.m.names[t.ID]; ok {
		return name
	}
	name := fmt.Sprintf("%%s.%d", g.m.next)
	g.m.next++
	g.m.names[t.ID] = name
	// The body may name further aggregates, so the slot is reserved before
	// it is built.
	slot := len(g.m.structs)
	g.m.structs = append(g.m.structs, "")
	body := g.aggregateBody(t)
	g.m.structs[slot] = name + " = type " + body + "\n"
	return name
}

// aggregateBody renders the LLVM body of a named aggregate type.
func (g *generator) aggregateBody(t *types.Type) string {
	switch t.Kind {
	case types.KindExternStruct:
		fields, _ := g.tab.Fields(t)
		return "{" + g.fieldList(fields) + "}"
	case types.KindStruct:
		placed, err := layout.Fields(t, g.tab, g.target.Layout())
		if err != nil {
			g.fail(fmt.Errorf("layout of %s: %w", t, err))
			return "{ i8 }"
		}
		if len(placed) == 0 {
			// A reference struct occupies one byte, so every object has a
			// distinct address.
			return "{ i8 }"
		}
		parts := make([]string, 0, len(placed))
		for _, f := range placed {
			parts = append(parts, g.llType(f.Type))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case types.KindUnion:
		return g.unionBody(t)
	}
	g.fail(fmt.Errorf("llvmgen: %s has no aggregate layout", t))
	return "{ i8 }"
}

// fieldList renders the declared fields of a C aggregate.
func (g *generator) fieldList(fields []layout.Field) string {
	if len(fields) == 0 {
		return "i8"
	}
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, g.llType(f.Type))
	}
	return strings.Join(parts, ", ")
}

// unionBody renders a union as its widest-aligned member followed by the
// padding that brings the object to the union's size.
func (g *generator) unionBody(t *types.Type) string {
	fields, ok := g.tab.Fields(t)
	if !ok {
		g.fail(fmt.Errorf("llvmgen: %s has no fields", t))
		return "{ i8 }"
	}
	size, err := layout.Size(t, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("layout of %s: %w", t, err))
		return "{ i8 }"
	}
	best := 0
	bestAlign := uint64(0)
	for i, f := range fields {
		align, err := layout.Align(f.Type, g.tab, g.target.Layout())
		if err != nil {
			g.fail(fmt.Errorf("layout of %s: %w", t, err))
			return "{ i8 }"
		}
		if align > bestAlign {
			best, bestAlign = i, align
		}
	}
	member, err := layout.Size(fields[best].Type, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("layout of %s: %w", t, err))
		return "{ i8 }"
	}
	pad := size - member
	if pad == 0 {
		return "{" + g.llType(fields[best].Type) + "}"
	}
	return fmt.Sprintf("{%s, [%d x i8]}", g.llType(fields[best].Type), pad)
}

// llType returns the LLVM type of a value as it is stored: the data block of
// an aggregate, the fat pointer of a string or a slice, the integer of a
// scalar.
func (g *generator) llType(t *types.Type) string {
	if t == nil {
		g.fail(fmt.Errorf("llvmgen: a value has no type"))
		return "i64"
	}
	switch t.Kind {
	case types.KindBool, types.KindInt8, types.KindUint8:
		return "i8"
	case types.KindInt16, types.KindUint16:
		return "i16"
	case types.KindInt32, types.KindUint32:
		return "i32"
	case types.KindInt64, types.KindUint64, types.KindUsize, types.KindDuration:
		return g.ptrInt()
	case types.KindInt128, types.KindUint128:
		return "i128"
	case types.KindFloat32:
		return "float"
	case types.KindFloat64:
		return "double"
	case types.KindChar:
		return "i32"
	case types.KindRaw, types.KindFunc:
		return "ptr"
	case types.KindStr, types.KindSlice:
		return "{ ptr, " + g.ptrInt() + " }"
	case types.KindInterface:
		return "{ ptr, ptr }"
	case types.KindChan, types.KindContainer:
		return "ptr"
	case types.KindArray:
		return fmt.Sprintf("[%d x %s]", t.Len, g.llType(t.Elem))
	case types.KindTuple:
		parts := make([]string, 0, len(t.Elems))
		for _, e := range t.Elems {
			parts = append(parts, g.llType(e))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case types.KindStruct, types.KindExternStruct, types.KindUnion:
		return g.structName(t)
	case types.KindEnum:
		g.fail(fmt.Errorf("llvmgen: enums are not lowered yet"))
		return "i8"
	}
	g.fail(fmt.Errorf("llvmgen: %s has no LLVM type", t))
	return "i64"
}

// llPass returns the LLVM type of a value in a register, a parameter, or a
// local: an aggregate passes as the reference that names its object.
func (g *generator) llPass(t *types.Type) string {
	if t == nil {
		g.fail(fmt.Errorf("llvmgen: a value has no type"))
		return "i64"
	}
	if byAddress(t) {
		return "ptr"
	}
	return g.llType(t)
}

// ptrInt returns the integer type that holds a pointer-sized value.
func (g *generator) ptrInt() string {
	if g.target.Layout().PtrSize == 4 {
		return "i32"
	}
	return "i64"
}

// byAddress reports whether a value of the type is passed and stored as the
// address of its object rather than as the object itself. A reference
// struct and a fixed-size array are created in a region, so a value of
// either names an object rather than carrying one.
func byAddress(t *types.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case types.KindStruct, types.KindExternStruct, types.KindUnion, types.KindEnum, types.KindArray:
		return true
	}
	return false
}

// zero renders the LLVM zero value of a type.
func (g *generator) zero(t *types.Type) string {
	ll := g.llPass(t)
	if byAddress(t) {
		return "null"
	}
	if strings.HasPrefix(ll, "{") || strings.HasPrefix(ll, "[") {
		return "zeroinitializer"
	}
	if ll == "float" || ll == "double" {
		return "0.0"
	}
	if ll == "ptr" {
		return "null"
	}
	return "0"
}

// typeOfNode returns the declared type of a `type` child, or nil.
func (g *generator) typeOfNode(n *zyxc.Node, module string) *types.Type {
	if n == nil || len(n.Payload) == 0 {
		return nil
	}
	t, err := g.tab.parse(module, n.Payload[0])
	if err != nil {
		g.fail(fmt.Errorf("%s: %w", n.Pos, err))
		return nil
	}
	return t
}
