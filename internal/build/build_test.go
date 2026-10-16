package build

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"zyx/internal/diag"
	"zyx/internal/mod"
	"zyx/internal/toolchain"
)

// write lays out files under root, creating directories as needed.
func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// linuxAMD64 is the target the selection tests run against.
func linuxAMD64() toolchain.Target {
	t, err := toolchain.ParseTarget("amd64-unknown-linux-gnu")
	if err != nil {
		panic(err)
	}
	return t
}

// loader builds a loader over a temporary module tree.
func loader(t *testing.T, files map[string]string) *Loader {
	t.Helper()
	root := t.TempDir()
	write(t, root, files)
	return &Loader{Root: root, ModulePath: "example.com/app", Target: linuxAMD64()}
}

func TestLoadPackageCollectsFiles(t *testing.T) {
	l := loader(t, map[string]string{
		"server/server.zyx": "package server\n\nfunc Listen() {\n}\n",
		"server/conn.zyx":   "package server\n\nstruct Conn {\n\thost: str\n}\n",
	})
	pkg, err := l.LoadPackage("example.com/app/server")
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Name != "server" {
		t.Errorf("name = %q, want server", pkg.Name)
	}
	if len(pkg.Files) != 2 || len(pkg.Syntax) != 2 {
		t.Fatalf("files = %d, syntax = %d, want 2 and 2", len(pkg.Files), len(pkg.Syntax))
	}
	if got := pkg.Files[0].Name(); got != "server/conn.zyx" {
		t.Errorf("first file = %q, want server/conn.zyx", got)
	}
	if len(pkg.Diags) != 0 {
		t.Errorf("diagnostics = %v", pkg.Diags)
	}
}

func TestLoadPackageReportsParseErrors(t *testing.T) {
	l := loader(t, map[string]string{
		"server/server.zyx": "package server\n\nfunc f( {\n}\n",
	})
	pkg, err := l.LoadPackage("example.com/app/server")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Diags) == 0 {
		t.Error("a malformed file must report a diagnostic")
	}
}

func TestPlatformSuffixSelection(t *testing.T) {
	l := loader(t, map[string]string{
		"server/net.zyx":               "package server\n",
		"server/net_linux.zyx":         "package server\n",
		"server/net_windows.zyx":       "package server\n",
		"server/net_arm64.zyx":         "package server\n",
		"server/net_linux_arm64.zyx":   "package server\n",
		"server/net_linux_amd64.zyx":   "package server\n",
		"server/net_foo.zyx":           "package server\n",
		"server/linux.zyx":             "package server\n",
		"server/net_linux_test.zyx":    "package server\n",
		"server/net_test.zyx":          "package server\n",
		"server/net_windows_amd64.zyx": "package server\n",
	})
	pkg, err := l.LoadPackage("example.com/app/server")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range pkg.Files {
		got[filepath.Base(f.Name())] = true
	}
	want := []string{
		"net.zyx",
		"net_linux.zyx",
		"net_linux_amd64.zyx",
		"net_foo.zyx",
		"linux.zyx",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("%s must take part in a linux/amd64 build", name)
		}
	}
	for _, name := range []string{
		"net_windows.zyx",
		"net_arm64.zyx",
		"net_linux_arm64.zyx",
		"net_windows_amd64.zyx",
		"net_linux_test.zyx",
		"net_test.zyx",
	} {
		if got[name] {
			t.Errorf("%s must not take part in a linux/amd64 build", name)
		}
	}
}

func TestTestFilesIncludedWhenTesting(t *testing.T) {
	l := loader(t, map[string]string{
		"server/net.zyx":            "package server\n",
		"server/net_linux_test.zyx": "package server\n",
		"server/net_test.zyx":       "package server\n",
	})
	l.Testing = true
	pkg, err := l.LoadPackage("example.com/app/server")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Files) != 3 {
		t.Errorf("files = %d, want 3", len(pkg.Files))
	}
}

func TestEmptyPackage(t *testing.T) {
	l := loader(t, map[string]string{
		"server/net_windows.zyx": "package server\n",
	})
	pkg, err := l.LoadPackage("example.com/app/server")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Files) != 0 {
		t.Errorf("files = %d, want none", len(pkg.Files))
	}
	if pkg.Name != "" {
		t.Errorf("name = %q, want empty", pkg.Name)
	}
}

