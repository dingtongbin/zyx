package e2e

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"zyx/internal/toolchain"
	"zyx/internal/zyxc"
)

// coveredKinds lists the node kinds the corpus exercises. It grows as the
// corpus grows: a kind that stops being emitted fails the test below, so the
// intermediate form cannot quietly lose a construct.
var coveredKinds = []string{
	"addr", "alias", "arg", "arm", "array-lit", "array-repeat", "assign", "assign-op",
	"binary",
	"c-alias", "c-const", "c-func", "c-global", "c-record", "c-variadic",
	"bind", "bind-const", "block", "body", "break", "c-call", "call", "callee", "callee-type", "chan", "cond", "const", "continue", "convert", "count",
	"default-arm", "defer", "dur", "elem", "else", "entry", "enum", "expr-stmt",
	"extern-struct", "extern-union", "field", "field-decl", "field-init", "for",
	"for-in", "func", "func-ref", "hi", "idx", "if", "impl", "import", "import-c",
	"index", "init", "interface", "is", "iter", "join", "key", "lambda", "lhs", "lit",
	"lo",
	"map-lit", "match", "mcall", "meta", "method-value", "msg", "name", "new",
	"operand",
	"panic", "param", "part-expr", "part-text", "pat", "pat-bind", "pat-lit",
	"pat-or", "pat-range", "pat-tuple", "pat-variant", "pat-wild",
	"qual", "range", "receiver", "recv", "recv-arm", "region", "result",
	"return", "rhs",
	"send-arm",
	"select", "set-lit", "sig", "slice", "spawn", "struct", "struct-lit", "timeout-arm",
	"subject", "target", "then", "tindex", "try", "tuple-lit", "type",
	"type-param", "unary",
	"value", "var", "variant", "variant-lit", "watcher",
}

// specKinds lists every node kind the format defines, mirroring the kind
// tables of the toolchain document and the notes beside them. The document is
// the source of truth: a kind added there belongs here too, and the coverage
// test below is what makes the two agree.
var specKinds = []string{
	"addr", "alias", "arm", "array-lit", "array-repeat", "assign", "assign-op",
	"binary",
	"c-alias", "c-const", "c-func", "c-global", "c-record", "c-variadic", "bind", "bind-const", "block", "break", "call", "c-call", "const",
	"continue", "convert", "default-arm", "defer", "dur", "enum", "expr-stmt",
	"extern-struct", "extern-union", "field", "field-decl", "for", "for-in", "func",
	"func-ref", "if", "impl", "import", "import-c", "index", "interface", "is",
	"join", "lambda", "lit", "map-lit", "match", "mcall", "meta", "method-value",
	"name", "new", "panic", "pat-bind", "pat-lit", "pat-or", "pat-range",
	"pat-tuple", "pat-variant", "pat-wild", "qual", "range", "receiver",
	"recv-arm", "region", "return", "select", "send-arm", "set-lit", "sig",
	"slice", "spawn", "struct", "struct-lit", "timeout-arm", "tindex", "try",
	"type-param", "unary", "var", "variant", "variant-lit",
}

// deferredKinds lists the kinds the format defines that no corpus reaches
// yet, with the reason. Every entry is work the front end still owes, not a
// decision to drop the kind.
var deferredKinds = map[string]string{}

// specMarks lists every mark the format defines, mirroring the mark table of
// the toolchain document. As with specKinds, the document is the source of
// truth.
var specMarks = []string{
	"alias", "boundary-clone", "boundary-share", "c-blocking", "c-fast",
	"chan:close", "chan:recv", "chan:select", "chan:send", "chan:try",
	"coro:body", "coro:handoff", "coro:host", "coro:panic-path", "coro:region",
	"coro:spawn", "defer-site", "defers", "entry", "home", "init", "iter-guard",
	"last-use", "mono-rule", "move", "region:enter", "region:exit", "shared-bind",
	"shared-ref", "shared-store", "static-release", "transfer", "view-borrow",
	"watched",
}

// coveredMarks lists the marks the corpus emits. It grows as the corpus
// grows: a mark that stops being emitted fails the test below.
var coveredMarks = []string{
	"alias", "boundary-clone", "boundary-share", "c-blocking", "chan:select",
	"coro:handoff", "coro:host", "coro:panic-path", "coro:region", "coro:spawn",
	"defer-site", "defers", "entry", "home", "init", "iter-guard", "last-use",
	"move", "region:enter", "region:exit", "shared-bind", "shared-ref",
	"shared-store", "static-release", "transfer", "view-borrow", "watched",
}

// deferredMarks lists the marks the format defines that no corpus reaches
// yet, with the reason. Every entry is work the front end still owes, not a
// decision to drop the mark.
var deferredMarks = map[string]string{
	"c-fast":     unexercised,
	"chan:close": unexercised,
	"chan:recv":  unexercised,
	"chan:send":  unexercised,
	"chan:try":   notEmitted,
	"coro:body":  notEmitted,
	"mono-rule":  notEmitted,
}

