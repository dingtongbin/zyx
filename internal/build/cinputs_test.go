package build

import (
	"path/filepath"
	"strings"
	"testing"

	"zyx/internal/toolchain"
)

// TestAFixedHeaderIsNotBehindTheOldError checks that the C sources and headers of
// a package decide its cache entry. A package whose header does not parse is
// cached with that failure, and fixing the header has to be a miss: otherwise the
// same failure is reported by every later run and a fix looks like it changed
// nothing.
func TestAFixedHeaderIsNotBehindTheOldError(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	files := map[string]string{
		"zyx.mod": "module example.com/app\n\nzyx 0.1\n",
		"main.zyx": "package main\n\nimport \"example.com/app/dep\"\n\n" +
			"func main() -> i32 {\n\treturn dep.Fetch()\n}\n",
		"dep/dep.zyx": "package dep\n\nimport c \"dep.h\"\n\n" +
			"func Fetch() -> i32 {\n\treturn c.zg_dep_value()\n}\n",
		"dep/dep.h": "zg_missing_type zg_dep_value(void);\n",
	}
	root := t.TempDir()
	write(t, root, files)
	opts := FrontEndOptions{CacheDir: filepath.Join(root, ".zyx-cache"), Compiler: "test"}

	run := func() string {
		loader := &Loader{Root: root, ModulePath: "example.com/app", Target: toolchain.HostTarget()}
		cache := OpenFrontEnd(root, opts)
		results, err := (&Session{Loader: loader, Cache: cache}).FrontEnd("example.com/app")
		if err != nil {
			t.Fatal(err)
		}
		var report strings.Builder
		for _, res := range results {
			report.Write(res.Report)
		}
		return report.String()
	}

	before := run()
	if !strings.Contains(before, "E3016") {
		t.Fatalf("the first run did not report the header:\n%s", before)
	}

	write(t, root, map[string]string{"dep/dep.h": "int zg_dep_value(void);\n"})
	after := run()
	if strings.Contains(after, "E3016") {
		t.Errorf("a header that was fixed still reports the old failure:\n%s", after)
	}
}