func TestImportGraphOrder(t *testing.T) {
	l := loader(t, map[string]string{
		"app/app.zyx": "package app\n\nimport \"example.com/app/mid\"\n",
		"mid/mid.zyx": "package mid\n\nimport \"example.com/app/low\"\n",
		"low/low.zyx": "package low\n",
	})
	g, err := l.Load("example.com/app/app")
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, pkg := range g.Order {
		order = append(order, pkg.ImportPath)
	}
	want := "example.com/app/low example.com/app/mid example.com/app/app"
	if got := strings.Join(order, " "); got != want {
		t.Errorf("order = %q, want %q", got, want)
	}
}

func TestImportCycleIsAnError(t *testing.T) {
	l := loader(t, map[string]string{
		"a/a.zyx": "package a\n\nimport \"example.com/app/b\"\n",
		"b/b.zyx": "package b\n\nimport \"example.com/app/a\"\n",
	})
	if _, err := l.Load("example.com/app/a"); err == nil {
		t.Fatal("an import cycle must report an error")
	} else if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error = %v, want an import cycle", err)
	}
}

func TestStdImportsNeedNoRequirement(t *testing.T) {
	l := loader(t, map[string]string{
		"app/app.zyx": "package app\n\nimport \"std/net\"\n",
	})
	std := t.TempDir()
	write(t, std, map[string]string{"net/net.zyx": "package net\n"})
	l.StdRoot = std
	g, err := l.Load("example.com/app/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Order) != 2 {
		t.Fatalf("packages = %d, want 2", len(g.Order))
	}
	if g.Order[0].ImportPath != "std/net" {
		t.Errorf("first package = %q, want std/net", g.Order[0].ImportPath)
	}
}

func TestResolveRejectsUnknownAndRelativePaths(t *testing.T) {
	l := loader(t, map[string]string{"app/app.zyx": "package app\n"})
	for _, path := range []string{
		"./other",
		"../other",
		"/abs",
		"example.com/other/thing",
	} {
		if _, err := l.Resolve(path); err == nil {
			t.Errorf("%q must not resolve", path)
		}
	}
}

func TestInternalImportIsRefused(t *testing.T) {
	// A path with an internal segment is only importable from inside the
	// subtree that holds it, so the loader reports the import rather than
	// reaching the package.
	l := loader(t, map[string]string{
		"main.zyx":                   "package main\n\nimport \"example.com/app/sub/internal/util\"\n\nfunc Main() {\n}\n",
		"sub/internal/util/util.zyx": "package util\n\nfunc Use() {\n}\n",
	})
	g, err := l.Load("example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	entry := g.Order[len(g.Order)-1]
	if len(entry.Diags) == 0 {
		t.Fatal("an import from outside the subtree was accepted")
	}
	if entry.Diags[0].Code != diag.CodeImportInvalid {
		t.Errorf("code = %s, want %s", entry.Diags[0].Code, diag.CodeImportInvalid)
	}
}

func TestInternalImportFromInsideIsAccepted(t *testing.T) {
	l := loader(t, map[string]string{
		"main.zyx":                   "package main\n\nimport \"example.com/app/sub/server\"\n\nfunc Main() {\n}\n",
		"sub/server/server.zyx":      "package server\n\nimport \"example.com/app/sub/internal/util\"\n\nfunc Listen() {\n}\n",
		"sub/internal/util/util.zyx": "package util\n\nfunc Use() {\n}\n",
	})
	g, err := l.Load("example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range g.Order {
		if len(pkg.Diags) != 0 {
			t.Errorf("%s reported %v", pkg.ImportPath, pkg.Diags)
		}
	}
}

func TestOutputPaths(t *testing.T) {
	linux := linuxAMD64()
	root := t.TempDir()
	paths, err := OutputPaths(root, []string{"example.com/app"}, "", linux)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "build", "app"); paths[0] != want {
		t.Errorf("path = %q, want %q", paths[0], want)
	}

	// The target decides the extension an executable carries.
	if windows, err := toolchain.ParseTarget("amd64-pc-windows-gnu"); err == nil {
		paths, err := OutputPaths(root, []string{"example.com/app"}, "", windows)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(root, "build", "app.exe"); paths[0] != want {
			t.Errorf("windows path = %q, want %q", paths[0], want)
		}
	}

	// Two main packages whose directories share a name cannot share the
	// default directory.
	if _, err := OutputPaths(root, []string{"example.com/app/tool", "example.com/lib/tool"}, "", linux); err == nil {
		t.Error("two packages with the same directory name were accepted")
	}

	// A file holds one executable only.
	if _, err := OutputPaths(root, []string{"example.com/app", "example.com/other"}, "out", linux); err == nil {
		t.Error("two main packages were accepted with a file output")
	}

	// A directory holds one executable per main package, named after its
	// directory.
	dir := filepath.Join(root, "bin") + string(filepath.Separator)
	paths, err = OutputPaths(root, []string{"example.com/app", "example.com/other"}, dir, linux)
	if err != nil {
		t.Fatal(err)
	}
	if paths[0] != filepath.Join(root, "bin", "app") || paths[1] != filepath.Join(root, "bin", "other") {
		t.Errorf("directory paths = %v", paths)
	}
}

