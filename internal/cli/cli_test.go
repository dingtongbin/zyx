package cli

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
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

// proxyFor serves one module over the proxy protocol, so a fetch can be
// tested without the network.
func proxyFor(t *testing.T, path, version string) *httptest.Server {
	t.Helper()
	manifest := "module " + path + "\n\nzyx 0.1\n"
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	for name, body := range map[string]string{
		path + "@" + version + "/zyx.mod":       manifest,
		path + "@" + version + "/util/util.zyx": "package util\n\nfunc Use() {\n}\n",
	} {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		path + "/@v/list":                []byte(version + "\n"),
		path + "/@v/" + version + ".mod": []byte(manifest),
		path + "/@v/" + version + ".zip": archive.Bytes(),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, err := w.Write(body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestGetFetchesAndRecords(t *testing.T) {
	server := proxyFor(t, "example.com/dep", "v1.0.0")
	dir := t.TempDir()
	write(t, dir, map[string]string{"zyx.mod": "module example.com/app\n\nzyx 0.1\n"})
	s, _, errBuf := session(dir)
	s.Env = func(name string) string {
		if name == "ZYXPROXY" {
			return server.URL
		}
		return ""
	}
	// A requirement written without a version resolves to the latest release.
	if code := s.Run([]string{"get", "example.com/dep"}); code != 0 {
		t.Fatalf("get failed with %d:\n%s", code, errBuf)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "zyx.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "require example.com/dep v1.0.0") {
		t.Errorf("the manifest holds:\n%s\nwant a requirement for example.com/dep", manifest)
	}
	sum, err := os.ReadFile(filepath.Join(dir, "zyx.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sum), "example.com/dep v1.0.0 h1:") {
		t.Errorf("the checksum file holds:\n%s\nwant both hashes", sum)
	}
	if !strings.Contains(string(sum), "example.com/dep v1.0.0/zyx.mod h1:") {
		t.Errorf("the checksum file holds:\n%s\nwant the manifest hash", sum)
	}
	if _, err := os.Stat(filepath.Join(dir, ".zyx-mod", "example.com", "dep@v1.0.0", "util", "util.zyx")); err != nil {
		t.Errorf("the module is not in the cache: %v", err)
	}
}

func TestGetWithoutASource(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"zyx.mod": "module example.com/app\n\nzyx 0.1\n"})
	s, _, errBuf := session(dir)
	if code := s.Run([]string{"get", "example.com/dep"}); code == 0 {
		t.Fatal("get succeeded with no source configured")
	}
	if !strings.Contains(errBuf.String(), "ZYXPROXY") {
		t.Errorf("the message does not name the variable to set:\n%s", errBuf)
	}
	// Nothing was written.
	if _, err := os.Stat(filepath.Join(dir, "zyx.sum")); err == nil {
		t.Error("a checksum file was written by a fetch that failed")
	}
}

func TestSplitRequirement(t *testing.T) {
	cases := []struct {
		spec    string
		path    string
		version string
		bad     bool
	}{
		{"example.com/dep", "example.com/dep", "", false},
		{"example.com/dep@v1.2.3", "example.com/dep", "v1.2.3", false},
		{"example.com/dep@upgrade", "", "", true},
		{"example.com/dep@none", "", "", true},
		{"example.com/dep@", "", "", true},
		{"@v1.0.0", "", "", true},
	}
	for _, c := range cases {
		path, version, err := splitRequirement(c.spec)
		if (err != nil) != c.bad {
			t.Errorf("splitRequirement(%q) error = %v, want error %v", c.spec, err, c.bad)
			continue
		}
		if c.bad {
			continue
		}
		if path != c.path || version != c.version {
			t.Errorf("splitRequirement(%q) = %q %q, want %q %q", c.spec, path, version, c.path, c.version)
		}
	}
}

func TestVendorMirrorsAndBuildsOffline(t *testing.T) {
	server := proxyFor(t, "example.com/dep", "v1.0.0")
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"zyx.mod":  "module example.com/app\n\nzyx 0.1\n",
		"main.zyx": "package main\n\nimport \"example.com/dep/util\"\n\nfunc main() {\n    util.Use()\n}\n",
	})
	s, _, errBuf := session(dir)
	s.Env = func(name string) string {
		if name == "ZYXPROXY" {
			return server.URL
		}
		return ""
	}
	if code := s.Run([]string{"get", "example.com/dep"}); code != 0 {
		t.Fatalf("get failed with %d:\n%s", code, errBuf)
	}
	if code := s.Run([]string{"mod", "vendor"}); code != 0 {
		t.Fatalf("vendor failed with %d:\n%s", code, errBuf)
	}

	// The mirror holds the module and the file that describes it.
	if _, err := os.Stat(filepath.Join(dir, "vendor", "example.com", "dep", "util", "util.zyx")); err != nil {
		t.Errorf("the module is not mirrored: %v", err)
	}
	text, err := os.ReadFile(filepath.Join(dir, "vendor", "modules.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# example.com/dep v1.0.0", "## explicit; zyx 0.1", "example.com/dep/util"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("modules.txt does not hold %q:\n%s", want, text)
		}
	}

	// With the module cache gone the build reads the mirror, which is what
	// makes a vendored module build offline.
	if err := os.RemoveAll(filepath.Join(dir, ".zyx-mod")); err != nil {
		t.Fatal(err)
	}
	if code := s.Run([]string{"check", "./..."}); code != 0 {
		t.Fatalf("check failed with a mirror and no cache: %d\n%s", code, errBuf)
	}
}

