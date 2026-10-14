package resolve

import (
	"testing"

	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/parser"
	"zyx/internal/source"
	"zyx/internal/types"
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

// stubImports resolves a fixed set of package interfaces.
type stubImports map[string]*Interface

func (s stubImports) Lookup(path string) (*Interface, bool) {
	itf, ok := s[path]
	return itf, ok
}

// resolve parses one package and resolves it.
func resolve(t *testing.T, path string, imports Imports, src string) (*Info, []diag.Diagnostic) {
	t.Helper()
	return Resolve(&Package{Path: path, Files: []*ast.File{parse(t, src)}}, imports)
}

// symbolIDs lists every resolved symbol id.
func symbolIDs(info *Info) []string {
	out := make([]string, 0, len(info.Symbols))
	for _, s := range info.Symbols {
		out = append(out, s.ID)
	}
	return out
}

// hasSymbol reports whether a symbol with the given id was created.
func hasSymbol(info *Info, id string) bool {
	for _, s := range info.Symbols {
		if s.ID == id {
			return true
		}
	}
	return false
}

// codes lists the diagnostic codes in order.
func codes(diags []diag.Diagnostic) []diag.Code {
	out := make([]diag.Code, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}

// wantCode fails unless the diagnostics contain exactly one of the code.
func wantCode(t *testing.T, diags []diag.Diagnostic, code diag.Code) {
	t.Helper()
	for _, d := range diags {
		if d.Code == code {
			return
		}
	}
	t.Fatalf("want %s, got %v", code, codes(diags))
}

// netFixture resolves a small net package and returns its interface.
func netFixture(t *testing.T) *Interface {
	t.Helper()
	src := `package net

struct Conn {
	func Close() {
	}
}

func Listen(port: i32) -> Conn {
	return Conn { }
}

func listen() -> i32 => 0
`
	info, diags := Resolve(&Package{Path: "std/net", Files: []*ast.File{parse(t, src)}}, nil)
	if len(diags) != 0 {
		t.Fatalf("net fixture diagnostics: %v", codes(diags))
	}
	return info.Interface
}

func TestTopLevelSymbolIDs(t *testing.T) {
	src := `package server

const MaxConn = 1024
var counter = 0
type Port = u16

struct Conn {
	host: str
	func Close() {
	}
}

func Listen(addr: str) -> Conn {
	return Conn { host: addr }
}
`
	info, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	want := []string{
		"server.MaxConn",
		"server.counter",
		"server.Port",
		"server.Conn",
		"server.Conn.host",
		"server.Conn.Close",
		"server.Conn.Close#1",
		"server.Listen",
		"server.Listen#1",
	}
	for _, id := range want {
		if !hasSymbol(info, id) {
			t.Errorf("missing symbol %q in %v", id, symbolIDs(info))
		}
	}
}

func TestInterfaceDeclarations(t *testing.T) {
	src := `package server

const maxConn = 1
const MaxConn = 2
struct Conn {
}
func listen() {
}
`
	info, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if info.Interface.Name != "server" {
		t.Errorf("interface name = %q, want server", info.Interface.Name)
	}
	for name, want := range map[string]bool{
		"MaxConn": true,
		"Conn":    true,
		"maxConn": false,
		"listen":  false,
	} {
		sym := info.Interface.Decls[name]
		if sym == nil {
			t.Errorf("%s missing from the interface", name)
			continue
		}
		if sym.Public != want {
			t.Errorf("%s.Public = %v, want %v", name, sym.Public, want)
		}
	}
}

func TestTypeParameterAndSelf(t *testing.T) {
	src := `package server

struct Pair<T> {
	first: T
	second: T
	func First() -> T => self.first
}

interface Comparable {
	func Eq(other: Self) -> bool
}
`
	info, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	for _, id := range []string{"server.Pair$T", "server.Pair.first", "server.Pair.First", "server.Pair.First#1"} {
		if !hasSymbol(info, id) {
			t.Errorf("missing symbol %q in %v", id, symbolIDs(info))
		}
	}
}

func TestLocalAndParameterNumbering(t *testing.T) {
	src := `package server

func Work(a: i32, b: i32) -> i32 {
	var sum = a + b
	var f = func(x: i32) -> i32 => x + 1
	return sum + f(b)
}
`
	info, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	for _, id := range []string{"server.Work#1", "server.Work#2", "server.Work#3", "server.Work#4", "server.Work#4#1"} {
		if !hasSymbol(info, id) {
			t.Errorf("missing symbol %q in %v", id, symbolIDs(info))
		}
	}
}

func TestDuplicateDeclaration(t *testing.T) {
	src := `package server

const Value = 1
const Value = 2
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeDuplicateName)
}

func TestDuplicateTypeMember(t *testing.T) {
	src := `package server

struct S {
	x: i32
	func x() {
	}
}
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeDuplicateName)
}

func TestUndefinedName(t *testing.T) {
	src := `package server

func Work() -> i32 => missing
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeUndefinedName)
}

func TestUndefinedType(t *testing.T) {
	src := `package server

func Work() -> Missing => 0
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeUndefinedName)
}

