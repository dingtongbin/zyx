// Built-in and pre-imported types.

package types

import "sort"

// The basic types. Each is a singleton; Identical compares them by pointer
// or kind.
var (
	Bool      = basic(KindBool)
	Int8      = basic(KindInt8)
	Int16     = basic(KindInt16)
	Int32     = basic(KindInt32)
	Int64     = basic(KindInt64)
	Int128    = basic(KindInt128)
	Uint8     = basic(KindUint8)
	Uint16    = basic(KindUint16)
	Uint32    = basic(KindUint32)
	Uint64    = basic(KindUint64)
	Uint128   = basic(KindUint128)
	Usize     = basic(KindUsize)
	Float32   = basic(KindFloat32)
	Float64   = basic(KindFloat64)
	Char      = basic(KindChar)
	Str       = basic(KindStr)
	Raw       = basic(KindRaw)
	TypeValue = basic(KindTypeValue)
)

// basic returns the singleton of a basic kind.
func basic(k Kind) *Type {
	name := kindNames[k]
	return &Type{Kind: k, ID: name, Name: name}
}

// The pre-imported types. Arity is the number of type parameters each
// declares; instantiate one with Instantiate.
var (
	// Option and Result are the pre-imported enums.
	Option = Named(KindEnum, "", "", "Option", 1)
	Result = Named(KindEnum, "", "", "Result", 2)
	// The containers are opaque runtime handles.
	Array         = Named(KindContainer, "", "", "Array", 1)
	Map           = Named(KindContainer, "", "", "Map", 2)
	Set           = Named(KindContainer, "", "", "Set", 1)
	PriorityQueue = Named(KindContainer, "", "", "PriorityQueue", 1)
	BlockingQueue = Named(KindContainer, "", "", "BlockingQueue", 1)
	// JoinHandle is the coroutine handle a spawn returns.
	JoinHandle = Named(KindContainer, "", "", "JoinHandle", 1)
	// Duration is the predeclared value type with built-in arithmetic.
	Duration = Named(KindDuration, "", "", "Duration", 0)
	// PanicError carries the message and location of a failed coroutine.
	PanicError = Named(KindStruct, "", "", "PanicError", 0)
	// Value is the pre-imported dynamic value.
	Value = Named(KindEnum, "", "", "Value", 0)
	// The built-in interfaces.
	Eq       = Named(KindInterface, "", "", "Eq", 0)
	Hash     = Named(KindInterface, "", "", "Hash", 0)
	Show     = Named(KindInterface, "", "", "Show", 0)
	SyncSafe = Named(KindInterface, "", "", "SyncSafe", 0)
)

// basics maps every basic kind to its singleton.
var basics = map[Kind]*Type{
	KindBool:      Bool,
	KindInt8:      Int8,
	KindInt16:     Int16,
	KindInt32:     Int32,
	KindInt64:     Int64,
	KindInt128:    Int128,
	KindUint8:     Uint8,
	KindUint16:    Uint16,
	KindUint32:    Uint32,
	KindUint64:    Uint64,
	KindUint128:   Uint128,
	KindUsize:     Usize,
	KindFloat32:   Float32,
	KindFloat64:   Float64,
	KindChar:      Char,
	KindStr:       Str,
	KindRaw:       Raw,
	KindTypeValue: TypeValue,
}

// Basic returns the singleton of a basic kind, or nil for other kinds.
func Basic(k Kind) *Type { return basics[k] }

// predeclared maps every pre-imported name to its type.
var predeclared = map[string]*Type{
	"bool":          Bool,
	"i8":            Int8,
	"i16":           Int16,
	"i32":           Int32,
	"i64":           Int64,
	"i128":          Int128,
	"u8":            Uint8,
	"u16":           Uint16,
	"u32":           Uint32,
	"u64":           Uint64,
	"u128":          Uint128,
	"usize":         Usize,
	"f32":           Float32,
	"f64":           Float64,
	"char":          Char,
	"str":           Str,
	"raw":           Raw,
	"type":          TypeValue,
	"Option":        Option,
	"Result":        Result,
	"Array":         Array,
	"Map":           Map,
	"Set":           Set,
	"PriorityQueue": PriorityQueue,
	"BlockingQueue": BlockingQueue,
	"JoinHandle":    JoinHandle,
	"Duration":      Duration,
	"PanicError":    PanicError,
	"Value":         Value,
	"Eq":            Eq,
	"Hash":          Hash,
	"Show":          Show,
	"SyncSafe":      SyncSafe,
}

// Predeclared returns the pre-imported type named name, or nil. The result
// is the uninstantiated form of a generic type.
func Predeclared(name string) *Type { return predeclared[name] }

// Names lists the pre-imported names in a stable order, for diagnostics
// and for the universe the checker builds.
func Names() []string {
	names := make([]string, 0, len(predeclared))
	for name := range predeclared {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Instantiate returns the named type with the given type arguments. It
// keeps the declaration identity, so Array<i64> and Array<str> stay the
// same type form with different arguments.
func (t *Type) Instantiate(args ...*Type) *Type {
	out := *t
	out.Args = args
	return &out
}