// zipOf builds a module archive holding the named files.
func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestResolvesARequiredModule(t *testing.T) {
	root := t.TempDir()
	cache, err := mod.OpenModuleCache(filepath.Join(root, ".zyx-mod"))
	if err != nil {
		t.Fatal(err)
	}
	// A module in the cache, as a fetch would leave it.
	archive := zipOf(t, map[string]string{
		"example.com/dep@v1.0.0/zyx.mod":       "module example.com/dep\n\nzyx 0.1\n",
		"example.com/dep@v1.0.0/util/util.zyx": "package util\n\nfunc Use() {\n}\n",
	})
	content, err := mod.HashContent(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Put("example.com/dep", "v1.0.0", content, archive); err != nil {
		t.Fatal(err)
	}
	write(t, root, map[string]string{
		"main.zyx": "package main\n\nimport \"example.com/dep/util\"\n\nfunc Main() {\n}\n",
	})
	l := &Loader{
		Root:       root,
		ModulePath: "example.com/app",
		Target:     linuxAMD64(),
		Requires:   map[string]string{"example.com/dep": "v1.0.0"},
		Modules:    cache,
	}
	pkg, err := l.LoadPackage("example.com/dep/util")
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Name != "util" {
		t.Errorf("name = %q, want util", pkg.Name)
	}
	if _, err := l.Load("example.com/app"); err != nil {
		t.Fatal(err)
	}
}

func TestModuleOfTakesTheLongestPrefix(t *testing.T) {
	l := &Loader{Requires: map[string]string{
		"example.com/dep":      "v1.0.0",
		"example.com/dep/tool": "v2.0.0",
	}}
	path, version, ok := l.moduleOf("example.com/dep/tool/run")
	if !ok || path != "example.com/dep/tool" || version != "v2.0.0" {
		t.Errorf("moduleOf = %q %q %v, want example.com/dep/tool v2.0.0", path, version, ok)
	}
	path, version, ok = l.moduleOf("example.com/dep/util")
	if !ok || path != "example.com/dep" || version != "v1.0.0" {
		t.Errorf("moduleOf = %q %q %v, want example.com/dep v1.0.0", path, version, ok)
	}
	if _, _, ok := l.moduleOf("example.com/other"); ok {
		t.Error("an unrelated path resolved to a requirement")
	}
}

func TestVendorTakesPrecedence(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"vendor/example.com/dep/util/util.zyx": "package util\n\nfunc Use() {\n}\n",
	})
	l := &Loader{
		Root:       root,
		ModulePath: "example.com/app",
		Target:     linuxAMD64(),
		Requires:   map[string]string{"example.com/dep": "v1.0.0"},
		Vendor:     filepath.Join(root, "vendor"),
	}
	dir, err := l.Resolve("example.com/dep/util")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "vendor", "example.com", "dep", "util"); dir != want {
		t.Errorf("dir = %q, want the vendor copy at %q", dir, want)
	}
}