func TestShadowingPackageDeclaration(t *testing.T) {
	src := `package server

const MaxConn = 1

func Work() {
	var MaxConn = 2
}
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeShadowing)
}

func TestShadowingPredeclaredName(t *testing.T) {
	src := `package server

func Work() {
	var str = 2
}
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeShadowing)
}

func TestPredeclaredRedefinition(t *testing.T) {
	src := `package server

const str = 1
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodePredeclaredName)
}

// TestPredeclaredUniverse guards the single source of truth for the
// predeclared names: the type names come from the types package, and the
// namespaces and global function are listed by resolution.
func TestPredeclaredUniverse(t *testing.T) {
	names := append(types.Names(), "meta", "dur", "panic")
	for _, name := range names {
		if name == "type" {
			continue // `type` is a keyword, so it never reaches resolution
		}
		src := "package server\n\nfunc f() {\n\tvar " + name + " = 1\n}\n"
		_, diags := resolve(t, "server", nil, src)
		if len(diags) == 0 {
			t.Errorf("%s: binding a predeclared name went unreported", name)
		}
	}
}

func TestPrivateMemberAcrossPackages(t *testing.T) {
	src := `package server

import "std/net"

func Work() -> i32 => net.listen()
`
	imports := stubImports{"std/net": netFixture(t)}
	_, diags := resolve(t, "server", imports, src)
	wantCode(t, diags, diag.CodePrivateName)
}

func TestQualifiedTypeAcrossPackages(t *testing.T) {
	src := `package server

import "std/net"

func Work(conn: net.Conn) {
	conn.Close()
}
`
	imports := stubImports{"std/net": netFixture(t)}
	info, diags := resolve(t, "server", imports, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if info.Interface.Name != "server" {
		t.Errorf("interface name = %q", info.Interface.Name)
	}
}

func TestUnusedImport(t *testing.T) {
	src := `package server

import "std/net"

func Work() {
}
`
	imports := stubImports{"std/net": netFixture(t)}
	_, diags := resolve(t, "server", imports, src)
	wantCode(t, diags, diag.CodeImportUnused)
}

func TestUnresolvedImport(t *testing.T) {
	src := `package server

import "std/net"

func Work() {
}
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeImportInvalid)
}

func TestDuplicateImportName(t *testing.T) {
	src := `package server

import "std/net"
import "other/net"

func Work() {
}
`
	imports := stubImports{
		"std/net":   netFixture(t),
		"other/net": &Interface{Path: "other/net", Name: "net", Decls: map[string]*Symbol{}},
	}
	_, diags := resolve(t, "server", imports, src)
	wantCode(t, diags, diag.CodeImportDuplicate)
}

func TestPackageNameMismatch(t *testing.T) {
	src := `package other

func Work() {
}
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodePackageName)
}

func TestReservedPackageName(t *testing.T) {
	src := `package meta

func Work() {
}
`
	_, diags := resolve(t, "meta", nil, src)
	wantCode(t, diags, diag.CodePackageName)
}

func TestUndefinedLabel(t *testing.T) {
	src := `package server

func Work() {
	for {
		break outer
	}
}
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeLabelUndefined)
}

