package types

import (
	"strings"
	"testing"
)

func TestBasicTypes(t *testing.T) {
	cases := map[string]*Type{
		"bool":  Bool,
		"i8":    Int8,
		"i128":  Int128,
		"u64":   Uint64,
		"usize": Usize,
		"f32":   Float32,
		"char":  Char,
		"str":   Str,
		"raw":   Raw,
		"type":  TypeValue,
	}
	for want, got := range cases {
		if got.String() != want {
			t.Errorf("String() = %q, want %q", got.String(), want)
		}
		if !got.IsBasic() {
			t.Errorf("%s: IsBasic() = false", want)
		}
	}
	if Basic(KindInt32) != Int32 {
		t.Error("Basic(KindInt32) is not the Int32 singleton")
	}
	if Basic(KindStruct) != nil {
		t.Error("Basic of a named kind must be nil")
	}
}

func TestCompositeText(t *testing.T) {
	cases := []struct {
		typ  *Type
		want string
	}{
		{SliceOf(Str), "[str]"},
		{ArrayOf(Int64, 4), "[i64; 4]"},
		{TupleOf(Int64, Str), "(i64, str)"},
		{FuncOf(Bool, Int64), "func(i64) -> bool"},
		{FuncOf(nil, Int64), "func(i64)"},
		{ChanOf(Int32), "chan<i32>"},
		{Array.Instantiate(Uint8), "Array<u8>"},
		{Map.Instantiate(Str, Int64), "Map<str, i64>"},
		{Array.Instantiate(Array.Instantiate(Int32)), "Array<Array<i32>>"},
		{Pair(), "Pair<i64, str>"},
		{Param("server.Pair$T", "T"), "T"},
		{ChanOf(SliceOf(Uint8)), "chan<[u8]>"},
	}
	for _, c := range cases {
		if got := c.typ.String(); got != c.want {
			t.Errorf("String() = %q, want %q", got, c.want)
		}
	}
}

// Pair returns a named generic user type for the text tests.
func Pair() *Type {
	return Named(KindStruct, "example.com/app/server", "", "Pair", 2,
		Int64, Str)
}

func TestNamedTypeText(t *testing.T) {
	local := Named(KindStruct, "example.com/app/server", "", "Conn", 0)
	if got := local.String(); got != "Conn" {
		t.Errorf("unqualified String() = %q, want Conn", got)
	}
	foreign := Named(KindStruct, "std/net", "net", "Conn", 0)
	if got := foreign.String(); got != "net.Conn" {
		t.Errorf("qualified String() = %q, want net.Conn", got)
	}
}

func TestIdentical(t *testing.T) {
	cases := []struct {
		name string
		a, b *Type
		want bool
	}{
		{"basic same", Int32, Int32, true},
		{"basic different", Int32, Uint32, false},
		{"slice", SliceOf(Str), SliceOf(Str), true},
		{"slice of different", SliceOf(Str), SliceOf(Int32), false},
		{"array length", ArrayOf(Int64, 2), ArrayOf(Int64, 3), false},
		{"tuple", TupleOf(Int64, Str), TupleOf(Int64, Str), true},
		{"tuple order", TupleOf(Int64, Str), TupleOf(Str, Int64), false},
		{"func result", FuncOf(Bool, Int64), FuncOf(Bool, Int64), true},
		{"func result differs", FuncOf(Bool, Int64), FuncOf(Str, Int64), false},
		{"generic args", Array.Instantiate(Int64), Array.Instantiate(Int64), true},
		{"generic args differ", Array.Instantiate(Int64), Array.Instantiate(Str), false},
		{"named by declaration", Pair(), Pair(), true},
		{
			"named by different declaration",
			Named(KindStruct, "a", "", "Conn", 0),
			Named(KindStruct, "b", "", "Conn", 0),
			false,
		},
		{
			"named ignores the display qualifier",
			Named(KindStruct, "std/net", "net", "Conn", 0),
			Named(KindStruct, "std/net", "net", "Conn", 0),
			true,
		},
		{"param same declaration", Param("f$T", "T"), Param("f$T", "T"), true},
		{"param different declaration", Param("f$T", "T"), Param("g$T", "T"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Identical(c.a, c.b); got != c.want {
				t.Errorf("Identical() = %v, want %v", got, c.want)
			}
			if got := Identical(c.b, c.a); got != c.want {
				t.Errorf("Identical() reversed = %v, want %v", got, c.want)
			}
		})
	}
	// A nil type is the absence of a result, so two functions that return
	// nothing match, and a function that returns something does not match
	// one that returns nothing.
	if !Identical(nil, nil) {
		t.Error("Identical(nil, nil) must be true: neither has a result")
	}
	if Identical(nil, Int64) || Identical(Int64, nil) {
		t.Error("a function that returns a value must not match one that returns nothing")
	}
	if !Identical(FuncOf(nil, Int64), FuncOf(nil, Int64)) {
		t.Error("two functions with no result must match")
	}
	if Identical(FuncOf(nil, Int64), FuncOf(Int64, Int64)) {
		t.Error("a result must tell two functions apart")
	}
	if Identical(Int32, nil) {
		t.Error("Identical with nil must be false")
	}
}

