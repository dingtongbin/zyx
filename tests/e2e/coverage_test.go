package e2e

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// coveredKinds lists the node kinds the corpus exercises. It grows as the
// corpus grows: a kind that stops being emitted fails the test below, so the
// intermediate form cannot quietly lose a construct.
var coveredKinds = []string{
	"alias", "arg", "arm", "array-lit", "array-repeat", "assign", "assign-op", "binary",
	"bind", "bind-const", "block", "body", "break", "c-call", "call", "callee",
	"callee-type", "chan", "cond", "const", "continue", "convert", "count",
	"default-arm", "defer", "dur", "elem", "else", "entry", "enum", "expr-stmt",
	"extern-struct", "extern-union", "field", "field-decl", "field-init", "for",
	"for-in", "func", "func-ref", "hi", "idx", "if", "impl", "import", "import-c",
	"index", "init", "interface", "is", "iter", "key", "lambda", "lhs", "lit", "lo",
	"map-lit", "match", "mcall", "method-value", "msg", "name", "new", "operand",
	"panic", "param", "part-expr", "part-text", "pat", "pat-bind", "pat-variant",
	"qual", "range", "receiver", "recv", "recv-arm", "region", "result",
	"return", "rhs",
	"send-arm",
	"select", "set-lit", "sig", "slice", "spawn", "struct", "struct-lit", "timeout-arm",
	"subject", "target", "then", "tindex", "try", "tuple-lit", "type",
	"type-param",
	"value", "var", "variant",
}

// deferredKinds lists the kinds the format defines that no corpus reaches
// yet, with the reason. Every entry is work the front end still owes, not a
// decision to drop the kind.
var deferredKinds = map[string]string{}

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
	var dirs []string
	for _, name := range programs(t) {
		dirs = append(dirs, filepath.Join("testdata", name))
	}
	for _, name := range corpus(t) {
		dirs = append(dirs, filepath.Join("corpus", name))
	}
	for _, dir := range dirs {
		for kind := range emitKinds(t, copyTree(t, dir)) {
			seen[kind] = true
		}
	}
	return seen
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
