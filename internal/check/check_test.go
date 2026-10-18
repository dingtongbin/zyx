package check

import (
	"testing"

	"zyx/internal/ast"
	"zyx/internal/cheader"
	"zyx/internal/diag"
	"zyx/internal/parser"
	"zyx/internal/resolve"
	"zyx/internal/source"
	"zyx/internal/toolchain"
)

// parse parses one source string and fails the test on syntax errors.
func parse(t *testing.T, src string) *ast.File {
	t.Helper()
	file, diags := parser.Parse(source.NewFile("test.zyx", []byte(src)))
	if len(diags) != 0 {
		t.Fatalf("parse diagnostics: %v", diags)
	}
	if file == nil {
		t.Fatal("parser returned no tree")
	}
	return file
}

// resolveDeps supplies dependency surfaces to resolution.
type resolveDeps map[string]*resolve.Interface

func (d resolveDeps) Lookup(path string) (*resolve.Interface, bool) {
	itf, ok := d[path]
	return itf, ok
}

// checkDeps supplies dependency surfaces to the checker.
type checkDeps map[string]*Interface

func (d checkDeps) Lookup(path string) (*Interface, bool) {
	itf, ok := d[path]
	return itf, ok
}

// checkSource resolves and checks one package for the host target.
func checkSource(t *testing.T, path string, rimports resolve.Imports, cimports Imports, src string) (*Package, []diag.Diagnostic) {
	t.Helper()
	pkg := &resolve.Package{Path: path, Files: []*ast.File{parse(t, src)}}
	info, rdiags := resolve.Resolve(pkg, rimports)
	if len(rdiags) != 0 {
		t.Fatalf("resolve diagnostics: %v", rdiags)
	}
	return Check(pkg, info, cimports, toolchain.HostTarget(), cTestTable())
}

// cTestTable is the C surface the checker tests call through. It declares the
// functions they name as variadic, so any argument fits: the tests walk a C
// call, and a header's own signatures are checked by their own test.
func cTestTable() map[string]*cheader.Table {
	return map[string]*cheader.Table{"test.h": {Functions: map[string]*cheader.Func{
		"printf": {Name: "printf", Variadic: true},
		"scanf":  {Name: "scanf", Variadic: true},
		"fwrite": {Name: "fwrite", Variadic: true},
	}}}
}

// TestConstantTakesTheTypeItsContextExpects checks the rule that an expression
// with an expected type is evaluated with it: a constant written beside an i32 is
// a value of that type, and one whose value does not fit is still an error rather
// than a value narrowed to fit.
func TestConstantTakesTheTypeItsContextExpects(t *testing.T) {
	fits := `package server

func f(n: i32) -> bool {
	return n < 640 * 400
}
`
	if _, diags := checkSource(t, "server", nil, nil, fits); len(diags) != 0 {
		t.Fatalf("a constant that fits was rejected: %v", codes(diags))
	}
	tooBig := `package server

func f(n: i32) -> bool {
	return n < 4000000 * 4000
}
`
	_, diags := checkSource(t, "server", nil, nil, tooBig)
	wantCode(t, diags, diag.CodeTypeMismatch)
}

// checkedDep resolves and checks a dependency package.
func checkedDep(t *testing.T, path, src string) (*resolve.Interface, *Interface) {
	t.Helper()
	pkg := &resolve.Package{Path: path, Files: []*ast.File{parse(t, src)}}
	info, rdiags := resolve.Resolve(pkg, nil)
	if len(rdiags) != 0 {
		t.Fatalf("resolve %s: %v", path, rdiags)
	}
	out, cdiags := Check(pkg, info, nil, toolchain.HostTarget(), nil)
	if len(cdiags) != 0 {
		t.Fatalf("check %s: %v", path, cdiags)
	}
	return info.Interface, out.Interface
}

// codes lists the diagnostic codes in order.
func codes(diags []diag.Diagnostic) []diag.Code {
	out := make([]diag.Code, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}

// wantCode fails unless the diagnostics contain the code.
func wantCode(t *testing.T, diags []diag.Diagnostic, code diag.Code) {
	t.Helper()
	for _, d := range diags {
		if d.Code == code {
			return
		}
	}
	t.Fatalf("want %s, got %v", code, codes(diags))
}

// messages renders the diagnostics with their messages, for test output.
func messages(diags []diag.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, string(d.Code)+": "+d.Message)
	}
	return out
}

// wantClean fails when a package reports any diagnostic.
func wantClean(t *testing.T, diags []diag.Diagnostic) {
	t.Helper()
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", messages(diags))
	}
}

func TestDeclaredTypes(t *testing.T) {
	src := `package server

struct Conn {
	host: str
}

enum Color {
	Red
	Green
}

interface Speaker {
	func Speak() -> str
}

extern struct Cfg {
	n: i32
}

extern union Bits {
	as_int: i32
	as_float: f32
}

type Port = u16
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	cases := map[string]string{
		"Conn":    "Conn",
		"Color":   "Color",
		"Speaker": "Speaker",
		"Cfg":     "Cfg",
		"Bits":    "Bits",
		"Port":    "u16",
	}
	for name, want := range cases {
		got := out.Types[name]
		if got == nil {
			t.Errorf("%s: missing from Types", name)
			continue
		}
		if got.String() != want {
			t.Errorf("%s: type = %q, want %q", name, got, want)
		}
	}
}

func TestAliasIsTransparent(t *testing.T) {
	src := `package server

type Port = u16

struct Conn {
	port: Port
}

func Listen(p: Port) -> Conn {
	return Conn { port: p }
}
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	d := out.Declared["server.Conn"]
	if d == nil {
		t.Fatal("Conn is not declared")
	}
	if got := d.Field("port").Type.String(); got != "u16" {
		t.Errorf("field type = %q, want u16", got)
	}
	if got := out.Decls["Listen"].String(); got != "func(u16) -> Conn" {
		t.Errorf("Listen type = %q, want func(u16) -> Conn", got)
	}
}

func TestMemberTable(t *testing.T) {
	src := `package server

struct Conn {
	host: str
	func Close() {
	}
	func Dial(addr: str, port: u16) -> bool => true
	const Version: i32 = 1
}
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	d := out.Declared["server.Conn"]
	if d == nil {
		t.Fatal("Conn is not declared")
	}
	if len(d.Fields) != 1 || d.Field("host") == nil {
		t.Errorf("fields = %v", d.Fields)
	}
	if got := d.Methods["Close"].String(); got != "func()" {
		t.Errorf("Close = %q, want func()", got)
	}
	if got := d.Methods["Dial"].String(); got != "func(str, u16) -> bool" {
		t.Errorf("Dial = %q, want func(str, u16) -> bool", got)
	}
	if _, ok := d.Consts["Version"]; !ok {
		t.Errorf("Version is missing from the constants")
	}
}

func TestEnumVariants(t *testing.T) {
	src := `package server

enum Message {
	Ping
	Text(str)
	Pair(i32, i32)
}
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	d := out.Declared["server.Message"]
	if d == nil {
		t.Fatal("Message is not declared")
	}
	if len(d.Variants) != 3 {
		t.Fatalf("variants = %d, want 3", len(d.Variants))
	}
	if got := d.Variants[1].Payload[0].String(); got != "str" {
		t.Errorf("Text payload = %q, want str", got)
	}
	if got := len(d.Variants[2].Payload); got != 2 {
		t.Errorf("Pair payload = %d, want 2", got)
	}
}