func TestTidyAddsFetchesAndDrops(t *testing.T) {
	server := proxyFor(t, "example.com/dep", "v1.0.0")
	// A requirement that nothing reads is still one the graph reads the
	// manifest of, so the source serves it too.
	other := proxyFor(t, "example.com/unused", "v1.0.0")
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"zyx.mod":  "module example.com/app\n\nzyx 0.1\n\nrequire example.com/unused v1.0.0\n",
		"main.zyx": "package main\n\nimport \"example.com/dep/util\"\n\nfunc main() {\n    util.Use()\n}\n",
	})
	s, _, errBuf := session(dir)
	s.Env = func(name string) string {
		if name == "ZYXPROXY" {
			return server.URL + "," + other.URL
		}
		return ""
	}
	if code := s.Run([]string{"mod", "tidy"}); code != 0 {
		t.Fatalf("tidy failed with %d:\n%s", code, errBuf)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "zyx.mod"))
	if err != nil {
		t.Fatal(err)
	}
	// The import is required now, and the requirement nothing reads is gone.
	if !strings.Contains(string(manifest), "require example.com/dep v1.0.0") {
		t.Errorf("the manifest holds:\n%s\nwant a requirement for example.com/dep", manifest)
	}
	if strings.Contains(string(manifest), "example.com/unused") {
		t.Errorf("the manifest still requires a module nothing reads:\n%s", manifest)
	}
	sum, err := os.ReadFile(filepath.Join(dir, "zyx.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sum), "example.com/dep v1.0.0 h1:") {
		t.Errorf("the checksum file holds:\n%s\nwant the hashes of what was fetched", sum)
	}
	if strings.Contains(string(sum), "example.com/unused") {
		t.Errorf("the checksum file still records a dropped module:\n%s", sum)
	}

	// Running it again changes nothing, which is what makes it usable in a
	// check.
	before := string(manifest) + string(sum)
	if code := s.Run([]string{"mod", "tidy"}); code != 0 {
		t.Fatalf("a second tidy failed with %d:\n%s", code, errBuf)
	}
	again, err := os.ReadFile(filepath.Join(dir, "zyx.mod"))
	if err != nil {
		t.Fatal(err)
	}
	againSum, err := os.ReadFile(filepath.Join(dir, "zyx.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if string(again)+string(againSum) != before {
		t.Errorf("a second tidy changed the files:\n%s\nwant\n%s", string(again)+string(againSum), before)
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
