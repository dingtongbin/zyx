package build

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

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
		include, test := fileFor(c.name, target)
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