func TestInstantiateKeepsTheDeclaration(t *testing.T) {
	inst := Array.Instantiate(Int64)
	if Array.Arity != 1 {
		t.Errorf("Array.Arity = %d, want 1", Array.Arity)
	}
	if len(Array.Args) != 0 {
		t.Errorf("Instantiate mutated the predeclared type: %v", Array.Args)
	}
	if inst.ID != Array.ID {
		t.Errorf("instance ID = %q, want %q", inst.ID, Array.ID)
	}
	if inst.Arity != 1 {
		t.Errorf("instance Arity = %d, want 1", inst.Arity)
	}
}

func TestSubstitute(t *testing.T) {
	tp := Param("server.Map$T", "T")
	up := Param("server.Map$U", "U")
	pattern := FuncOf(
		Array.Instantiate(tp),
		SliceOf(tp),
		Map.Instantiate(up, tp),
	)
	bindings := map[string]*Type{tp.ID: Int64, up.ID: Str}
	got := Substitute(pattern, bindings).String()
	want := "func([i64], Map<str, i64>) -> Array<i64>"
	if got != want {
		t.Errorf("Substitute() = %q, want %q", got, want)
	}
	if ContainsTypeParam(Substitute(pattern, bindings)) {
		t.Error("a fully substituted type must not mention a parameter")
	}
	if !ContainsTypeParam(pattern) {
		t.Error("the pattern mentions parameters")
	}
	// An unbound parameter survives substitution.
	partial := Substitute(Array.Instantiate(tp), map[string]*Type{up.ID: Str})
	if partial.String() != "Array<T>" {
		t.Errorf("partial Substitute() = %q, want Array<T>", partial.String())
	}
}

func TestParamIDs(t *testing.T) {
	tp := Param("server.Map$T", "T")
	up := Param("server.Map$U", "U")
	typ := FuncOf(Array.Instantiate(up), tp, tp)
	got := ParamIDs(typ)
	if len(got) != 2 || got[0] != tp.ID || got[1] != up.ID {
		t.Errorf("ParamIDs() = %v, want [%s %s]", got, tp.ID, up.ID)
	}
}

func TestBindings(t *testing.T) {
	tp := Param("server.Map$T", "T")
	up := Param("server.Map$U", "U")
	pattern := Map.Instantiate(tp, up)

	got, ok := Bindings(pattern, Map.Instantiate(Str, Int64))
	if !ok {
		t.Fatal("Bindings() failed")
	}
	if !Identical(got[tp.ID], Str) || !Identical(got[up.ID], Int64) {
		t.Errorf("Bindings() = %v, want T=str U=i64", got)
	}

	if _, ok := Bindings(pattern, Set.Instantiate(Str)); ok {
		t.Error("Bindings() must fail on different declarations")
	}
	if _, ok := Bindings(Map.Instantiate(tp, tp), Map.Instantiate(Str, Int64)); ok {
		t.Error("Bindings() must fail when one parameter gets two types")
	}
	if _, ok := Bindings(Map.Instantiate(tp, tp), Map.Instantiate(Str, Str)); !ok {
		t.Error("Bindings() must accept a repeated parameter bound consistently")
	}
}

