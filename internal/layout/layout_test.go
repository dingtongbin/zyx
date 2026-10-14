package layout

import (
	"errors"
	"testing"

	"zyx/internal/types"
)

// testProvider supplies the members of the named types a test declares.
type testProvider struct {
	fields   map[string][]Field
	variants map[string][]Variant
}

func (p testProvider) Fields(t *types.Type) ([]Field, bool) {
	f, ok := p.fields[t.ID]
	return f, ok
}

func (p testProvider) Variants(t *types.Type) ([]Variant, bool) {
	v, ok := p.variants[t.ID]
	return v, ok
}

// named returns a named type of the test package.
func named(kind types.Kind, name string) *types.Type {
	return types.Named(kind, "test", "", name, 0)
}

// The two targets every layout test runs against.
var (
	target64 = Target{PtrSize: 8, PtrAlign: 8}
	target32 = Target{PtrSize: 4, PtrAlign: 4}
)

func TestScalarSizes(t *testing.T) {
	cases := []struct {
		typ  *types.Type
		size uint64
	}{
		{types.Bool, 1},
		{types.Int8, 1},
		{types.Uint8, 1},
		{types.Int16, 2},
		{types.Int32, 4},
		{types.Int64, 8},
		{types.Int128, 16},
		{types.Uint128, 16},
		{types.Float32, 4},
		{types.Float64, 8},
		{types.Char, 4},
		{types.Duration, 8},
		{types.Usize, 8},
		{types.Raw, 8},
		{types.FuncOf(types.Bool, types.Int64), 8},
		{types.Str, 16},
		{types.SliceOf(types.Uint8), 16},
		{types.Eq, 16},
		{types.ChanOf(types.Int32), 8},
		{types.Array.Instantiate(types.Int32), 8},
		{types.ArrayOf(types.Int32, 4), 16},
		{types.ArrayOf(types.Uint8, 3), 3},
		{types.ArrayOf(types.Str, 2), 32},
		{types.TupleOf(types.Int64, types.Int8), 16},
	}
	for _, c := range cases {
		got, err := Size(c.typ, testProvider{}, target64)
		if err != nil {
			t.Errorf("%s: %v", c.typ, err)
			continue
		}
		if got != c.size {
			t.Errorf("%s: size = %d, want %d", c.typ, got, c.size)
		}
	}
}

func TestNarrowTargetSizes(t *testing.T) {
	cases := []struct {
		typ  *types.Type
		size uint64
	}{
		{types.Usize, 4},
		{types.Raw, 4},
		{types.Str, 8},
		{types.SliceOf(types.Uint8), 8},
		{types.Eq, 8},
		{types.Array.Instantiate(types.Int32), 4},
		{types.ArrayOf(types.Usize, 2), 8},
	}
	for _, c := range cases {
		got, err := Size(c.typ, testProvider{}, target32)
		if err != nil {
			t.Errorf("%s: %v", c.typ, err)
			continue
		}
		if got != c.size {
			t.Errorf("%s: size = %d, want %d", c.typ, got, c.size)
		}
	}
}

func TestStructReordersFields(t *testing.T) {
	s := named(types.KindStruct, "S")
	p := testProvider{fields: map[string][]Field{
		s.ID: {
			{Name: "a", Type: types.Int8},
			{Name: "b", Type: types.Int64},
			{Name: "c", Type: types.Int8},
		},
	}}
	size, err := Size(s, p, target64)
	if err != nil {
		t.Fatal(err)
	}
	if size != 16 {
		t.Errorf("size = %d, want 16", size)
	}
	placed, err := Fields(s, p, target64)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name   string
		offset uint64
	}{{"b", 0}, {"a", 8}, {"c", 9}}
	for i, w := range want {
		if placed[i].Name != w.name || placed[i].Offset != w.offset {
			t.Errorf("field %d = %s@%d, want %s@%d",
				i, placed[i].Name, placed[i].Offset, w.name, w.offset)
		}
	}
}

func TestExternStructKeepsDeclarationOrder(t *testing.T) {
	s := named(types.KindExternStruct, "C")
	p := testProvider{fields: map[string][]Field{
		s.ID: {
			{Name: "a", Type: types.Int8},
			{Name: "b", Type: types.Int64},
			{Name: "c", Type: types.Int8},
		},
	}}
	size, err := Size(s, p, target64)
	if err != nil {
		t.Fatal(err)
	}
	if size != 24 {
		t.Errorf("size = %d, want 24", size)
	}
	placed, err := Fields(s, p, target64)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name   string
		offset uint64
	}{{"a", 0}, {"b", 8}, {"c", 16}}
	for i, w := range want {
		if placed[i].Name != w.name || placed[i].Offset != w.offset {
			t.Errorf("field %d = %s@%d, want %s@%d",
				i, placed[i].Name, placed[i].Offset, w.name, w.offset)
		}
	}
}