func TestTypeArity(t *testing.T) {
	cases := map[string]string{
		"too many": `package server

struct S {
	cells: Array<i64, str>
}
`,
		"too few": `package server

struct S {
	cells: Map<str>
}
`,
		"non generic": `package server

struct S {
	n: i64<i32>
}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, src)
			wantCode(t, diags, diag.CodeTypeArity)
		})
	}
}

func TestArrayLength(t *testing.T) {
	// A constant length is settled at compile time, and it may be written
	// as an expression over constants.
	src := `package server

const N = 4

struct Buffer {
	bytes: [u8; N]
	sums: [i32; N * 2]
}

var table: [i32; 3] = [1, 2, 3]
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)

	// A length that is not a constant is rejected.
	src = `package server

func f(n: usize) {
	var buf: [u8; n] = [0, 0, 0, 0]
}
`
	_, diags = checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeArrayLength)
}

func TestArrayLiteralLength(t *testing.T) {
	src := `package server

var table: [i32; 3] = [1, 2]
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeArrayLength)
}

func TestExternFieldRestriction(t *testing.T) {
	cases := map[string]string{
		"str": `package server

extern struct Cfg {
	name: str
}
`,
		"container": `package server

extern struct Cfg {
	names: Array<str>
}
`,
		"union": `package server

extern union Bits {
	as_str: str
}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, src)
			wantCode(t, diags, diag.CodeFieldNotABI)
		})
	}
}

func TestExternFieldAllowed(t *testing.T) {
	src := `package server

extern struct Point {
	x: f64
	y: f64
	label: [u8; 8]
	next: i32
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestEmptyExternStruct(t *testing.T) {
	src := `package server

extern struct Nothing {
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeEmptyCLayout)
}

func TestFieldRecursion(t *testing.T) {
	cases := map[string]string{
		"direct": `package server

struct Node {
	next: Node
}
`,
		"through a tuple": `package server

struct Node {
	pair: (i32, Node)
}
`,
		"through a fixed array": `package server

struct Node {
	kids: [Node; 4]
}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, src)
			wantCode(t, diags, diag.CodeFieldRecursion)
		})
	}
}

func TestRecursionThroughContainerIsAllowed(t *testing.T) {
	src := `package server

struct Node {
	kids: Array<Node>
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestInterfaceMethodBody(t *testing.T) {
	src := `package server

interface Speaker {
	func Speak() -> str {
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeInterfaceBody)
}

// TestDurationIsOrdered checks that a duration takes part in the built-in
// ordering the language promises it: the comparison of two durations is
// ordered by their nanosecond count.
func TestDurationIsOrdered(t *testing.T) {
	src := `package server

const one: Duration = dur.sec(1)
const two: Duration = dur.sec(2)

func Work() -> bool {
	return one < two
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

// TestConstDeclaredAfterItsUse checks that a function reads the type of a
// package-level binding declared after it. A body is checked in the order the
// declarations are written, so a binding declared later reaches the body with
// no type unless the declared type of the binding is read from its declaration.
func TestConstDeclaredAfterItsUse(t *testing.T) {
	src := `package server

func Digit(b: u8) -> u8 {
	return hex[usize(b >> 4)]
}

const hex: str = "0123456789abcdef"

func Other() -> i64 {
	return 1
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

// TestTypeArgumentMustImplementConstraint checks that a call reports a type
// argument the declaration's constraint does not hold for, rather than letting
// the body reach a method the argument has no implementation of. The control
// shows the check is not simply refusing every call.
func TestTypeArgumentMustImplementConstraint(t *testing.T) {
	src := `package server

enum E {
	A
}

func Use<T: Show>(v: T) -> i64 {
	return 1
}

func Work() -> i64 {
	return Use(E.A)
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeTypeArgument)

	control := `package server

func Use<T: Show>(v: T) -> i64 {
	return 1
}

func Work() -> i64 {
	return Use(1)
}
`
	_, diags = checkSource(t, "server", nil, nil, control)
	wantClean(t, diags)
}

// checkFreestanding checks one package for a target that leaves the runtime
// out.
func checkFreestanding(t *testing.T, src string) (*Package, []diag.Diagnostic) {
	t.Helper()
	pkg := &resolve.Package{Path: "server", Files: []*ast.File{parse(t, src)}}
	info, rdiags := resolve.Resolve(pkg, nil)
	if len(rdiags) != 0 {
		t.Fatalf("resolve diagnostics: %v", rdiags)
	}
	return Check(pkg, info, nil, toolchain.HostTarget(), cTestTable(), Options{Freestanding: true})
}

// TestFreestandingSubsetIsRejected checks every construct the specification
// says a target without the runtime does not have, each with its own code. The
// last case is the subset that stays: value types, control flow, raw, and a
// call into C.
func TestFreestandingSubsetIsRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
		code diag.Code
	}{
		{"spawn", `package server

func Work() {
	region {
		spawn Run()
	}
}

func Run() {
}
`, diag.CodeFreestandingSpawn},
		{"spawn_watched", `package server

func Run() {
}

func Watch(e: PanicError) {
}

func Work() {
	region {
		spawn_watched Run(), func Watch
	}
}
`, diag.CodeFreestandingSpawnWatched},
		{"chan", `package server

func Work(ch: chan<i64>) {
}
`, diag.CodeFreestandingChan},
		{"select", `package server

func Work(ch: chan<i64>) {
	select {
		recv (ch) -> v, ok {
		}
	}
}
`, diag.CodeFreestandingSelect},
		{"region", `package server

func Work() {
	region {
	}
}
`, diag.CodeFreestandingRegion},
		{"new", `package server

func Work() {
	var xs = new Array<i64>(0)
}
`, diag.CodeFreestandingNew},
		{"literal", `package server

struct Holder {
	v: i64
}

func Work() -> i64 {
	var h = Holder { v: 1 }
	return h.v
}
`, diag.CodeFreestandingLiteral},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, diags := checkFreestanding(t, c.src)
			wantCode(t, diags, c.code)
		})
	}

	t.Run("the subset that stays", func(t *testing.T) {
		src := `package server

extern func fwrite(p: raw, size: usize, count: usize, stream: raw) -> usize

struct Pair {
	v: i64
}

func Work(n: i64) -> i64 {
	var total: i64 = 0
	for i in 0 .. n {
		total = total + i
	}
	var xs: [i64; 4] = [1, 2, 3, 4]
	return total + xs[0] + n
}
`
		_, diags := checkFreestanding(t, src)
		wantClean(t, diags)
	})
}

// TestCFuncBodySubsetIsRejected checks what a `c func` body may not do. The
// body runs on the blocking-pool thread that C called it on: it has no
// coroutine to park and no region to allocate from, so the constructs that need
// either are refused, each with its own code. The last case is the subset that
// stays: value types, raw, and the Try forms, which answer instead of waiting.
func TestCFuncBodySubsetIsRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
		code diag.Code
	}{
		{"new", `package server

c func Callback() {
	var xs = new Array<i64>(0)
}
`, diag.CodeCFuncNew},
		{"literal", `package server

struct Holder {
	v: i64
}

c func Callback() {
	var h = Holder { v: 1 }
}
`, diag.CodeCFuncLiteral},
		{"chan", `package server

c func Callback(ch: chan<i64>) {
	ch.Send(1)
}
`, diag.CodeCFuncChan},
		{"select", `package server

c func Callback(ch: chan<i64>) {
	select {
		recv (ch) -> v, ok {
		}
	}
}
`, diag.CodeCFuncSelect},
		{"join", `package server

c func Callback(h: JoinHandle<i64>) {
	h.Join()
}
`, diag.CodeCFuncJoin},
		{"io", `package server

import c "test.h"

c func Callback() {
	c.zyxrt_loop_wait(0, 1)
}
`, diag.CodeCFuncIO},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, c.src)
			wantCode(t, diags, c.code)
		})
	}

	t.Run("mutex", func(t *testing.T) {
		rdep, cdep := checkedDep(t, "std/sync", `package sync

struct Mutex<T> {
	func Lock() {
	}
	func TryLock() -> bool {
		return true
	}
}
`)
		src := `package server

import "std/sync"

c func Callback(m: sync.Mutex<i64>) {
	m.Lock()
}
`
		_, diags := checkSource(t, "server",
			resolveDeps{"std/sync": rdep}, checkDeps{"std/sync": cdep}, src)
		wantCode(t, diags, diag.CodeCFuncMutex)
	})

	t.Run("the subset that stays", func(t *testing.T) {
		src := `package server

c func Callback(ch: chan<i64>, p: raw) -> usize {
	var ok = ch.TrySend(1)
	var v, got = ch.TryRecv()
	var xs: [i64; 3] = [1, 2, 3]
	if ok && got {
		return usize(v) + usize(xs[0])
	}
	return usize(p) + usize(xs[2])
}
`
		_, diags := checkSource(t, "server", nil, nil, src)
		wantClean(t, diags)
	})
}

