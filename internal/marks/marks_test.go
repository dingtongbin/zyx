package marks

import (
	"sort"
	"strings"
	"testing"

	"zyx/internal/ast"
	"zyx/internal/cheader"
	"zyx/internal/check"
	"zyx/internal/coro"
	"zyx/internal/diag"
	"zyx/internal/parser"
	"zyx/internal/resolve"
	"zyx/internal/source"
	"zyx/internal/toolchain"
)

// marksOf runs the front end on one package and returns its mark model.
func marksOf(t *testing.T, src string) (*Info, []diag.Diagnostic) {
	t.Helper()
	file, pdiags := parser.Parse(source.NewFile("test.zyx", []byte(src)))
	if len(pdiags) != 0 {
		t.Fatalf("parse: %v", pdiags)
	}
	pkg := &resolve.Package{Path: "server", Files: []*ast.File{file}}
	rinfo, rdiags := resolve.Resolve(pkg, nil)
	if len(rdiags) != 0 {
		t.Fatalf("resolve: %v", rdiags)
	}
	cpkg, cdiags := check.Check(pkg, rinfo, nil, toolchain.HostTarget(), map[string]*cheader.Table{"test.h": {Functions: map[string]*cheader.Func{
		"printf": {Name: "printf", Variadic: true},
	}}})
	if len(cdiags) != 0 {
		t.Fatalf("check: %v", cdiags)
	}
	return Build(pkg, rinfo, cpkg, coro.Build(pkg))
}

// allMarks renders every mark of the model, sorted, for assertions.
func allMarks(info *Info) []string {
	var out []string
	for _, marks := range info.Marks {
		for _, m := range marks {
			out = append(out, m.String())
		}
	}
	sort.Strings(out)
	return out
}

// hasMark reports whether any node carries a mark with that spelling.
func hasMark(info *Info, want string) bool {
	for _, s := range allMarks(info) {
		if s == want {
			return true
		}
	}
	return false
}

// wantMark fails when no node carries the mark.
func wantMark(t *testing.T, info *Info, want string) {
	t.Helper()
	if !hasMark(info, want) {
		t.Errorf("no %q mark in %v", want, allMarks(info))
	}
}

// wantClean fails when the pass reports a diagnostic.
func wantClean(t *testing.T, diags []diag.Diagnostic) {
	t.Helper()
	if len(diags) != 0 {
		var out []string
		for _, d := range diags {
			out = append(out, string(d.Code)+": "+d.Message)
		}
		t.Fatalf("unexpected diagnostics: %v", out)
	}
}

// hasCode reports whether the pass reported a diagnostic with that code.
func hasCode(diags []diag.Diagnostic, code diag.Code) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestRegionMarks(t *testing.T) {
	src := `package server

func Work() {
	region {
		var x = new Array<i64>(4)
	}
}
`
	info, diags := marksOf(t, src)
	wantClean(t, diags)
	wantMark(t, info, "region:enter:r1")
	wantMark(t, info, "region:exit:r1")
	wantMark(t, info, "home:r1")
}

func TestSpawnMarks(t *testing.T) {
	src := `package server

func Work() {
	region {
		spawn Handle()
	}
}

func Handle() {
}
`
	info, diags := marksOf(t, src)
	wantClean(t, diags)
	wantMark(t, info, "coro:spawn")
	wantMark(t, info, "coro:region:c0")
	wantMark(t, info, "coro:host")
}

func TestBoundaryMarks(t *testing.T) {
	src := `package server

func Work(ch: chan<i32>, n: i64) {
	region {
		spawn Handle(ch, n)
	}
}

func Handle(ch: chan<i32>, n: i64) {
}
`
	info, diags := marksOf(t, src)
	wantClean(t, diags)
	// A channel is SyncSafe and crosses as it is; a value type is copied.
	wantMark(t, info, "boundary-share")
	wantMark(t, info, "boundary-clone")
	wantMark(t, info, "coro:handoff")
}