func TestUnionLayout(t *testing.T) {
	u := named(types.KindUnion, "U")
	p := testProvider{fields: map[string][]Field{
		u.ID: {
			{Name: "x", Type: types.Int32},
			{Name: "y", Type: types.Float64},
			{Name: "z", Type: types.Uint8},
		},
	}}
	size, err := Size(u, p, target64)
	if err != nil {
		t.Fatal(err)
	}
	if size != 8 {
		t.Errorf("size = %d, want 8", size)
	}
	placed, err := Fields(u, p, target64)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range placed {
		if f.Offset != 0 {
			t.Errorf("field %s: offset = %d, want 0", f.Name, f.Offset)
		}
	}
}

func TestEnumLayout(t *testing.T) {
	e := named(types.KindEnum, "Msg")
	p := testProvider{variants: map[string][]Variant{
		e.ID: {
			{Name: "Ping"},
			{Name: "Text", Payload: []*types.Type{types.Str}},
			{Name: "Pair", Payload: []*types.Type{types.Int32, types.Int32}},
		},
	}}
	size, err := Size(e, p, target64)
	if err != nil {
		t.Fatal(err)
	}
	// The tag is one byte; the payload union holds the fat pointer, which
	// aligns the payload to eight bytes.
	if size != 24 {
		t.Errorf("size = %d, want 24", size)
	}
	align, err := Align(e, p, target64)
	if err != nil {
		t.Fatal(err)
	}
	if align != 8 {
		t.Errorf("align = %d, want 8", align)
	}
}

func TestEnumTagWidth(t *testing.T) {
	e := named(types.KindEnum, "Big")
	variants := make([]Variant, 300)
	for i := range variants {
		variants[i] = Variant{Name: "V"}
	}
	p := testProvider{variants: map[string][]Variant{e.ID: variants}}
	size, err := Size(e, p, target64)
	if err != nil {
		t.Fatal(err)
	}
	if size != 2 {
		t.Errorf("size = %d, want 2", size)
	}
}

func TestOptionLayout(t *testing.T) {
	option := types.Option.Instantiate(types.Int64)
	p := testProvider{variants: map[string][]Variant{
		option.ID: {
			{Name: "None"},
			{Name: "Some", Payload: []*types.Type{types.Int64}},
		},
	}}
	size, err := Size(option, p, target64)
	if err != nil {
		t.Fatal(err)
	}
	// One tag byte aligned to the payload, then the payload itself.
	if size != 16 {
		t.Errorf("size = %d, want 16", size)
	}
}

func TestZeroFieldStruct(t *testing.T) {
	s := named(types.KindStruct, "Empty")
	p := testProvider{fields: map[string][]Field{s.ID: {}}}
	size, err := Size(s, p, target64)
	if err != nil {
		t.Fatal(err)
	}
	if size != 1 {
		t.Errorf("size = %d, want 1", size)
	}
	align, err := Align(s, p, target64)
	if err != nil {
		t.Fatal(err)
	}
	if align != 1 {
		t.Errorf("align = %d, want 1", align)
	}
}

func TestUnknownLayouts(t *testing.T) {
	param := types.Param("f$T", "T")
	if _, err := Size(param, testProvider{}, target64); !errors.Is(err, ErrUnknown) {
		t.Errorf("a type parameter: err = %v, want ErrUnknown", err)
	}
	missing := named(types.KindStruct, "Missing")
	if _, err := Size(missing, testProvider{}, target64); !errors.Is(err, ErrUnknown) {
		t.Errorf("an unknown declaration: err = %v, want ErrUnknown", err)
	}
	if _, err := Size(types.TypeValue, testProvider{}, target64); err == nil {
		t.Error("a type value has no size")
	}
}

func TestValueCycleIsAnError(t *testing.T) {
	a := named(types.KindExternStruct, "A")
	b := named(types.KindExternStruct, "B")
	p := testProvider{fields: map[string][]Field{
		a.ID: {{Name: "b", Type: b}},
		b.ID: {{Name: "a", Type: a}},
	}}
	if _, err := Size(a, p, target64); err == nil {
		t.Error("a value type that contains itself must report an error")
	}
}

func TestOffsetOf(t *testing.T) {
	s := named(types.KindExternStruct, "C")
	p := testProvider{fields: map[string][]Field{
		s.ID: {{Name: "a", Type: types.Int32}, {Name: "b", Type: types.Int64}},
	}}
	off, err := Offset(s, "b", p, target64)
	if err != nil {
		t.Fatal(err)
	}
	if off != 8 {
		t.Errorf("offset = %d, want 8", off)
	}
	if _, err := Offset(s, "missing", p, target64); err == nil {
		t.Error("an unknown field must report an error")
	}
}
