package build

import (
	"bytes"
	"path/filepath"
	"testing"

	"zyx/internal/toolchain"
)

// TestAWarmFrontEndMatchesTheColdOne checks what the front-end cache is for: a
// run that finds every entry produces the same tree and the same diagnostics
// as the run that built them, byte for byte, so an incremental build is a full
// one.
func TestAWarmFrontEndMatchesTheColdOne(t *testing.T) {
	files := map[string]string{
		"zyx.mod": "module example.com/app\n\nzyx 0.1\n",
		"main.zyx": "package main\n\nimport \"example.com/app/greet\"\n\n" +
			"func main() {\n\tgreet.Hello()\n}\n",
		"greet/greet.zyx": "package greet\n\nfunc Hello() -> i64 {\n\treturn 1 + 2\n}\n",
	}
	root := t.TempDir()
	write(t, root, files)
	loader := &Loader{Root: root, ModulePath: "example.com/app", Target: toolchain.HostTarget()}
	opts := FrontEndOptions{CacheDir: filepath.Join(root, ".zyx-cache"), Compiler: "test"}

	coldCache := OpenFrontEnd(root, opts)
	cold, err := (&Session{Loader: loader, Cache: coldCache}).FrontEnd("example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	coldHits, coldMisses := coldCache.Stats()
	if coldHits != 0 {
		t.Errorf("a cold run reports %d hits, want 0", coldHits)
	}
	if coldMisses != len(cold) {
		t.Errorf("a cold run reports %d misses, want %d", coldMisses, len(cold))
	}

	warmCache := OpenFrontEnd(root, opts)
	warm, err := (&Session{Loader: loader, Cache: warmCache}).FrontEnd("example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	warmHits, warmMisses := warmCache.Stats()
	if warmMisses != 0 {
		t.Errorf("a warm run reports %d misses, want 0", warmMisses)
	}
	if warmHits != len(warm) {
		t.Errorf("a warm run reports %d hits, want %d", warmHits, len(warm))
	}

	if len(cold) != len(warm) {
		t.Fatalf("cold = %d packages, warm = %d", len(cold), len(warm))
	}
	for i := range cold {
		if cold[i].Package.ImportPath != warm[i].Package.ImportPath {
			t.Fatalf("package %d is %s cold and %s warm", i, cold[i].Package.ImportPath, warm[i].Package.ImportPath)
		}
		if !bytes.Equal(cold[i].Text, warm[i].Text) {
			t.Errorf("%s: the product differs:\n--- cold ---\n%s\n--- warm ---\n%s",
				cold[i].Package.ImportPath, cold[i].Text, warm[i].Text)
		}
		if !bytes.Equal(cold[i].Report, warm[i].Report) {
			t.Errorf("%s: the diagnostics differ:\n--- cold ---\n%s\n--- warm ---\n%s",
				cold[i].Package.ImportPath, cold[i].Report, warm[i].Report)
		}
		if (cold[i].IR == nil) != (warm[i].IR == nil) {
			t.Errorf("%s: one run has a tree and the other does not", cold[i].Package.ImportPath)
		}
	}
}

// TestAWarmFrontEndReplaysTheDiagnostics checks the other half of the product:
// a package that reported diagnostics is cached too, and a warm run prints
// what the cold one printed.
func TestAWarmFrontEndReplaysTheDiagnostics(t *testing.T) {
	files := map[string]string{
		"zyx.mod": "module example.com/app\n\nzyx 0.1\n",
		"main.zyx": "package main\n\nimport \"example.com/app/greet\"\n\n" +
			"func main() {\n\tgreet.Hello()\n}\n",
		"greet/greet.zyx": "package greet\n\nfunc Hello() -> i64 {\n\treturn \"text\"\n}\n",
	}
	root := t.TempDir()
	write(t, root, files)
	loader := &Loader{Root: root, ModulePath: "example.com/app", Target: toolchain.HostTarget()}
	opts := FrontEndOptions{CacheDir: filepath.Join(root, ".zyx-cache"), Compiler: "test"}

	coldCache := OpenFrontEnd(root, opts)
	cold, err := (&Session{Loader: loader, Cache: coldCache}).FrontEnd("example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	_, coldMisses := coldCache.Stats()
	if coldMisses == 0 {
		t.Fatal("the cold run built no entry")
	}

	warmCache := OpenFrontEnd(root, opts)
	warm, err := (&Session{Loader: loader, Cache: warmCache}).FrontEnd("example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	warmHits, warmMisses := warmCache.Stats()
	if warmMisses != 0 || warmHits != len(warm) {
		t.Errorf("a warm run reports %d hits and %d misses, want %d and 0", warmHits, warmMisses, len(warm))
	}

	reported := false
	for i := range cold {
		if len(cold[i].Report) != 0 {
			reported = true
		}
		if !bytes.Equal(cold[i].Report, warm[i].Report) {
			t.Errorf("%s: the diagnostics differ:\n--- cold ---\n%s\n--- warm ---\n%s",
				cold[i].Package.ImportPath, cold[i].Report, warm[i].Report)
		}
	}
	if !reported {
		t.Fatal("the program was expected to report a diagnostic")
	}
}

// TestAFrontEndKeyCoversTheSources checks the key of §9.2: the sources of the
// package, the interfaces of what it imports, and the module files all take
// part, so a change to any of them is a miss.
func TestAFrontEndKeyCoversTheSources(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"zyx.mod":         "module example.com/app\n\nzyx 0.1\n",
		"main.zyx":        "package main\n\nimport \"example.com/app/greet\"\n\nfunc main() {\n\tgreet.Hello()\n}\n",
		"greet/greet.zyx": "package greet\n\nfunc Hello() -> i64 {\n\treturn 1\n}\n",
	})
	loader := &Loader{Root: root, ModulePath: "example.com/app", Target: toolchain.HostTarget()}
	f := OpenFrontEnd(root, FrontEndOptions{CacheDir: filepath.Join(root, ".zyx-cache"), Compiler: "test"})
	pkg, err := loader.LoadPackage("example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	dep, err := loader.LoadPackage("example.com/app/greet")
	if err != nil {
		t.Fatal(err)
	}
	packages := map[string]*Package{pkg.ImportPath: pkg, dep.ImportPath: dep}
	same := f.key(pkg, "amd64-pc-windows-msvc", transitiveImports(pkg, packages))
	if again := f.key(pkg, "amd64-pc-windows-msvc", transitiveImports(pkg, packages)); again != same {
		t.Errorf("the same inputs gave %q and %q", same, again)
	}
	if other := f.key(pkg, "arm64-unknown-linux-gnu", transitiveImports(pkg, packages)); other == same {
		t.Error("the target must take part in the key")
	}
	f.exports["example.com/app/greet"] = "abc"
	if other := f.key(pkg, "amd64-pc-windows-msvc", transitiveImports(pkg, packages)); other == same {
		t.Error("the interface of an import must take part in the key")
	}
}