func TestImportableInternalRule(t *testing.T) {
	cases := []struct {
		from string
		to   string
		want bool
	}{
		{"example.com/app", "example.com/app/server", true},
		{"example.com/app/server", "example.com/app/internal/util", true},
		{"example.com/app/internal/util", "example.com/app/internal/other", true},
		{"example.com/app/other", "example.com/app/internal/util", true},
		{"example.com/other", "example.com/app/internal/util", false},
		{"example.com/app", "example.com/app/internal", true},
	}
	for _, c := range cases {
		if got := Importable(c.from, c.to); got != c.want {
			t.Errorf("Importable(%q, %q) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestFileFor(t *testing.T) {
	target := linuxAMD64()
	cases := []struct {
		name    string
		include bool
		test    bool
	}{
		{"net.zyx", true, false},
		{"net_linux.zyx", true, false},
		{"net_windows.zyx", false, false},
		{"net_amd64.zyx", true, false},
		{"net_arm64.zyx", false, false},
		{"net_linux_amd64.zyx", true, false},
		{"net_windows_amd64.zyx", false, false},
		{"net_foo.zyx", true, false},
		{"net_linux_foo.zyx", true, false},
		{"linux.zyx", true, false},
		{"net_test.zyx", true, true},
		{"net_linux_test.zyx", true, true},
		{"net_windows_test.zyx", false, true},
	}
	for _, c := range cases {
		include, test := fileFor(c.name, ".zyx", target)
		if include != c.include || test != c.test {
			t.Errorf("fileFor(%q) = %v, %v; want %v, %v", c.name, include, test, c.include, c.test)
		}
	}
}

func TestFrontEndRunsEveryStage(t *testing.T) {
	l := loader(t, map[string]string{
		"app/app.zyx":   "package app\n\nimport \"example.com/app/conn\"\n\nfunc Run(link: conn.Conn) -> i64 {\n\tlink.Close()\n\treturn 0\n}\n",
		"conn/conn.zyx": "package conn\n\nstruct Conn {\n\thost: str\n\tfunc Close() {\n\t}\n}\n",
	})
	s := &Session{Loader: l}
	results, err := s.FrontEnd("example.com/app/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("packages = %d, want 2", len(results))
	}
	for _, res := range results {
		if len(res.Diags) != 0 {
			t.Fatalf("%s: %v", res.Package.ImportPath, res.Diags)
		}
		if res.IR == nil || len(res.Text) == 0 {
			t.Fatalf("%s: no intermediate representation", res.Package.ImportPath)
		}
	}
	last := results[len(results)-1]
	if last.Package.ImportPath != "example.com/app/app" {
		t.Fatalf("last package = %s, want the entry", last.Package.ImportPath)
	}
	if !strings.Contains(string(last.Text), "mcall Close example.com/app/conn.Conn.Close") {
		t.Errorf("the cross-package method reference is missing:\n%s", last.Text)
	}
}

func TestFrontEndStopsAfterAFailure(t *testing.T) {
	l := loader(t, map[string]string{
		"app/app.zyx": "package app\n\nfunc f() -> i64 => missing\n",
	})
	s := &Session{Loader: l}
	results, err := s.FrontEnd("example.com/app/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("packages = %d, want 1", len(results))
	}
	if len(results[0].Diags) == 0 {
		t.Error("want a diagnostic")
	}
	if results[0].IR != nil {
		t.Error("a package that failed must not reach emission")
	}
}

func TestParseIsDeterministicAcrossManyFiles(t *testing.T) {
	files := map[string]string{}
	var want []string
	for i := 0; i < 32; i++ {
		name := "server/f" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".zyx"
		files[name] = "package server\n"
		want = append(want, filepath.Base(name))
	}
	sort.Strings(want)
	l := loader(t, files)
	pkg, err := l.LoadPackage("example.com/app/server")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Files) != len(want) {
		t.Fatalf("files = %d, want %d", len(pkg.Files), len(want))
	}
	for i, f := range pkg.Files {
		if filepath.Base(f.Name()) != want[i] {
			t.Fatalf("file %d = %q, want %q", i, f.Name(), want[i])
		}
	}
}

func TestPayloadFreeVariantPatternIsAVariant(t *testing.T) {
	l := loader(t, map[string]string{
		"app/app.zyx": "package app\n\nenum Colour {\n\tRed\n\tGreen\n}\n\nfunc Name(colour: Colour) -> i64 {\n\tmatch colour {\n\t\tRed => return 1\n\t\tGreen => return 2\n\t}\n}\n",
	})
	s := &Session{Loader: l}
	results, err := s.FrontEnd("example.com/app/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(results[0].Diags) != 0 {
		t.Fatalf("diagnostics: %v", results[0].Diags)
	}
	text := string(results[0].Text)
	// A bare name that names a variant is a variant pattern, not a binding:
	// a binding would make the arm irrefutable and the enum unmatched.
	for _, want := range []string{"pat-variant Red", "pat-variant Green"} {
		if !strings.Contains(text, want) {
			t.Errorf("the tree is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "pat-bind Red") || strings.Contains(text, "pat-bind Green") {
		t.Errorf("a variant name was taken as a binding:\n%s", text)
	}
}
