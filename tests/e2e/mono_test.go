package e2e

import (
	"path/filepath"
	"strings"
	"testing"

	"zyx/internal/zyxc"
)

// TestEmitZyxcMonoWritesTheInstantiatedView checks the view `zyx emit-zyxc
// --mono` writes: the same format as the front end's output, with every
// generic definition replaced by one instance per signature, each written
// once and in the order the uses appear.
func TestEmitZyxcMonoWritesTheInstantiatedView(t *testing.T) {
	dir := copyTree(t, filepath.Join("testdata", "mono"))

	generic, stderr, code := run(dir, "emit-zyxc", ".")
	if code != 0 {
		t.Fatalf("emit-zyxc failed with %d:\n%s", code, stderr)
	}
	if !strings.Contains(generic, "type-param") {
		t.Errorf("the front end's view must keep the generic definitions:\n%s", generic)
	}

	mono, stderr, code := run(dir, "emit-zyxc", "--mono", ".")
	if code != 0 {
		t.Fatalf("emit-zyxc --mono failed with %d:\n%s", code, stderr)
	}
	file, err := zyxc.Parse([]byte(mono))
	if err != nil {
		t.Fatalf("the monomorphized view does not read back: %v\n%s", err, mono)
	}
	if strings.Contains(mono, "type-param") || strings.Contains(mono, "<T>") {
		t.Errorf("the monomorphized view still holds generic text:\n%s", mono)
	}

	// Every instance is written once, and the order is the order the uses
	// appear in the source: i64 first, then str.
	var seen []string
	for _, decl := range file.Decls {
		if decl.Kind != "func" && decl.Kind != "struct" {
			continue
		}
		if len(decl.Payload) < 1 || !strings.Contains(decl.Payload[len(decl.Payload)-1], "$$") {
			continue
		}
		seen = append(seen, decl.Payload[len(decl.Payload)-1])
	}
	want := []string{
		"example.com/mono.Pick$$1e9275fc575d5840",
		"example.com/mono.Pick$$330aaedbbea90d8f",
		"example.com/mono.Box$$1e9275fc575d5840",
		"example.com/mono.Box$$330aaedbbea90d8f",
	}
	if len(seen) != len(want) {
		t.Fatalf("the view holds %d instances, want %d:\n%s", len(seen), len(want), mono)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("instance %d is %q, want %q", i, seen[i], want[i])
		}
	}

	// The same inputs must give the same bytes, and the text names no
	// absolute path, because the format is a cache key as well as a view.
	again, _, code := run(dir, "emit-zyxc", "--mono", ".")
	if code != 0 {
		t.Fatal("the second emit-zyxc --mono failed")
	}
	if again != mono {
		t.Errorf("two runs differ:\n%s\n%s", mono, again)
	}
	if strings.Contains(mono, filepath.ToSlash(dir)) || strings.Contains(mono, ":/") {
		t.Errorf("the view names an absolute path:\n%s", mono)
	}
}
