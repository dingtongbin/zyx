package check

import (
	"testing"

	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/parser"
	"zyx/internal/resolve"
	"zyx/internal/source"
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

// checkSource resolves and checks one package.
func checkSource(t *testing.T, path string, rimports resolve.Imports, cimports Imports, src string) (*Package, []diag.Diagnostic) {
	t.Helper()
	pkg := &resolve.Package{Path: path, Files: []*ast.File{parse(t, src)}}
	info, rdiags := resolve.Resolve(pkg, rimports)
	if len(rdiags) != 0 {
		t.Fatalf("resolve diagnostics: %v", rdiags)
	}
	return Check(pkg, info, cimports)
}

// checkedDep resolves and checks a dependency package.
func checkedDep(t *testing.T, path, src string) (*resolve.Interface, *Interface) {
	t.Helper()
	pkg := &resolve.Package{Path: path, Files: []*ast.File{parse(t, src)}}
	info, rdiags := resolve.Resolve(pkg, nil)
	if len(rdiags) != 0 {
		t.Fatalf("resolve %s: %v", path, rdiags)
	}
	out, cdiags := Check(pkg, info, nil)
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

// wantClean fails when a package reports any diagnostic.
func wantClean(t *testing.T, diags []diag.Diagnostic) {
	t.Helper()
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
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
	src := `package server

const n = 4

struct Buffer {
	bytes: [u8; n]
}
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
		func Show() -> str => "server"
	}
}
`
	_, diags := checkSource(t, "server", nil, nil, src)
	wantClean(t, diags)
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
		func Show() -> str => "server"
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
