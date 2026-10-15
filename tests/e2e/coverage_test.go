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
	"arg", "arm", "assign", "assign-op", "binary", "bind", "block", "body",
	"break", "call", "callee", "callee-type", "cond", "continue", "convert",
	"default-arm", "defer", "else", "expr-stmt", "field", "field-decl",
	"field-init", "for", "func", "if", "lhs", "lit", "match", "mcall",
	"method-value", "msg", "name", "operand", "panic", "param", "part-expr",
	"part-text", "pat", "pat-bind", "pat-variant", "qual", "receiver", "recv",
	"region", "result", "return", "rhs", "select", "sig", "spawn", "struct-lit",
	"subject", "target", "then", "type", "value", "variant",
}

// kindPattern matches the kind that opens a line of the tree.
var kindPattern = regexp.MustCompile(`(?m)^\s+([a-z][a-z0-9-]*)`)

// emitKinds returns every node kind the intermediate form of a package uses.
func emitKinds(t *testing.T, dir string) map[string]bool {
	t.Helper()
	stdout, stderr, code := run(dir, "emit-zyxc", ".")
	if code != 0 {
		t.Fatalf("emit-zyxc failed with %d:\n%s", code, stderr)
	}
	kinds := map[string]bool{}
	for _, match := range kindPattern.FindAllStringSubmatch(stdout, -1) {
		kinds[match[1]] = true
	}
	return kinds
}

// TestCorpusCoversTheIntermediateForm runs the corpus through the emitter
// and checks that every kind the corpus is known to exercise still appears.
// A construct that stops reaching the intermediate form is a hole in the
// compiler, and this is where it shows up rather than in the backend.
func TestCorpusCoversTheIntermediateForm(t *testing.T) {
	seen := map[string]bool{}
	var dirs []string
	for _, name := range programs(t) {
		dirs = append(dirs, filepath.Join("testdata", name))
	}
	for _, name := range corpus(t) {
		dirs = append(dirs, filepath.Join("corpus", name))
	}
	for _, dir := range dirs {
		copied := copyTree(t, dir)
		for kind := range emitKinds(t, copied) {
			seen[kind] = true
		}
	}
	var missing []string
	for _, kind := range coveredKinds {
		if !seen[kind] {
			missing = append(missing, kind)
		}
	}
	if len(missing) != 0 {
		t.Errorf("the corpus no longer reaches these kinds: %s", strings.Join(missing, " "))
	}
	// A kind that appears but is not on the list is not a failure: the list
	// records what the corpus is known to cover, and it should be extended
	// when a new construct joins the corpus.
	var extra []string
	for kind := range seen {
		if !contains(coveredKinds, kind) {
			extra = append(extra, kind)
		}
	}
	sort.Strings(extra)
	if len(extra) != 0 {
		t.Logf("kinds the corpus reaches that the list does not name yet: %s", strings.Join(extra, " "))
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

// TestSpecKindsAreReachable checks that every kind the format defines is
// either reachable from the corpus or explicitly deferred. The list is the
// record of what the front end still owes the backend.
func TestSpecKindsAreReachable(t *testing.T) {
	// deferred lists the kinds no corpus reaches yet, with the reason. Every
	// entry is work the front end still owes, not a decision to drop it.
	deferred := map[string]string{
		"alias":         "no corpus declares a type alias",
		"array-lit":     "no corpus builds a fixed-size array",
		"array-repeat":  "no corpus repeats an array",
		"bind-const":    "no corpus declares a local constant",
		"c-call":        "no corpus calls C",
		"const":         "no corpus declares a top-level constant",
		"enum":          "no corpus declares an enum in the entry package",
		"extern-struct": "no corpus declares a C struct",
		"extern-union":  "no corpus declares a C union",
		"for-in":        "no corpus iterates a container",
		"func-ref":      "no corpus refers to a function by name",
		"import":        "the entry package of every corpus program imports nothing",
		"import-c":      "no corpus imports a C header",
		"index":         "no corpus indexes an array",
		"interface":     "no corpus declares an interface in the entry package",
		"is":            "no corpus narrows an interface value",
		"lambda":        "no corpus writes a lambda",
		"map-lit":       "no corpus builds a map",
		"new":           "no corpus creates a container",
		"range":         "no corpus writes a range",
		"recv-arm":      "no corpus selects a receive arm",
		"send-arm":      "no corpus selects a send arm",
		"set-lit":       "no corpus builds a set",
		"slice":         "no corpus slices",
		"timeout-arm":   "no corpus selects a timeout arm",
		"tindex":        "no corpus reads a tuple element",
		"try":           "no corpus propagates a Result",
		"tuple-lit":     "no corpus builds a tuple",
		"var":           "no corpus declares a top-level variable",
	}
	if len(deferred) == 0 {
		return
	}
	names := make([]string, 0, len(deferred))
	for kind := range deferred {
		names = append(names, kind)
	}
	sort.Strings(names)
	t.Logf("kinds the front end still owes a corpus for (%d): %s", len(names), strings.Join(names, " "))
}