// unexercised is the reason a mark the front end already produces is deferred:
// no program of the corpus has its shape yet, so nothing verifies it.
const unexercised = "no corpus program has this shape yet"

// notEmitted is the reason a mark the format defines is deferred: the pass
// that would produce it does not run yet.
const notEmitted = "the pass that produces it does not run yet"

// emitMarks returns every mark the intermediate form of a package writes,
// read back through the format's own reader. Reading the text rather than
// scanning it also checks that what the writer wrote is what the reader reads.
func emitMarks(t *testing.T, dir string) map[string]bool {
	t.Helper()
	stdout, stderr, code := run(dir, "emit-zyxc", ".")
	if code != 0 {
		t.Fatalf("emit-zyxc failed with %d:\n%s", code, stderr)
	}
	file, err := zyxc.Parse([]byte(stdout))
	if err != nil {
		t.Fatalf("the emitted form does not parse: %v", err)
	}
	marks := map[string]bool{}
	var walk func(*zyxc.Node)
	walk = func(n *zyxc.Node) {
		for _, m := range n.Marks {
			marks[m.Name] = true
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	for _, decl := range file.Decls {
		walk(decl)
	}
	return marks
}

// corpusDirs returns every program the corpus check reads, over both the
// programs that run and the ones that are only checked.
func corpusDirs(t *testing.T) []string {
	t.Helper()
	var dirs []string
	for _, name := range programs(t) {
		dirs = append(dirs, filepath.Join("testdata", name))
	}
	for _, name := range corpus(t) {
		dirs = append(dirs, filepath.Join("corpus", name))
	}
	return dirs
}

// kindPattern matches the kind that opens a line of the tree.
var kindPattern = regexp.MustCompile(`(?m)^\s*([a-z][a-z0-9-]*)`)

// emitKinds returns every node kind the intermediate form of a package uses.
// The four header lines are not nodes, so the scan starts after them.
func emitKinds(t *testing.T, dir string) map[string]bool {
	t.Helper()
	stdout, stderr, code := run(dir, "emit-zyxc", ".")
	if code != 0 {
		t.Fatalf("emit-zyxc failed with %d:\n%s", code, stderr)
	}
	for i := 0; i < 4; i++ {
		if _, rest, ok := strings.Cut(stdout, "\n"); ok {
			stdout = rest
		}
	}
	kinds := map[string]bool{}
	for _, match := range kindPattern.FindAllStringSubmatch(stdout, -1) {
		kinds[match[1]] = true
	}
	return kinds
}

// corpusKinds returns every kind the corpus reaches, over both the programs
// that run and the ones that are only checked.
func corpusKinds(t *testing.T) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	for _, dir := range corpusDirs(t) {
		for kind := range emitKinds(t, copyTree(t, dir)) {
			seen[kind] = true
		}
	}
	return seen
}

// corpusMarks returns every mark the corpus reaches.
func corpusMarks(t *testing.T) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	for _, dir := range corpusDirs(t) {
		for mark := range emitMarks(t, copyTree(t, dir)) {
			seen[mark] = true
		}
	}
	return seen
}

// TestCorpusCoversTheMarks checks that every mark the corpus is known to
// exercise still reaches the intermediate form, and that no mark appears that
// the list does not name.
func TestCorpusCoversTheMarks(t *testing.T) {
	seen := corpusMarks(t)
	var missing []string
	for _, mark := range coveredMarks {
		if !seen[mark] {
			missing = append(missing, mark)
		}
	}
	if len(missing) != 0 {
		t.Errorf("the corpus no longer emits these marks: %s", strings.Join(missing, " "))
	}
	var extra []string
	for mark := range seen {
		if !contains(coveredMarks, mark) {
			extra = append(extra, mark)
		}
	}
	sort.Strings(extra)
	if len(extra) != 0 {
		t.Errorf("the corpus emits marks the list does not name: %s", strings.Join(extra, " "))
	}
}

// TestEverySpecMarkIsEmittedOrDeferred checks that the corpus emits every
// mark the format defines, or that the mark is recorded in deferredMarks with
// a reason. A mark nothing emits is a fact nothing records.
func TestEverySpecMarkIsEmittedOrDeferred(t *testing.T) {
	emitted := map[string]bool{}
	for _, mark := range coveredMarks {
		emitted[mark] = true
	}
	var unrecorded []string
	for _, mark := range specMarks {
		if emitted[mark] {
			continue
		}
		if reason, ok := deferredMarks[mark]; ok && reason != "" {
			continue
		}
		unrecorded = append(unrecorded, mark)
	}
	if len(unrecorded) != 0 {
		t.Errorf("no corpus emits these marks and none is deferred: %s",
			strings.Join(unrecorded, " "))
	}
	for mark := range deferredMarks {
		if !contains(specMarks, mark) {
			t.Errorf("deferredMarks names %q, which the format does not define", mark)
		}
	}
}