func TestIterationGuardAndDefer(t *testing.T) {
	src := `package server

func Work(xs: Array<i64>) -> i64 {
	defer Cleanup()
	var total: i64 = 0
	for x in xs {
		total = total + x
	}
	return total
}

func Cleanup() {
}
`
	info, diags := marksOf(t, src)
	wantClean(t, diags)
	wantMark(t, info, "iter-guard")
	wantMark(t, info, "defer-site")
}

func TestChannelMarks(t *testing.T) {
	src := `package server

func Work(ch: chan<i32>) -> i32 {
	ch.Send(1)
	var v, ok = ch.Recv()
	if !ok {
		ch.Close()
	}
	return v
}
`
	info, diags := marksOf(t, src)
	wantClean(t, diags)
	wantMark(t, info, "chan:send")
	wantMark(t, info, "chan:recv")
	wantMark(t, info, "chan:close")
}

func TestCMarks(t *testing.T) {
	src := `package server

import c "test.h" {
	fast printf
}
extern func strlen(s: str) -> usize
fast extern func puts(s: str) -> i32

func Work() {
	c.printf(0)
	puts("")
	strlen("")
}
`
	info, diags := marksOf(t, src)
	wantClean(t, diags)
	wantMark(t, info, "c-blocking")
	wantMark(t, info, "c-fast")
}

func TestStaticRelease(t *testing.T) {
	src := `package server

func Work() -> usize {
	var xs = new Array<i64>(4)
	xs.Push(1)
	return xs.Len()
}
`
	info, diags := marksOf(t, src)
	wantClean(t, diags)
	wantMark(t, info, "home:r0")
	wantMark(t, info, "last-use")
	wantMark(t, info, "static-release")
}

func TestTransferOnReturn(t *testing.T) {
	src := `package server

func Make() -> Array<i64> {
	return new Array<i64>(4)
}
`
	info, diags := marksOf(t, src)
	wantClean(t, diags)
	wantMark(t, info, "transfer:caller")
	wantMark(t, info, "alias")
}

func TestMoveToCoroutine(t *testing.T) {
	src := `package server

func Work() {
	region {
		spawn Handle(new Array<i64>(4))
	}
}

func Handle(xs: Array<i64>) {
}
`
	info, diags := marksOf(t, src)
	wantClean(t, diags)
	wantMark(t, info, "move:c0")
}

func TestSharedStore(t *testing.T) {
	src := `package server

struct Holder {
	xs: Array<i64>
}

func Work(h: Holder) {
	var xs = new Array<i64>(4)
	h.xs = xs
}
`
	info, diags := marksOf(t, src)
	wantClean(t, diags)
	wantMark(t, info, "shared-ref")
	// An object stored where it outlives its binding binds to the region
	// that contains every use, which here is the function body.
	wantMark(t, info, "shared-bind:r0")
}

func TestViewStoredIsRejected(t *testing.T) {
	src := `package server

struct Holder {
	buf: [u8]
}

func Work(h: Holder, xs: Array<u8>) {
	h.buf = xs[0..2]
}
`
	_, diags := marksOf(t, src)
	found := false
	for _, d := range diags {
		if d.Code == diag.CodeViewLifetime {
			found = true
		}
	}
	if !found {
		t.Errorf("want a view lifetime diagnostic, got %v", diags)
	}
}

func TestMarksAreSorted(t *testing.T) {
	src := `package server

func Work() {
	region {
		spawn Handle()
	}
}

func Handle() {
}
`
	info, _ := marksOf(t, src)
	for node, marks := range info.Marks {
		for i := 1; i < len(marks); i++ {
			prev := marks[i-1].String()
			cur := marks[i].String()
			if prev > cur {
				t.Errorf("marks of %T are not sorted: %v", node, marks)
				break
			}
		}
	}
}

func TestRegionsAreNamedInOrder(t *testing.T) {
	src := `package server

func Work() {
	region {
		var a = new Array<i64>(1)
	}
	region {
		var b = new Array<i64>(1)
	}
}
`
	info, diags := marksOf(t, src)
	wantClean(t, diags)
	wantMark(t, info, "home:r1")
	wantMark(t, info, "home:r2")
}