func TestDuplicateLabel(t *testing.T) {
	src := `package server

func Work() {
outer: for {
	outer: for {
	}
}
}
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeLabelDuplicate)
}

func TestLabeledBreakResolves(t *testing.T) {
	src := `package server

func Work() {
outer: for i in 0..4 {
	for j in 0..4 {
		if i == j: continue outer
	}
}
}
`
	_, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
}

func TestLambdaCapture(t *testing.T) {
	src := `package server

func Work() {
	var x = 1
	var g = func() -> i32 => x
}
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeCapture)
}

func TestLambdaParametersAreFree(t *testing.T) {
	src := `package server

func Work() {
	var g = func(x: i32) -> i32 => x + 1
}
`
	_, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
}

func TestExportedPrivateType(t *testing.T) {
	src := `package server

type conn = i32

func Open() -> conn => 0
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeExportedPrivateType)
}

func TestPrivateTypeInPrivateSignature(t *testing.T) {
	src := `package server

type conn = i32

func open() -> conn => 0
`
	_, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
}

func TestStructLiteralFieldBinding(t *testing.T) {
	src := `package server

struct Point {
	x: i32
	y: i32
}

func Origin() -> Point => Point { x: 0, y: 0 }
`
	info, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if !hasSymbol(info, "server.Point.x") || !hasSymbol(info, "server.Point.y") {
		t.Errorf("fields missing: %v", symbolIDs(info))
	}
}

func TestStructLiteralUnknownField(t *testing.T) {
	src := `package server

struct Point {
	x: i32
}

func Origin() -> Point => Point { z: 0 }
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeUndefinedName)
}

func TestOrPatternBindingMismatch(t *testing.T) {
	src := `package server

func Work(r: Result<i32, i32>) {
	match r {
		Ok(v) | Err(e) => { }
	}
}
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeDuplicateName)
}

func TestOrPatternBindingMatch(t *testing.T) {
	src := `package server

func Work(r: Result<i32, i32>) {
	match r {
		Ok(v) | Err(v) => { }
	}
}
`
	_, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
}

func TestCNamespaceWithoutImport(t *testing.T) {
	src := `package server

func Work() {
	c.printf(0)
}
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeCNamespace)
}

func TestCNamespaceWithImport(t *testing.T) {
	src := `package server

import c "stdio.h"

func Work() {
	c.printf(0)
}
`
	info, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if !hasSymbol(info, "server.Work") {
		t.Errorf("Work missing: %v", symbolIDs(info))
	}
}

func TestMethodNamesReserved(t *testing.T) {
	cases := map[string]string{
		"main": `package server

struct S {
	func main() {
	}
}
`,
		"init in an enum": `package server

enum E {
	A
	func init() {
	}
}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := resolve(t, "server", nil, src)
			wantCode(t, diags, diag.CodeDuplicateName)
		})
	}
}

func TestInitInsideStruct(t *testing.T) {
	src := `package server

struct Point {
	x: i32
	func init(x: i32) -> Self {
		return Point { x: x }
	}
}
`
	_, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
}

func TestFStringInterpolationBinds(t *testing.T) {
	src := `package server

func Work(n: i32) -> str => f"count = {n}"
`
	_, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
}

func TestFStringInterpolationUndefined(t *testing.T) {
	src := `package server

func Work() -> str => f"count = {n}"
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeUndefinedName)
}

func TestMainCannotBeReferenced(t *testing.T) {
	src := `package main

func main() {
	main()
}
`
	_, diags := resolve(t, "main", nil, src)
	wantCode(t, diags, diag.CodeUndefinedName)
}