func TestValueAndReference(t *testing.T) {
	cases := []struct {
		name      string
		typ       *Type
		value     bool
		reference bool
		inline    bool
	}{
		{"i32", Int32, true, false, true},
		{"str", Str, false, true, false},
		{"slice", SliceOf(Int32), false, true, false},
		{"extern struct", Named(KindExternStruct, "server", "", "Cfg", 0), true, false, true},
		{"union", Named(KindUnion, "server", "", "U", 0), true, false, true},
		{"func", FuncOf(Bool, Int32), true, false, true},
		{"Duration", Duration, true, false, true},
		{"struct", Named(KindStruct, "server", "", "S", 0), false, true, false},
		{"enum", Named(KindEnum, "server", "", "E", 0), false, true, false},
		{"interface", Named(KindInterface, "server", "", "I", 0), false, true, false},
		{"chan", ChanOf(Int32), false, true, false},
		{"container", Array.Instantiate(Int32), false, true, false},
		{"tuple of values", TupleOf(Int32, Bool), true, false, true},
		{"tuple with a view", TupleOf(Str, Int32), false, false, true},
		{"array of values", ArrayOf(Int32, 4), true, false, true},
		{"array of views", ArrayOf(Str, 4), false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.typ.IsValue(); got != c.value {
				t.Errorf("IsValue() = %v, want %v", got, c.value)
			}
			if got := c.typ.IsReference(); got != c.reference {
				t.Errorf("IsReference() = %v, want %v", got, c.reference)
			}
			if got := c.typ.IsInline(); got != c.inline {
				t.Errorf("IsInline() = %v, want %v", got, c.inline)
			}
		})
	}
}

func TestAssignable(t *testing.T) {
	if !Assignable(Int32, Int32) {
		t.Error("identical types must be assignable")
	}
	if Assignable(Int64, Int32) {
		t.Error("the language has no implicit numeric conversion")
	}
	if Assignable(SliceOf(Int32), ArrayOf(Int32, 4)) {
		t.Error("a slice and an array are different types")
	}
	if !NeedsImplementation(Show, Named(KindStruct, "server", "", "S", 0)) {
		t.Error("assigning a concrete type to an interface needs the checker")
	}
	if NeedsImplementation(Int32, Int32) {
		t.Error("identical types need no implementation check")
	}
}

func TestConvertible(t *testing.T) {
	cases := []struct {
		dst, src *Type
		want     bool
	}{
		{Int64, Int32, true},
		{Int32, Int64, true},
		{Uint8, Int32, true},
		{Float64, Int64, true},
		{Int64, Float32, true},
		{Char, Uint32, true},
		{Uint32, Char, true},
		{Raw, Usize, true},
		{Usize, Raw, true},
		{Bool, Int32, false},
		{Int32, Bool, false},
		{Str, Int32, false},
		{Int32, Str, false},
		{Char, Int32, false},
		{Int32, Char, false},
		{Raw, Int32, false},
		{SliceOf(Int32), ArrayOf(Int32, 4), false},
	}
	for _, c := range cases {
		if got := Convertible(c.dst, c.src); got != c.want {
			t.Errorf("Convertible(%s, %s) = %v, want %v",
				c.dst, c.src, got, c.want)
		}
	}
}

func TestClassification(t *testing.T) {
	if !Int32.IsNumeric() || !Float64.IsNumeric() || Int32.IsFloat() {
		t.Error("numeric classification is wrong")
	}
	if !Int32.IsSigned() || !Usize.IsUnsigned() || Int32.IsUnsigned() {
		t.Error("signedness classification is wrong")
	}
	if Char.IsNumeric() {
		t.Error("char is not numeric")
	}
	if !Char.IsOrdered() || !Int32.IsOrdered() || Str.IsOrdered() {
		t.Error("ordering classification is wrong")
	}
	if !Str.IsIterable() || !Array.Instantiate(Int32).IsIterable() {
		t.Error("str and Array are iterable")
	}
	if !Set.Instantiate(Int32).IsIterable() || !Map.Instantiate(Str, Int32).IsIterable() {
		t.Error("Set and Map are iterable")
	}
	if PriorityQueue.Instantiate(Int32).IsIterable() {
		t.Error("PriorityQueue is not iterable")
	}
	if !ArrayOf(Int32, 4).IsIterable() || Int32.IsIterable() {
		t.Error("fixed-size arrays are iterable and scalars are not")
	}
}

func TestPredeclaredTable(t *testing.T) {
	if Predeclared("i64") != Int64 {
		t.Error("Predeclared(i64) is not the Int64 singleton")
	}
	if Predeclared("Array") != Array {
		t.Error("Predeclared(Array) is not the Array singleton")
	}
	if Predeclared("nope") != nil {
		t.Error("Predeclared of an unknown name must be nil")
	}
	names := Names()
	if len(names) == 0 {
		t.Fatal("Names() is empty")
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("Names() is not sorted: %v", names)
		}
	}
	if !strings.Contains(strings.Join(names, ","), "SyncSafe") {
		t.Errorf("Names() is missing the built-in interfaces: %v", names)
	}
}