func TestViewBorrowMark(t *testing.T) {
	src := `package server

func Work(xs: Array<u8>) -> u8 {
	var view = xs[0..2]
	return view[0]
}
`
	info, diags := marksOf(t, src)
	wantClean(t, diags)
	wantMark(t, info, "view-borrow")
}

// TestMoveOutOfRegionMovesTheHome checks that an object created inside a
// region and used after it leaves is created in the region that holds it. A
// region releases everything it holds when it exits, so an object left where
// it was made would be read after it returned to the pool.
func TestMoveOutOfRegionMovesTheHome(t *testing.T) {
	src := `package server

struct Box {
	v: i64
}

func Work() -> i64 {
	var held = Box { v: 1 }
	region {
		held = Box { v: 41 }
	}
	return held.v
}
`
	info, diags := marksOf(t, src)
	wantClean(t, diags)
	wantMark(t, info, "home:r0")
	wantMark(t, info, "move:r0")
	wantMark(t, info, "why-last-use")
	if hasMark(info, "home:r1") {
		t.Errorf("an object used outside the region is still created in it: %v", allMarks(info))
	}
}

// TestStoreIntoAnOuterContainerBindsOuter checks that a value stored into a
// container that outlives the region belongs to the container's region: the
// store hands the value to a destination the region does not own.
func TestStoreIntoAnOuterContainerBindsOuter(t *testing.T) {
	src := `package server

struct Holder {
	xs: Array<i64>
}

func Work(h: Holder) {
	region {
		var xs = new Array<i64>(4)
		h.xs = xs
	}
}
`
	info, diags := marksOf(t, src)
	wantClean(t, diags)
	wantMark(t, info, "shared-bind:r0")
	if hasMark(info, "shared-bind:r1") {
		t.Errorf("a value stored into an outer container binds to the inner region: %v", allMarks(info))
	}
}

// TestViewModifiedIsRejected checks that a container is not structurally
// modified while a view of its buffer is live: the view names the buffer, and
// the modification may hand the container a different one.
func TestViewModifiedIsRejected(t *testing.T) {
	src := `package server

func Work(xs: Array<u8>) -> u8 {
	var view = xs[0..2]
	xs.Push(9)
	return view[0]
}
`
	_, diags := marksOf(t, src)
	if !hasCode(diags, diag.CodeBorrowModified) {
		t.Errorf("want a borrow modification diagnostic, got %v", diags)
	}
}

// TestLoopModificationIsRejected checks that a loop over a container holds a
// borrow of it for as long as the body runs.
func TestLoopModificationIsRejected(t *testing.T) {
	src := `package server

func Work(xs: Array<u8>) {
	for x in xs {
		xs.Push(x)
	}
}
`
	_, diags := marksOf(t, src)
	if !hasCode(diags, diag.CodeBorrowModified) {
		t.Errorf("want a borrow modification diagnostic, got %v", diags)
	}
}

// TestLoopEndsTheBorrow checks that the borrow a loop holds ends with the
// loop: a modification after it is what the language allows.
func TestLoopEndsTheBorrow(t *testing.T) {
	src := `package server

func Work(xs: Array<u8>) {
	for x in xs {
		if x == 0 {
			break
		}
	}
	xs.Push(1)
}
`
	_, diags := marksOf(t, src)
	wantClean(t, diags)
}

func TestMarkString(t *testing.T) {
	if got := (Mark{Name: "home", Arg: "r1"}).String(); got != "home:r1" {
		t.Errorf("String() = %q, want home:r1", got)
	}
	if got := (Mark{Name: "alias"}).String(); got != "alias" {
		t.Errorf("String() = %q, want alias", got)
	}
	if !strings.Contains(strings.Join(allMarks(&Info{Marks: map[ast.Node][]Mark{}}), " "), "") {
		t.Error("an empty model must render nothing")
	}
}