func TestSelfOutsideMethod(t *testing.T) {
	src := `package server

func Work() -> i32 => self.x
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeUndefinedName)
}

func TestSelectBindings(t *testing.T) {
	src := `package server

func use(v: i32) {
}

func Work(ch: chan<i32>) {
	select {
		recv(ch) -> v, ok {
			if ok {
				use(v)
			}
		}
		timeout(dur.ms(100)) {
			use(0)
		}
	}
}
`
	info, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if !hasSymbol(info, "server.Work#1") || !hasSymbol(info, "server.Work#2") {
		t.Errorf("select bindings missing: %v", symbolIDs(info))
	}
}

func TestMatchBindingsReachTheBody(t *testing.T) {
	src := `package server

func Work(r: Result<i32, str>) -> i32 {
	match r {
		Ok(v) => { return v }
		Err(msg) => { return 0 }
	}
}
`
	_, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
}

func TestMetaAndDurNamespaces(t *testing.T) {
	src := `package server

const Size = meta.size_of(i64)

func Sleep() {
	var d = dur.ms(100)
}
`
	_, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
}

func TestFunctionValueReference(t *testing.T) {
	src := `package server

func IsPositive(x: i64) -> bool => x > 0

func Work() {
	var g = func IsPositive
}
`
	_, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
}

func TestQualifiedFunctionValueReference(t *testing.T) {
	src := `package server

import "std/net"

func Work() {
	var g = func net.Listen
}
`
	imports := stubImports{"std/net": netFixture(t)}
	_, diags := resolve(t, "server", imports, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
}

func TestRealisticProgram(t *testing.T) {
	src := `package server

import "std/net"

const MaxConn = 65536

struct Server {
	host: str
	port: u16
	conns: Array<net.Conn>
	func IsLocal() -> bool => self.host == "127.0.0.1"
	impl Show {
		func Show() -> str {
			return f"{self.host}:{self.port}"
		}
	}
}

func Serve(s: Server, jobs: chan<i32>) -> Result<i32, PanicError> {
	region {
		spawn Handle(s, jobs)
	}
	for {
		var job, ok = jobs.Recv()
		if !ok {
			break
		}
		defer Drain(s)
		if job < 0 {
			continue
		}
		match Result.Ok(job) {
			Ok(v) => { Run(s, v) }
			Err(e) => { panic("bad job") }
		}
	}
	return Result.Ok(0)
}

func Handle(s: Server, jobs: chan<i32>) {
}

func Drain(s: Server) {
}

func Run(s: Server, job: i32) {
}

func Start() {
}
`
	info, diags := resolve(t, "server", stubImports{"std/net": netFixture(t)}, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	for _, id := range []string{
		"server.MaxConn",
		"server.Server",
		"server.Server.host",
		"server.Server.port",
		"server.Server.conns",
		"server.Server.IsLocal",
		"server.Server.Show",
		"server.Serve",
		"server.Handle",
		"server.Drain",
		"server.Run",
		"server.Start",
	} {
		if !hasSymbol(info, id) {
			t.Errorf("missing symbol %q", id)
		}
	}
}

func TestSpawnOutsideRegionIsNotAResolveConcern(t *testing.T) {
	// Where spawn may appear is a memory rule; resolution only binds names.
	src := `package server

func Work() {
	spawn Handle()
}

func Handle() {
}
`
	_, diags := resolve(t, "server", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
}

func TestUppercaseLocalName(t *testing.T) {
	cases := map[string]string{
		"binding": `package server

func Work() {
	var Foo = 1
}
`,
		"parameter": `package server

func Work(Foo: i32) {
}
`,
		"loop binding": `package server

func Work() {
	for Item in 0..4 {
	}
}
`,
		"select binding": `package server

func Work(ch: chan<i32>) {
	select {
		recv(ch) -> Value, ok {
		}
	}
}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := resolve(t, "server", nil, src)
			wantCode(t, diags, diag.CodeUppercaseLocal)
		})
	}
}

func TestMainOutsideCommandPackage(t *testing.T) {
	src := `package server

func main() {
}
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeMainPackage)
}

func TestMainInsideCommandPackage(t *testing.T) {
	src := `package main

func main() {
}
`
	_, diags := resolve(t, "main", nil, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
}

func TestRelativeImport(t *testing.T) {
	src := `package server

import "./other"

func Work() {
}
`
	_, diags := resolve(t, "server", nil, src)
	wantCode(t, diags, diag.CodeImportRelative)
}