func TestInterfaceDefaultParam(t *testing.T) {
	src := `package server

interface Speaker {
	func Speak(loud: bool = true) -> str
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeDefaultParam)
}

func TestImplComplete(t *testing.T) {
	src := `package server

interface Speaker {
	func Speak() -> str
}

struct Dog {
	impl Speaker {
		func Speak() -> str => "woof"
	}
}
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	d := out.Declared["server.Dog"]
	if len(d.Impls) != 1 {
		t.Fatalf("impls = %d, want 1", len(d.Impls))
	}
	if got := d.Impls[0].Interface.String(); got != "Speaker" {
		t.Errorf("interface = %q, want Speaker", got)
	}
}

func TestImplMissingMethod(t *testing.T) {
	src := `package server

interface Speaker {
	func Speak() -> str
	func Stop() -> bool
}

struct Dog {
	impl Speaker {
		func Speak() -> str => "woof"
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeMissingMethod)
}

func TestImplExtraMethod(t *testing.T) {
	src := `package server

interface Speaker {
	func Speak() -> str
}

struct Dog {
	impl Speaker {
		func Speak() -> str => "woof"
		func Bark() {
		}
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeExtraMethod)
}

func TestImplSignatureMismatch(t *testing.T) {
	src := `package server

interface Speaker {
	func Speak() -> str
}

struct Dog {
	impl Speaker {
		func Speak() -> i32 => 0
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeTypeMismatch)
}

func TestImplSelfSubstitution(t *testing.T) {
	src := `package server

interface Eq2 {
	func Same(other: Self) -> bool
}

struct Point {
	x: i32
	impl Eq2 {
		func Same(other: Self) -> bool => true
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestDuplicateImpl(t *testing.T) {
	src := `package server

struct Job {
	impl SyncSafe {
	}
	impl SyncSafe {
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeDuplicateImpl)
}

func TestImplNotInterface(t *testing.T) {
	src := `package server

struct Dog {
	impl i32 {
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeNotInterface)
}

func TestConstraintWeakened(t *testing.T) {
	src := `package server

struct Pair<T: Eq> {
	first: T
	impl<T> Eq {
		func Eq(other: Self) -> bool => true
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeConstraintWeakened)
}

func TestConstraintKept(t *testing.T) {
	src := `package server

struct Pair<T: Eq> {
	first: T
	impl<T: Eq> Eq {
		func Eq(other: Self) -> bool => true
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestSyncSafeEmptyImpl(t *testing.T) {
	src := `package server

struct Job {
	impl SyncSafe {
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestShowImpl(t *testing.T) {
	src := `package server

struct Server {
	host: str
	impl Show {
		func Show(w: Buf) {
			w.WriteStr("server")
		}
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

// TestIntegerLiteralRange checks that an integer literal must fit the type it
// is written with, and that a negated literal may reach the magnitude of the
// smallest negative value, which is the one value a signed type holds that has
// no positive counterpart.
func TestIntegerLiteralRange(t *testing.T) {
	cases := []struct {
		name string
		body string
		code diag.Code
	}{
		{"i8 maximum", "func f() -> i8 => 127", ""},
		{"i8 minimum", "func f() -> i8 => -128", ""},
		{"i8 overflows", "func f() -> i8 => 128", diag.CodeLiteralRange},
		{"i8 underflows", "func f() -> i8 => -129", diag.CodeLiteralRange},
		{"u8 maximum", "func f() -> u8 => 255", ""},
		{"u8 overflows", "func f() -> u8 => 256", diag.CodeLiteralRange},
		{"i32 minimum", "func f() -> i32 => -2147483648", ""},
		{"i64 minimum", "func f() -> i64 => -9223372036854775808", ""},
		{"i64 overflows", "func f() -> i64 => 9223372036854775808", diag.CodeLiteralRange},
		{"u64 maximum", "func f() -> u64 => 18446744073709551615", ""},
		{"u64 overflows", "func f() -> u64 => 18446744073709551616", diag.CodeLiteralRange},
		{"i128 minimum", "func f() -> i128 => -170141183460469231731687303715884105728", ""},
		{"i128 overflows", "func f() -> i128 => 170141183460469231731687303715884105728", diag.CodeLiteralRange},
		{"u128 maximum", "func f() -> u128 => 340282366920938463463374607431768211455", ""},
		{"u128 overflows", "func f() -> u128 => 340282366920938463463374607431768211456", diag.CodeLiteralRange},
		{"a suffix names the type", "func f() -> i8 => 127i8", ""},
		{"a suffixed literal overflows", "func f() -> i8 => 300i8", diag.CodeLiteralRange},
		{"digit separators fit", "func f() -> i32 => 1_000_000", ""},
		{"hexadecimal fits", "func f() -> u8 => 0xFF", ""},
		{"hexadecimal overflows", "func f() -> u8 => 0x100", diag.CodeLiteralRange},
		{"binary fits", "func f() -> u8 => 0b1111_1111", ""},
		{"octal fits", "func f() -> u8 => 0o377", ""},
		{"a conversion names the type", "func f() -> u64 => u64(18446744073709551615)", ""},
		{"a conversion holds its literal to the type", "func f() -> i8 => i8(300)", diag.CodeLiteralRange},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "package server\n\n" + c.body + "\n"
			_, diags := checkSource(t, "server", nil, nil, src)
			if c.code == "" {
				wantClean(t, diags)
				return
			}
			wantCode(t, diags, c.code)
		})
	}
}

func TestExternFuncBody(t *testing.T) {
	src := `package server

extern func strlen(s: str) -> usize {
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeExternBody)
}

func TestMissingBody(t *testing.T) {
	src := `package server

func Work() {
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	src = `package server

func Work()
`
	_, diags = checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeMissingBody)
}

func TestVariadic(t *testing.T) {
	cases := map[string]string{
		"not extern": `package server

func Work(fmt: str, ...) {
}
`,
		"not last": `package server

extern func Work(fmt: str, ..., n: i32)
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, src)
			wantCode(t, diags, diag.CodeVariadic)
		})
	}
}

func TestVariadicExtern(t *testing.T) {
	src := `package server

extern func printf(fmt: str, ...)
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if got := out.Decls["printf"].String(); got != "func(str)" {
		t.Errorf("printf = %q, want func(str)", got)
	}
}

func TestDefaultParamTrailing(t *testing.T) {
	src := `package server

func Connect(host: str, port: u16 = 8080, limit: u16 = 30) -> bool => true
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)

	src = `package server

func Connect(host: str = "x", port: u16) -> bool => true
`
	_, diags = checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeDefaultParam)
}

func TestMethodTypeParams(t *testing.T) {
	src := `package server

struct Conn {
	func Send<T>(v: T) {
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeMethodTypeParams)
}

func TestMainShape(t *testing.T) {
	cases := map[string]string{
		"parameters": `package main

func main(x: i32) {
}
`,
		"generic": `package main

func main<T>() {
}
`,
		"qualifier": `package main

comptime func main() {
}
`,
		"expression body": `package main

func main() => 0
`,
		"result": `package main

func main() -> bool => true
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "main", nil, nil, src)
			wantCode(t, diags, diag.CodeMainSignature)
		})
	}
}

func TestMainValid(t *testing.T) {
	src := `package main

func main() {
}
`
	_, diags := checkSource(t, "main", nil, nil, src)
	wantClean(t, diags)
}

func TestInitShape(t *testing.T) {
	cases := map[string]string{
		"no result": `package server

struct Point {
	x: i32
	func init(x: i32) {
	}
}
`,
		"wrong result": `package server

struct Point {
	x: i32
	func init(x: i32) -> i32 => 0
}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, src)
			wantCode(t, diags, diag.CodeInitSignature)
		})
	}
}

func TestInitValid(t *testing.T) {
	src := `package server

struct Point {
	x: i32
	func init(x: i32) -> Self => Point { x: x }
}
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if got := out.Declared["server.Point"].Methods["init"].String(); got != "func(i32) -> Point" {
		t.Errorf("init = %q, want func(i32) -> Point", got)
	}
}

// An init builds a value rather than reading one, so it has no receiver
// value, while Self still names the type it builds. The block body is the
// case that reads the result type while checking a return statement.
func TestInitSelfInABlockBody(t *testing.T) {
	src := `package server

struct Point {
	x: i32
	func init(x: i32) -> Self {
		return Point { x: x}
	}
}
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if got := out.Declared["server.Point"].Methods["init"].String(); got != "func(i32) -> Point" {
		t.Errorf("init = %q, want func(i32) -> Point", got)
	}
}

func TestInitHasNoReceiverValue(t *testing.T) {
	src := `package server

struct Point {
	x: i32
	func init(x: i32) -> Self {
		return Point { x: self.x}
	}
}
`
	pkg := &resolve.Package{Path: "server", Files: []*ast.File{parse(t, src)}}
	_, diags := resolve.Resolve(pkg, nil)
	if len(diags) == 0 {
		t.Error("reading self inside an init must be reported")
	}
}

func TestTopLevelVarType(t *testing.T) {
	src := `package server

var table: Map<str, i64> = Map { }
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeTopLevelVar)
}

func TestTopLevelVarAllowed(t *testing.T) {
	src := `package server

var counter: i32 = 0
var name: str = "server"
var table: [i32; 4] = [0, 0, 0, 0]
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestCrossPackageType(t *testing.T) {
	rdep, cdep := checkedDep(t, "std/net", `package net

struct Conn {
	host: str
}

func Listen(port: i32) -> Conn {
	return Conn { host: "local" }
}
`)
	src := `package server

import "std/net"

func Use(conn: net.Conn) {
}
`
	out, diags := checkSource(t, "server",
		resolveDeps{"std/net": rdep}, checkDeps{"std/net": cdep}, src)
	wantClean(t, diags)
	if got := out.Decls["Use"].String(); got != "func(net.Conn)" {
		t.Errorf("Use = %q, want func(net.Conn)", got)
	}
}

func TestCrossPackageGenericType(t *testing.T) {
	rdep, cdep := checkedDep(t, "std/box", `package box

struct Box<T> {
	value: T
}
`)
	src := `package server

import "std/box"

func Make(b: box.Box<i64>) -> box.Box<i64> {
	return b
}
`
	out, diags := checkSource(t, "server",
		resolveDeps{"std/box": rdep}, checkDeps{"std/box": cdep}, src)
	wantClean(t, diags)
	if got := out.Decls["Make"].String(); got != "func(box.Box<i64>) -> box.Box<i64>" {
		t.Errorf("Make = %q, want func(box.Box<i64>) -> box.Box<i64>", got)
	}
}

func TestArithmeticOperands(t *testing.T) {
	cases := map[string]string{
		"mixed widths": `package server

func f(a: i32, b: i64) -> i32 => a + b
`,
		"text ordering": `package server

func f(a: str, b: str) -> bool => a < b
`,
		"unsigned negation": `package server

func f(a: u32) -> u32 => -a
`,
		"negate an integer": `package server

func f(a: i32) -> bool => !a
`,
		"bits on a float": `package server

func f(a: f64) -> f64 => a & a
`,
		"arithmetic on text": `package server

func f(a: str, b: str) -> str => a - b
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, src)
			wantCode(t, diags, diag.CodeTypeMismatch)
		})
	}
}

func TestLiteralTyping(t *testing.T) {
	src := `package server

func Narrow() -> u8 {
	var x: u8 = 5
	return x
}

func Widen(a: i32) -> i32 => 1 + a

func Suffixed() -> i8 => 42i8

func Default() -> i64 => 7
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	for name, want := range map[string]string{
		"Narrow":   "func() -> u8",
		"Widen":    "func(i32) -> i32",
		"Suffixed": "func() -> i8",
		"Default":  "func() -> i64",
	} {
		if got := out.Decls[name].String(); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestCallArguments(t *testing.T) {
	cases := map[string]struct {
		src  string
		code diag.Code
	}{
		"too few": {`package server

func g(a: i32, b: i32) -> i32 => a + b

func f() -> i32 => g(1)
`, diag.CodeArgCount},
		"too many": {`package server

func g(a: i32) -> i32 => a

func f() -> i32 => g(1, 2)
`, diag.CodeArgCount},
		"wrong type": {`package server

func g(a: i32) -> i32 => a

func f() -> i32 => g("s")
`, diag.CodeTypeMismatch},
		"defaults allow fewer": {`package server

func g(a: i32, b: i32 = 2) -> i32 => a + b

func f() -> i32 => g(1)
`, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, c.src)
			if c.code == "" {
				wantClean(t, diags)
				return
			}
			wantCode(t, diags, c.code)
		})
	}
}

func TestGenericInference(t *testing.T) {
	src := `package server

func Identity<T>(v: T) -> T {
	return v
}

func Numbers() -> i64 => Identity(5)

func Texts() -> str => Identity("s")

func Narrow() -> i32 => Identity(5)
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if got := out.Decls["Numbers"].String(); got != "func() -> i64" {
		t.Errorf("Numbers = %q, want func() -> i64", got)
	}
}

func TestGenericInferenceFails(t *testing.T) {
	src := `package server

func Empty<T>() -> Array<T> {
	return new Array<T>(0)
}

func f() -> Array<i64> {
	var xs = Empty()
	return xs
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeTypeArity)
}

func TestContainerMethods(t *testing.T) {
	src := `package server

func Fill() -> usize {
	var xs = new Array<i64>(8)
	xs.Push(1)
	xs.Push(2)
	var first = xs.Get(0)
	if first.IsSome() {
		return xs.Len()
	}
	return 0
}
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if out.Decls["Fill"] == nil {
		t.Error("Fill is not declared")
	}
}

func TestContainerMethodTypeError(t *testing.T) {
	src := `package server

func f() {
	var xs = new Array<i64>(8)
	xs.Push("s")
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeTypeMismatch)
}

func TestUnknownMethod(t *testing.T) {
	src := `package server

func f() {
	var xs = new Array<i64>(8)
	xs.PushAll(1)
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeNoMethod)
}

func TestConversion(t *testing.T) {
	cases := map[string]struct {
		src  string
		code diag.Code
	}{
		"numeric": {`package server

func f(a: i32) -> i64 => i64(a)
`, ""},
		"char": {`package server

func f(a: char) -> u32 => u32(a)
`, ""},
		"bool is excluded": {`package server

func f(a: bool) -> i32 => i32(a)
`, diag.CodeNoConversion},
		"text is excluded": {`package server

func f(a: str) -> i32 => i32(a)
`, diag.CodeNoConversion},
		"char is not numeric": {`package server

func f(a: char) -> i32 => i32(a)
`, diag.CodeNoConversion},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, c.src)
			if c.code == "" {
				wantClean(t, diags)
				return
			}
			wantCode(t, diags, c.code)
		})
	}
}

func TestNew(t *testing.T) {
	src := `package server

struct Point {
	x: i32
	func init(x: i32) -> Self => Point { x: x }
}

func Build() -> Point => new Point(3)

func Empty() -> Array<i64> => new Array<i64>(16)

func Table() -> Map<str, i64> => new Map<str, i64>()
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestNewWithoutInit(t *testing.T) {
	src := `package server

struct Point {
	x: i32
}

func Build() -> Point => new Point(3)
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeTypeMismatch)
}

func TestStructLiteralFields(t *testing.T) {
	cases := map[string]struct {
		src  string
		code diag.Code
	}{
		"missing": {`package server

struct Point {
	x: i32
	y: i32
}

func f() -> Point => Point { x: 1 }
`, diag.CodeMissingField},
		"duplicate": {`package server

struct Point {
	x: i32
}

func f() -> Point => Point { x: 1, x: 2 }
`, diag.CodeDuplicateField},
		"unknown": {`package server

struct Point {
	x: i32
}

func f() -> Point => Point { z: 1 }
`, diag.CodeNoField},
		"wrong type": {`package server

struct Point {
	x: i32
}

func f() -> Point => Point { x: "s" }
`, diag.CodeTypeMismatch},
		"enum literal": {`package server

enum Color {
	Red
}

func f() -> Color => Color { }
`, diag.CodeEnumLiteral},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, c.src)
			wantCode(t, diags, c.code)
		})
	}
}

func TestMapAndSetLiteral(t *testing.T) {
	src := `package server

func Scores() -> Map<str, i64> => Map { "alice": 90, "bob": 85 }

func Ids() -> Set<i32> => Set { 1, 2, 3 }
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestMapLiteralWrongValue(t *testing.T) {
	src := `package server

func Scores() -> Map<str, i64> => Map { "alice": "high" }
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeTypeMismatch)
}

func TestIndexAndSlice(t *testing.T) {
	src := `package server

func Bytes(s: str) -> u8 => s[0]

func Head(xs: [i32; 4]) -> i32 => xs[0]

func Part(s: str) -> str => s[0..2]

func View(xs: Array<i32>) -> [i32] => xs[0..2]
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestIndexError(t *testing.T) {
	src := `package server

struct Point {
	x: i32
}

func f(p: Point) -> i32 => p[0]
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeIndex)
}

func TestTryPropagation(t *testing.T) {
	src := `package server

func parse(s: str) -> Result<i32, str> => Result.Ok(0)

func Work(s: str) -> Result<i32, str> {
	var n = try parse(s)
	return Result.Ok(n)
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestTryMismatch(t *testing.T) {
	src := `package server

func parse(s: str) -> Result<i32, str> => Result.Ok(0)

func Work(s: str) -> Result<i32, PanicError> {
	var n = try parse(s)
	return Result.Ok(n)
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeTryResult)
}

func TestUnitPayloadResult(t *testing.T) {
	// A Result whose success carries nothing is written with the unit type and
	// built without an argument.
	src := `package server

func Wait(fd: i32) -> Result<(), str> {
	if fd < 0 {
		return Result.Err("bad")
	}
	return Result.Ok()
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestUnitPayloadIsTheOnlyOneWithoutAnArgument(t *testing.T) {
	src := `package server

func Bad() -> Result<i32, str> {
	return Result.Ok()
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeArgCount)
}

func TestSpawnNeedsRegion(t *testing.T) {
	src := `package server

func Work() {
	spawn Handle()
}

func Handle() {
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeSpawnOutsideRegion)
}

func TestSpawnInsideRegion(t *testing.T) {
	src := `package server

func Work() -> JoinHandle<i32> {
	region {
		return spawn Compute()
	}
}

func Compute() -> i32 => 0
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestSelectArms(t *testing.T) {
	src := `package server

func Work(ch: chan<i32>) {
	select {
		recv(ch) -> v, ok {
			if !ok {
				return
			}
		}
		timeout(dur.sec(1)) {
		}
		default {
		}
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeSelectArms)
}

func TestMatchExhaustiveness(t *testing.T) {
	src := `package server

enum Color {
	Red
	Green
}

func Name(col: Color) -> str {
	match col {
		Red => { return "red" }
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeMissingArm)
}

func TestMatchComplete(t *testing.T) {
	src := `package server

enum Color {
	Red
	Green
}

func Name(col: Color) -> str {
	match col {
		Red => { return "red" }
		Green => { return "green" }
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestMatchOnOption(t *testing.T) {
	src := `package server

func UnwrapOrZero(o: Option<i64>) -> i64 {
	match o {
		Some(v) => { return v }
		None => { return 0 }
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestMatchWildcard(t *testing.T) {
	src := `package server

func Sign(n: i32) -> i32 {
	match n {
		0 => { return 0 }
		_ => { return 1 }
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestReturnPaths(t *testing.T) {
	cases := map[string]struct {
		src  string
		code diag.Code
	}{
		"missing": {`package server

func f(a: i32) -> i32 {
	if a > 0 {
		return 1
	}
}
`, diag.CodeMissingReturn},
		"wrong type": {`package server

func f() -> i32 => "s"
`, diag.CodeReturnType},
		"value in a void function": {`package server

func f() {
	return 1
}
`, diag.CodeReturnType},
		"both branches": {`package server

func f(a: i32) -> i32 {
	if a > 0 {
		return 1
	} else {
		return 2
	}
}
`, ""},
		"panic path": {`package server

func f(a: i32) -> i32 {
	if a > 0 {
		return 1
	}
	panic("no")
}
`, ""},
		"match paths": {`package server

enum Flag {
	On
	Off
}

func f(flag: Flag) -> i32 {
	match flag {
		On => { return 1 }
		Off => { return 0 }
	}
}
`, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, c.src)
			if c.code == "" {
				wantClean(t, diags)
				return
			}
			wantCode(t, diags, c.code)
		})
	}
}

func TestForIn(t *testing.T) {
	src := `package server

func Sum(xs: Array<i32>) -> i32 {
	var total: i32 = 0
	for x in xs {
		total = total + x
	}
	return total
}

func Count(m: Map<str, i64>) -> i64 {
	var n: i64 = 0
	for k, v in m {
		n = n + v
	}
	return n
}

func Letters(s: str) -> i32 {
	var n: i32 = 0
	for ch in s {
		n = n + 1
	}
	return n
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestForInNotIterable(t *testing.T) {
	src := `package server

func f(n: i32) {
	for x in n {
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeNotIterable)
}

func TestFStringInterpolation(t *testing.T) {
	cases := map[string]struct {
		src  string
		code diag.Code
	}{
		"primitive": {`package server

func f(n: i32) -> str => f"count = {n}"
`, ""},
		"struct needs Show": {`package server

struct Point {
	x: i32
}

func f(p: Point) -> str => f"point = {p}"
`, diag.CodeInterpolation},
		"with Show": {`package server

struct Point {
	x: i32
	impl Show {
		func Show(w: Buf) {
			w.WriteStr("point")
		}
	}
}

func f(p: Point) -> str => f"point = {p}"
`, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, c.src)
			if c.code == "" {
				wantClean(t, diags)
				return
			}
			wantCode(t, diags, c.code)
		})
	}
}

func TestComptimeOnlyNamespaces(t *testing.T) {
	cases := map[string]struct {
		src  string
		code diag.Code
	}{
		"dur at run time": {`package server

func f() -> Duration => dur.ms(100)
`, diag.CodeComptimeOnly},
		"meta at run time": {`package server

func f() -> usize => meta.size_of(i64)
`, diag.CodeComptimeOnly},
		"dur in a constant": {`package server

const Timeout = dur.sec(1)
`, ""},
		"meta in a constant": {`package server

const Width = meta.size_of(i64)
`, ""},
		"unknown query": {`package server

const Width = meta.width_of(i64)
`, diag.CodeMetaName},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, c.src)
			if c.code == "" {
				wantClean(t, diags)
				return
			}
			wantCode(t, diags, c.code)
		})
	}
}

func TestClone(t *testing.T) {
	cases := map[string]struct {
		src  string
		code diag.Code
	}{
		"struct with a view": {`package server

struct Conn {
	host: str
}

func f(conn: Conn) -> Conn => conn.clone()
`, ""},
		"plain integer": {`package server

func f(n: i32) -> i32 => n.clone()
`, diag.CodeClone},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, c.src)
			if c.code == "" {
				wantClean(t, diags)
				return
			}
			wantCode(t, diags, c.code)
		})
	}
}

func TestAddrOf(t *testing.T) {
	cases := map[string]struct {
		src  string
		code diag.Code
	}{
		"in a C call": {`package server

import c "stdio.h"

func f() {
	var n: i32 = 1
	c.scanf(&n)
}
`, ""},
		"outside a C call": {`package server

func g(p: raw) {
}

func f() {
	var n: i32 = 1
	g(&n)
}
`, diag.CodeAddrOf},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, c.src)
			if c.code == "" {
				wantClean(t, diags)
				return
			}
			wantCode(t, diags, c.code)
		})
	}
}

func TestBuiltinMethodValue(t *testing.T) {
	src := `package server

func f() {
	var xs = new Array<i64>(8)
	var g = xs.Push
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeBuiltinMethodValue)
}

func TestVariantValue(t *testing.T) {
	cases := map[string]struct {
		src  string
		code diag.Code
	}{
		"payload free": {`package server

enum Color {
	Red
	Green
}

func f() -> Color => Color.Red
`, ""},
		"with a payload": {`package server

func f() -> Result<i32, str> => Result.Ok
`, diag.CodeEnumLiteral},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, c.src)
			if c.code == "" {
				wantClean(t, diags)
				return
			}
			wantCode(t, diags, c.code)
		})
	}
}

func TestRealisticBodies(t *testing.T) {
	src := `package server

const Limit = dur.ms(250)

struct Server {
	host: str
	port: u16
	func Label() -> str => f"{self.host}:{self.port}"
}

enum State {
	Idle
	Running(i32)
	Stopped(str)
}

func Describe(s: State) -> str {
	match s {
		Idle => { return "idle" }
		Running(n) => { return f"running {n}" }
		Stopped(why) => { return why }
	}
}

func Count(xs: Array<i32>) -> i32 {
	var total: i32 = 0
	for x in xs {
		total = total + x
	}
	return total
}

func Run(s: Server, ch: chan<i32>) -> Result<i32, PanicError> {
	region {
		spawn Count(new Array<i32>(0))
	}
	select {
		recv(ch) -> v, ok {
			if !ok {
				return Result.Ok(0)
			}
			return Result.Ok(v)
		}
		timeout(Limit) {
			return Result.Ok(-1)
		}
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestComptimeConstants(t *testing.T) {
	src := `package server

const N = 4
const M = N * 2
const Name = "server"
const Greeting = f"hi {Name} {N}"
const Mask = (1 << 3) | 1

struct Buffer {
	bytes: [u8; N]
	sums: [i32; M]
}
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if got := out.Consts["N"]; got == nil || got.Int != 4 {
		t.Errorf("N = %v, want 4", got)
	}
	if got := out.Consts["M"]; got == nil || got.Int != 8 {
		t.Errorf("M = %v, want 8", got)
	}
	if got := out.Consts["Mask"]; got == nil || got.Int != 9 {
		t.Errorf("Mask = %v, want 9", got)
	}
	if got := out.Consts["Name"]; got == nil || got.Str != "server" {
		t.Errorf("Name = %v, want server", got)
	}
	if got := out.Consts["Greeting"]; got == nil || got.Str != "hi server 4" {
		t.Errorf("Greeting = %v, want hi server 4", got)
	}
}

// TestAnnotatedConstIsAValue checks that a constant written with a type names
// a value of that type. Only the unannotated form used to record a type, so
// every use of an annotated constant read as nothing and a call that took one
// reported its argument as missing.
func TestAnnotatedConstIsAValue(t *testing.T) {
	src := `package server

const n: i64 = 5

func takes(v: i64) -> i64 => v

func f() -> i64 => takes(n)
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if got := out.Consts["n"]; got == nil || got.Int != 5 {
		t.Errorf("n = %v, want 5", got)
	}
}

func TestMetaLayoutQueries(t *testing.T) {
	src := `package server

extern struct Cfg {
	port: u16
	backlog: i32
}

const IntSize = meta.size_of(i64)
const StrSize = meta.size_of(str)
const IntAlign = meta.align_of(i64)
const PortOffset = meta.offset_of(Cfg, "port")
const BacklogOffset = meta.offset_of(Cfg, "backlog")
const FieldCount = meta.field_count(Cfg)
const FirstField = meta.field_name(Cfg, 0)
const TypeName = meta.name(i64)
const HasPort = meta.has_field(Cfg, "port")
const HasZed = meta.has_field(Cfg, "zed")
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	ptr := toolchain.HostTarget().Layout().PtrSize
	cases := map[string]uint64{
		"IntSize":       8,
		"StrSize":       2 * ptr,
		"IntAlign":      8,
		"PortOffset":    0,
		"BacklogOffset": 4,
		"FieldCount":    2,
	}
	for name, want := range cases {
		got := out.Consts[name]
		if got == nil {
			t.Errorf("%s: no value", name)
			continue
		}
		if got.Int != want {
			t.Errorf("%s = %d, want %d", name, got.Int, want)
		}
	}
	if got := out.Consts["FirstField"]; got == nil || got.Str != "port" {
		t.Errorf("FirstField = %v, want port", got)
	}
	if got := out.Consts["TypeName"]; got == nil || got.Str != "i64" {
		t.Errorf("TypeName = %v, want i64", got)
	}
	if got := out.Consts["HasPort"]; got == nil || !got.Bool {
		t.Errorf("HasPort = %v, want true", got)
	}
	if got := out.Consts["HasZed"]; got == nil || got.Bool {
		t.Errorf("HasZed = %v, want false", got)
	}
}

func TestMetaAttr(t *testing.T) {
	src := `package server

struct User {
	name: str
	const json = Map { "name": "userName" }
}

const Field = meta.attr(User, "json").Get("name").UnwrapOr("name")
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if got := out.Consts["Field"]; got == nil || got.Str != "userName" {
		t.Errorf("Field = %v, want userName", got)
	}
}

func TestComptimeRequired(t *testing.T) {
	cases := map[string]string{
		"constant": `package server

func now() -> i64 => 0

const T = now()
`,
		"top-level var": `package server

func now() -> i64 => 0

var started = now()
`,
		"default value": `package server

func now() -> i64 => 0

func g(a: i64 = now()) -> i64 => a
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, src)
			wantCode(t, diags, diag.CodeComptimeRequired)
		})
	}
}

func TestContainerLiteralInference(t *testing.T) {
	src := `package server

func Ids() -> Set<i64> => Set { 1, 2, 3 }

func Scores() -> Map<str, i64> => Map { "alice": 90 }
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if got := out.Decls["Ids"].String(); got != "func() -> Set<i64>" {
		t.Errorf("Ids = %q, want func() -> Set<i64>", got)
	}
	if got := out.Decls["Scores"].String(); got != "func() -> Map<str, i64>" {
		t.Errorf("Scores = %q, want func() -> Map<str, i64>", got)
	}
}

func TestDeferredLayoutQuery(t *testing.T) {
	// A layout query on a type parameter is settled when the type is
	// instantiated, so it reports nothing here.
	src := `package server

comptime func Width<T>() -> usize => meta.size_of(T)
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestComptimeFunction(t *testing.T) {
	src := `package server

comptime func Square(n: i64) -> i64 => n * n

const Nine = Square(3)
const Sum = Square(3) + 1
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if got := out.Consts["Nine"]; got == nil || got.Int != 9 {
		t.Errorf("Nine = %v, want 9", got)
	}
	if got := out.Consts["Sum"]; got == nil || got.Int != 10 {
		t.Errorf("Sum = %v, want 10", got)
	}
}

func TestComptimeStatements(t *testing.T) {
	src := `package server

comptime func SumTo(n: i64) -> i64 {
	var total: i64 = 0
	for i in 0..n {
		total = total + i
	}
	return total
}

comptime func Count(items: Array<str>) -> i64 {
	var n: i64 = 0
	for item in items {
		if item == "" {
			continue
		}
		n = n + 1
	}
	return n
}

comptime func Name(kind: i32) -> str {
	match kind {
		0 => { return "zero" }
		_ => { return "other" }
	}
}

const Total = SumTo(5)
const NonEmpty = Count(["a", "", "b"])
const Zero = Name(0)
const Other = Name(7)
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if got := out.Consts["Total"]; got == nil || got.Int != 10 {
		t.Errorf("Total = %v, want 10", got)
	}
	if got := out.Consts["NonEmpty"]; got == nil || got.Int != 2 {
		t.Errorf("NonEmpty = %v, want 2", got)
	}
	if got := out.Consts["Zero"]; got == nil || got.Str != "zero" {
		t.Errorf("Zero = %v, want zero", got)
	}
	if got := out.Consts["Other"]; got == nil || got.Str != "other" {
		t.Errorf("Other = %v, want other", got)
	}
}

func TestComptimeRecursion(t *testing.T) {
	src := `package server

comptime func Factorial(n: i64) -> i64 {
	if n <= 1 {
		return 1
	}
	return n * Factorial(n - 1)
}

const Six = Factorial(3)
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if got := out.Consts["Six"]; got == nil || got.Int != 6 {
		t.Errorf("Six = %v, want 6", got)
	}
}

func TestComptimeWhileLoop(t *testing.T) {
	src := `package server

comptime func Halve(n: i64) -> i64 {
	var steps: i64 = 0
	for n > 1 {
		n = n / 2
		steps = steps + 1
	}
	return steps
}

const Steps = Halve(16)
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if got := out.Consts["Steps"]; got == nil || got.Int != 4 {
		t.Errorf("Steps = %v, want 4", got)
	}
}

func TestComptimeJSONField(t *testing.T) {
	// The example of the language reference: a compile-time function reads
	// a type body constant through meta.attr.
	src := `package server

struct User {
	name: str
	const json = Map { "name": "userName" }
}

comptime func JsonField(T: type, field: str) -> str {
	var m = meta.attr(T, "json")
	return m.Get(field).UnwrapOr(field)
}

const Name = JsonField(User, "name")
const Missing = JsonField(User, "age")
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if got := out.Consts["Name"]; got == nil || got.Str != "userName" {
		t.Errorf("Name = %v, want userName", got)
	}
	if got := out.Consts["Missing"]; got == nil || got.Str != "age" {
		t.Errorf("Missing = %v, want age", got)
	}
}

func TestComptimeLayoutThroughFunction(t *testing.T) {
	src := `package server

extern struct Cfg {
	port: u16
	backlog: i32
}

comptime func Offset(field: str) -> usize => meta.offset_of(Cfg, field)

const PortAt = Offset("port")
const BacklogAt = Offset("backlog")
`
	out, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	if got := out.Consts["PortAt"]; got == nil || got.Int != 0 {
		t.Errorf("PortAt = %v, want 0", got)
	}
	if got := out.Consts["BacklogAt"]; got == nil || got.Int != 4 {
		t.Errorf("BacklogAt = %v, want 4", got)
	}
}

func TestComptimeBlockInFunction(t *testing.T) {
	src := `package server

struct Cfg {
	n: i32
	const Size = meta.size_of(i32)
}

func f() -> i64 {
	comptime {
		var s = Cfg.Size
		if s > 0 {
			var doubled = s * 2
		}
	}
	return 0
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestComptimeCallAtRuntime(t *testing.T) {
	src := `package server

comptime func Width() -> i64 => 4

func f() -> i64 => Width()
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeComptimeOnly)
}

func TestComptimeFunctionValue(t *testing.T) {
	src := `package server

comptime func Width() -> i64 => 4

func f() -> i64 {
	var g = func Width
	return g()
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeComptimeOnly)
}

func TestComptimeLandingRule(t *testing.T) {
	// A constant whose type cannot live in static storage is only usable
	// inside a comptime context.
	src := `package server

func f() -> usize {
	const table = Map { "k": 1 }
	return table.Len()
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeComptimeOnly)
}

func TestComptimeLandingAllowed(t *testing.T) {
	src := `package server

const Greeting = "hello"

struct Cfg {
	const Size = meta.size_of(i32)
}

func f() -> i64 {
	const limit = 4
	comptime {
		var s = Cfg.Size
		if s > 0 {
			var doubled = limit * 2
		}
	}
	return limit
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestComptimePanic(t *testing.T) {
	src := `package server

comptime func Bad(n: i64) -> i64 {
	if n < 0 {
		panic("negative")
	}
	return n
}

const Outcome = Bad(-1)
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeComptimePanic)
}

func TestPanicIsNotAConversion(t *testing.T) {
	src := `package server

func f(n: i64) -> i64 {
	if n < 0 {
		panic("negative")
	}
	return n
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestEmptyExternUnion(t *testing.T) {
	src := `package server

extern union Nothing {
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeEmptyCLayout)
}

func TestBadConstraint(t *testing.T) {
	cases := map[string]string{
		"concrete type": `package server

struct Pair<T: i64> {
	first: T
}
`,
		"struct": `package server

struct Point {
	x: i32
}

func f<T: Point>(v: T) {
}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := checkSource(t, "server", nil, nil, src)
			wantCode(t, diags, diag.CodeNotInterface)
		})
	}
}

func TestGoodConstraints(t *testing.T) {
	src := `package server

struct Pair<T: Eq> {
	first: T
}

comptime func TypeName<T: type>() -> str => meta.name(T)

func Keep<T: Eq + Hash>(v: T) -> T => v
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestGenericStructLiteralInference(t *testing.T) {
	src := `package server

struct Pair<T> {
	first: T
	second: T
}

func Make() -> i64 {
	var p = Pair { first: 1, second: 2 }
	return p.first
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
}

func TestGenericStructLiteralUninferable(t *testing.T) {
	src := `package server

struct Holder<T, U> {
	only: T
}

func Bad() -> i64 {
	var h = Holder { only: 1 }
	return 0
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeTypeArity)
}

func TestRealisticDeclarations(t *testing.T) {
	rdep, cdep := checkedDep(t, "std/net", `package net

struct Conn {
	host: str
}
`)
	src := `package server

import "std/net"

const MaxConn: usize = 65536

type Port = u16

interface Handler {
	func Handle(conn: net.Conn) -> bool
}

struct Server {
	host: str
	port: Port
	conns: Array<net.Conn>
	func IsLocal() -> bool => self.host == "127.0.0.1"
	impl Show {
		func Show(w: Buf) {
			w.WriteStr("server")
		}
	}
	const Tag: str = "server/1.0"
}

enum State {
	Idle
	Running(i32)
	Stopped(str)
}

extern struct Cfg {
	port: u16
	backlog: i32
}

func Serve(s: Server, jobs: chan<i32>) -> Result<i32, PanicError> {
	return Result.Ok(0)
}
`
	_, diags := checkSource(t, "server",
		resolveDeps{"std/net": rdep}, checkDeps{"std/net": cdep}, src)
	wantClean(t, diags)
}

func TestCFastDeclarations(t *testing.T) {
	src := `package server

import c "test.h" {
	fast printf
}
fast extern func getpid() -> i32
extern func scanf(fmt: str, ...)

func Work() {
	c.printf(0)
	c.scanf("")
	getpid()
}
`
	pkg, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
	for _, name := range []string{"printf", "getpid"} {
		if !pkg.FastC[name] {
			t.Errorf("%s was declared fast, but it is not in the fast set", name)
		}
	}
	if pkg.FastC["scanf"] {
		t.Errorf("scanf was declared blocking, but it is in the fast set")
	}
}

func TestCFastOnADefinedFunction(t *testing.T) {
	src := `package server

fast func Work() {
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeUnexpectedToken)
}

func TestCFastNameNotInTheHeader(t *testing.T) {
	src := `package server

import c "test.h" {
	fast nosuch
}

func Work() {
	c.printf(0)
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantCode(t, diags, diag.CodeUndefinedName)
}
