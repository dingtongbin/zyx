package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// session returns a session rooted at a directory.
func session(dir string) (*Session, *bytes.Buffer, *bytes.Buffer) {
	var out, errBuf bytes.Buffer
	return &Session{Stdout: &out, Stderr: &errBuf, Dir: dir, Env: func(string) string { return "" }}, &out, &errBuf
}

// write lays out files under root.
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

func TestParseOptionsCollectsFlags(t *testing.T) {
	s, _, _ := session(t.TempDir())
	o, err := s.parseOptions([]string{"-O3", "-g", "--jobs", "4", "-l", "m", "-L", "/usr/lib", "./..."})
	if err != nil {
		t.Fatal(err)
	}
	if o.level != "-O3" || !o.levelSet || !o.debug || o.jobs != 4 {
		t.Errorf("options = %+v", o)
	}
	if len(o.libs) != 1 || o.libs[0] != "m" || len(o.libDirs) != 1 {
		t.Errorf("libraries = %v, %v", o.libs, o.libDirs)
	}
	if len(o.paths) != 1 || o.paths[0] != "./..." {
		t.Errorf("paths = %v", o.paths)
	}
	if o.target.Triple == "" {
		t.Error("the host target must be the default")
	}
	if o.level == "" || o.level != "-O3" {
		t.Errorf("level = %q", o.level)
	}
}

func TestParseOptionsDefaults(t *testing.T) {
	s, _, _ := session(t.TempDir())
	o, err := s.parseOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.level != "-O2" || o.levelSet {
		t.Errorf("level = %q, set = %v", o.level, o.levelSet)
	}
}

func TestParseOptionsRejectsUnknownFlags(t *testing.T) {
	s, _, _ := session(t.TempDir())
	if _, err := s.parseOptions([]string{"--nope"}); err == nil {
		t.Fatal("an unknown option must be an error")
	}
	if _, err := s.parseOptions([]string{"--jobs", "x"}); err == nil {
		t.Fatal("a non-numeric job count must be an error")
	}
	if _, err := s.parseOptions([]string{"--target", "sparc-unknown-linux"}); err == nil {
		t.Fatal("an unknown architecture must be an error")
	}
}

func TestParseOptionsTakesProgramArgumentsAfterDoubleDash(t *testing.T) {
	s, _, _ := session(t.TempDir())
	o, err := s.parseOptions([]string{"run", "./...", "--", "-v", "extra"})
	if err != nil {
		t.Fatal(err)
	}
	if len(o.paths) != 2 || len(o.programArgs) != 2 {
		t.Fatalf("paths = %v, program arguments = %v", o.paths, o.programArgs)
	}
	if o.programArgs[0] != "-v" || o.programArgs[1] != "extra" {
		t.Errorf("program arguments = %v", o.programArgs)
	}
}

func TestFindModuleWalksUp(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"zyx.mod":        "module example.com/app\n\nzyx 0.1\n",
		"deep/a/b.zyx":   "package b\n",
		"deep/a/c/x.zyx": "package x\n",
	})
	s, _, _ := session(filepath.Join(root, "deep", "a", "c"))
	m, err := s.findModule(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.File.Module != "example.com/app" {
		t.Errorf("module = %q", m.File.Module)
	}
	if m.Root != root {
		t.Errorf("root = %q, want %q", m.Root, root)
	}
}

func TestFindModuleWithoutAManifest(t *testing.T) {
	s, _, _ := session(t.TempDir())
	if _, err := s.findModule(s.Dir); err == nil {
		t.Fatal("a directory with no manifest above it must fail")
	}
}

func TestImportPathMapsDirectories(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"zyx.mod":      "module example.com/app\n",
		"server/s.zyx": "package server\n",
	})
	s, _, _ := session(root)
	m, err := s.findModule(root)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		".":        "example.com/app",
		"./server": "example.com/app/server",
		"server":   "example.com/app/server",
		"std/net":  "std/net",
	}
	for arg, want := range cases {
		got, err := s.importPath(m, arg)
		if err != nil {
			t.Errorf("importPath(%q): %v", arg, err)
			continue
		}
		if got != want {
			t.Errorf("importPath(%q) = %q, want %q", arg, got, want)
		}
	}
}

func TestImportPathRejectsOutsideDirectories(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"zyx.mod": "module example.com/app\n"})
	s, _, _ := session(root)
	m, err := s.findModule(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.importPath(m, "../elsewhere"); err == nil {
		t.Fatal("a directory outside the module must fail")
	}
}

func TestModInitWritesAManifest(t *testing.T) {
	dir := t.TempDir()
	s, _, errBuf := session(dir)
	if code := s.Run([]string{"mod", "init", "example.com/app"}); code != exitOK {
		t.Fatalf("mod init failed with %d:\n%s", code, errBuf)
	}
	data, err := os.ReadFile(filepath.Join(dir, "zyx.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, "module example.com/app") || !strings.Contains(got, "zyx 0.1") {
		t.Errorf("zyx.mod =\n%s", got)
	}
	// A second run must refuse to overwrite.
	if code := s.Run([]string{"mod", "init", "example.com/app"}); code != exitError {
		t.Errorf("a second mod init returned %d, want %d", code, exitError)
	}
}

func TestModInitNeedsAFullPath(t *testing.T) {
	s, _, _ := session(t.TempDir())
	if code := s.Run([]string{"mod", "init", "plain"}); code != exitUsage {
		t.Errorf("mod init returned %d, want %d", code, exitUsage)
	}
}

func TestNewWritesASkeleton(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "app")
	s, _, errBuf := session(parent)
	if code := s.Run([]string{"new", dir}); code != exitOK {
		t.Fatalf("new failed with %d:\n%s", code, errBuf)
	}
	for _, name := range []string{"zyx.mod", "main.zyx", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s is missing: %v", name, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "zyx.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "module example.com/app") {
		t.Errorf("zyx.mod =\n%s", data)
	}
}

func TestUnknownCommandIsAUsageError(t *testing.T) {
	s, _, errBuf := session(t.TempDir())
	if code := s.Run([]string{"nope"}); code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errBuf.String(), "unknown command") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}

func TestUsageListsEveryCommand(t *testing.T) {
	s, out, _ := session(t.TempDir())
	if code := s.Run([]string{"help"}); code != exitOK {
		t.Fatalf("help returned %d", code)
	}
	for _, command := range []string{"check", "build", "run", "fmt", "mod", "new", "version", "emit-zyxc"} {
		if !strings.Contains(out.String(), command) {
			t.Errorf("the usage text does not mention %s", command)
		}
	}
}