// TestCorpusCoversTheIntermediateForm checks that every kind the corpus is
// known to exercise still reaches the intermediate form. A construct that
// stops being emitted is a hole in the compiler, and this is where it shows
// up rather than in the backend.
func TestCorpusCoversTheIntermediateForm(t *testing.T) {
	seen := corpusKinds(t)
	var missing []string
	for _, kind := range coveredKinds {
		if !seen[kind] {
			missing = append(missing, kind)
		}
	}
	if len(missing) != 0 {
		t.Errorf("the corpus no longer reaches these kinds: %s", strings.Join(missing, " "))
	}
	var extra []string
	for kind := range seen {
		if !contains(coveredKinds, kind) {
			extra = append(extra, kind)
		}
	}
	sort.Strings(extra)
	if len(extra) != 0 {
		t.Errorf("the corpus reaches kinds the list does not name: %s", strings.Join(extra, " "))
	}
}

// TestEverySpecKindIsExercisedOrDeferred checks that the corpus reaches every
// kind the format defines. A kind no corpus reaches is a construct nothing
// verifies, and the only way past this test is to reach it or to record in
// deferredKinds why not.
func TestEverySpecKindIsExercisedOrDeferred(t *testing.T) {
	covered := map[string]bool{}
	for _, kind := range coveredKinds {
		covered[kind] = true
	}
	var unexercised []string
	for _, kind := range specKinds {
		if covered[kind] {
			continue
		}
		if reason, ok := deferredKinds[kind]; ok && reason != "" {
			continue
		}
		unexercised = append(unexercised, kind)
	}
	if len(unexercised) != 0 {
		t.Errorf("no corpus reaches these kinds and none is deferred: %s",
			strings.Join(unexercised, " "))
	}
}

// TestStaticReleaseReachesTheBackend checks that an object the analysis marks
// as no longer used is released through the runtime. The value the program
// computes proves the release does not happen before the use it follows,
// which is what a wrong placement would break.
func TestStaticReleaseReachesTheBackend(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	dir := copyTree(t, filepath.Join("testdata", "release"))
	stdout, stderr, code := run(dir, "emit-llvm", ".")
	if code != 0 {
		t.Fatalf("emit-llvm failed with %d:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "call void @zyxrt_region_release") {
		t.Errorf("no object is released:\n%s", stdout)
	}
	if !strings.Contains(stdout, "zyxrt_region_current") {
		t.Errorf("the release does not name a region:\n%s", stdout)
	}
}

// TestBoundaryCopyReachesTheBackend checks that an object crossing into a
// coroutine is copied, and that the copy is what the coroutine reads: the
// argument block holds the reference the callee receives, not the object
// itself, which the program's value confirms.
func TestBoundaryCopyReachesTheBackend(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	dir := copyTree(t, filepath.Join("testdata", "boundary"))
	stdout, stderr, code := run(dir, "emit-llvm", ".")
	if code != 0 {
		t.Fatalf("emit-llvm failed with %d:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "llvm.memcpy") {
		t.Errorf("the boundary copy is missing:\n%s", stdout)
	}
}

// TestGenericCallsCarryTheirInstantiatedSignature checks that a call to a
// generic function annotates its callee with the signature the call
// instantiates. The monomorphizer settles the type arguments by matching that
// signature against the declaration's own, so an annotation left generic
// would send it looking for the arguments somewhere else.
func TestGenericCallsCarryTheirInstantiatedSignature(t *testing.T) {
	dir := copyTree(t, filepath.Join("testdata", "generics"))
	stdout, stderr, code := run(dir, "emit-zyxc", ".")
	if code != 0 {
		t.Fatalf("emit-zyxc failed with %d:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, `type "func(i64, i64, bool) -> i64"`) {
		t.Errorf("the call to pick carries no instantiated signature:\n%s", stdout)
	}
	if strings.Contains(stdout, "func(T, T, bool) -> T") {
		t.Errorf("the call to pick still carries the generic signature:\n%s", stdout)
	}
}

// TestInterpolationWritesIntoABuffer checks that interpolating a declared type
// hands the gather buffer to the value's Show rather than asking it for a
// string and appending that. A Show that answered with a string would still
// produce the same output, so the test reads the call the backend emits: the
// argument after the receiver has to be the register the buffer came from.
func TestInterpolationWritesIntoABuffer(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	dir := copyTree(t, filepath.Join("testdata", "interpolation"))
	stdout, stderr, code := run(dir, "emit-llvm", ".")
	if code != 0 {
		t.Fatalf("emit-llvm failed with %d:\n%s", code, stderr)
	}
	if regexp.MustCompile(`call \{ ptr, i64 \} @"[^"]*\.Show"`).MatchString(stdout) {
		t.Errorf("Show still answers with a string:\n%s", stdout)
	}
	// A value the fixed table writes goes straight into the buffer, so no
	// formatter that hands back a string of its own remains.
	if regexp.MustCompile(`@zyxrt_(i64|u64|f64|bool|char)_str`).MatchString(stdout) {
		t.Errorf("a formatted value still becomes a string of its own:\n%s", stdout)
	}
	if !strings.Contains(stdout, "zyxrt_buf_write_") {
		t.Errorf("no value is written into the buffer:\n%s", stdout)
	}
	buffers := regexp.MustCompile(`(%[\w.]+) = call ptr @zyxrt_buf_new`).FindAllStringSubmatch(stdout, -1)
	if len(buffers) == 0 {
		t.Fatalf("no interpolation gathers into a buffer:\n%s", stdout)
	}
	for _, buffer := range buffers {
		call := regexp.MustCompile(`call void @"[^"]*\.Show"\(ptr %[\w.]+, ptr ` + regexp.QuoteMeta(buffer[1]) + `\)`)
		if call.MatchString(stdout) {
			return
		}
	}
	t.Errorf("no Show receives the gather buffer:\n%s", stdout)
}

// deferredRunning lists the kinds the format defines that no program which
// runs reaches, with the reason. A kind only a checked corpus directory
// reaches is a construct no machine code carries yet, so only the checker
// verifies it.
var deferredRunning = map[string]string{
	// A construct that exists only while the program is compiled, so no
	// machine code carries it by nature.
	"alias":      compileOnly,
	"bind-const": compileOnly,
	"meta":       compileOnly,
	// A construct that produces machine code and that no program of the
	// corpus uses yet.
	"addr":          unexercised,
	"extern-struct": unexercised,
	"extern-union":  unexercised,
}

// inImported is the reason a kind is deferred from this measurement: its only
// use is inside a package the program imports.
const inImported = "its only use is in an imported package, which the entry package's tree does not show"

// compileOnly is the reason a kind is deferred: the construct is settled while
// the program is compiled and leaves no machine code behind.
const compileOnly = "the construct is settled at compile time and leaves no machine code"

// programKinds returns every kind the programs that run reach. A program is a
// directory of testdata, which the end-to-end test compiles to machine code
// and executes.
func programKinds(t *testing.T) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	for _, name := range programs(t) {
		for kind := range emitKinds(t, copyTree(t, filepath.Join("testdata", name))) {
			seen[kind] = true
		}
	}
	return seen
}

// TestEverySpecKindReachesMachineCode checks that a program which runs reaches
// every kind the format defines. A kind only a checked corpus directory
// reaches is a construct no machine code carries yet, and this is where that
// shows up rather than in a claim about coverage. The count it reports is the
// answer to how much of the syntax the corpus carries into machine code.
func TestEverySpecKindReachesMachineCode(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	seen := programKinds(t)
	var unbuilt []string
	for _, kind := range specKinds {
		if seen[kind] {
			continue
		}
		if reason, ok := deferredRunning[kind]; ok && reason != "" {
			continue
		}
		unbuilt = append(unbuilt, kind)
	}
	if len(unbuilt) != 0 {
		t.Errorf("no program that runs reaches these kinds and none is deferred: %s",
			strings.Join(unbuilt, " "))
	}
	// A kind that a running program now reaches belongs in the covered set,
	// so a stale deferral is a fact the record gets wrong.
	var reached []string
	for kind := range deferredRunning {
		if seen[kind] {
			reached = append(reached, kind)
		}
	}
	sort.Strings(reached)
	if len(reached) != 0 {
		t.Errorf("a program that runs reaches these kinds, so they are no longer deferred: %s",
			strings.Join(reached, " "))
	}
	t.Logf("a running program reaches %d of the %d kinds the format defines; the rest are deferred with a reason",
		len(specKinds)-len(deferredRunning), len(specKinds))
}

// TestDeferredKindsAreStillUnreached checks the record of what the front end
// still owes a corpus for. When a construct joins the corpus, its entry must
// move to coveredKinds, and this test says so.
func TestDeferredKindsAreStillUnreached(t *testing.T) {
	seen := corpusKinds(t)
	var reached []string
	for kind := range deferredKinds {
		if seen[kind] {
			reached = append(reached, kind)
		}
	}
	sort.Strings(reached)
	if len(reached) != 0 {
		t.Errorf("these kinds now have a corpus and belong on the covered list: %s",
			strings.Join(reached, " "))
	}
}

// contains reports whether a list holds a string.
func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
