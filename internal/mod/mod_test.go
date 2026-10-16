package mod

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestParseManifest(t *testing.T) {
	src := `module example.com/app

zyx 1.0

require github.com/foo/bar v1.2.3
require github.com/baz/qux v0.4.0 // indirect

require (
	github.com/one/two v2.0.0
	github.com/three/four v1.0.0-20260101120000-abcdef123456
)

exclude github.com/bad/idea v1.0.1

replace github.com/foo/bar => ../bar
replace github.com/baz/qux v0.4.0 => github.com/baz/qux v0.5.0

retract v1.0.0
retract [v1.1.0, v1.2.0] // superseded
`
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if f.Module != "example.com/app" {
		t.Errorf("module = %q", f.Module)
	}
	if f.Lang != "1.0" {
		t.Errorf("zyx = %q", f.Lang)
	}
	if len(f.Require) != 4 {
		t.Fatalf("require = %d, want 4", len(f.Require))
	}
	if f.Require[1].Path != "github.com/baz/qux" || f.Require[1].Version != "v0.4.0" {
		t.Errorf("second require = %+v", f.Require[1])
	}
	if f.Require[3].Version != "v1.0.0-20260101120000-abcdef123456" {
		t.Errorf("pseudo-version = %q", f.Require[3].Version)
	}
	if len(f.Exclude) != 1 || f.Exclude[0].Path != "github.com/bad/idea" {
		t.Errorf("exclude = %+v", f.Exclude)
	}
	if len(f.Replace) != 2 {
		t.Fatalf("replace = %d, want 2", len(f.Replace))
	}
	if f.Replace[0].NewPath != "../bar" || f.Replace[0].Version != "" {
		t.Errorf("first replace = %+v", f.Replace[0])
	}
	if f.Replace[1].NewVersion != "v0.5.0" {
		t.Errorf("second replace = %+v", f.Replace[1])
	}
	if len(f.Retract) != 2 {
		t.Fatalf("retract = %d, want 2", len(f.Retract))
	}
	if f.Retract[1].Low != "v1.1.0" || f.Retract[1].High != "v1.2.0" {
		t.Errorf("range retract = %+v", f.Retract[1])
	}
	if f.Retract[1].Rationale != "superseded" {
		t.Errorf("rationale = %q, want superseded", f.Retract[1].Rationale)
	}
}

func TestParseRejectsMalformedManifests(t *testing.T) {
	cases := map[string]string{
		"unknown directive":  "module example.com/app\nbogus x\n",
		"missing version":    "module example.com/app\nrequire github.com/foo/bar\n",
		"bad version":        "module example.com/app\nrequire github.com/foo/bar 1.2.3\n",
		"unclosed block":     "module example.com/app\nrequire (\n\tgithub.com/foo/bar v1.2.3\n",
		"replace without =>": "module example.com/app\nreplace github.com/foo/bar\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(src)); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestFormatIsCanonical(t *testing.T) {
	f := &File{
		Module: "example.com/app",
		Lang:   "1.0",
		Require: []Requirement{
			{Path: "github.com/z/z", Version: "v1.0.0"},
			{Path: "github.com/a/a", Version: "v1.0.0"},
		},
		Exclude: []Module{{Path: "github.com/m/m", Version: "v1.0.0"}},
		Replace: []Replacement{{Path: "github.com/b/b", NewPath: "../b"}},
		Retract: []Retraction{{Low: "v1.0.0", High: "v1.0.0"}},
	}
	want := `module example.com/app
zyx 1.0
require github.com/a/a v1.0.0
require github.com/z/z v1.0.0
exclude github.com/m/m v1.0.0
replace github.com/b/b => ../b
retract v1.0.0
`
	if got := string(f.Format()); got != want {
		t.Errorf("format =\n%s\nwant\n%s", got, want)
	}
}

func TestCheckLang(t *testing.T) {
	cases := []struct {
		need    string
		wantErr bool
	}{
		{"0.1", false},
		{"0.0", false},
		{"0.1.0", false},
		{"1.0", true},
		{"0.2", true},
	}
	for _, c := range cases {
		err := CheckLang(&File{Lang: c.need}, "0.1.0")
		if (err != nil) != c.wantErr {
			t.Errorf("CheckLang(%q) error = %v, want error %v", c.need, err, c.wantErr)
		}
	}
	if err := CheckLang(&File{}, "0.1.0"); err != nil {
		t.Errorf("a manifest with no zyx directive was refused: %v", err)
	}
	if err := CheckLang(&File{Lang: "latest"}, "0.1.0"); err == nil {
		t.Error("a manifest with an unreadable version was accepted")
	}
}

func TestSumRoundTrips(t *testing.T) {
	text := "example.com/one v1.0.0 h1:AAA=\nexample.com/one v1.0.0/zyx.mod h1:BBB=\n"
	s, err := ParseSum([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Content("example.com/one", "v1.0.0"); got != "h1:AAA=" {
		t.Errorf("content = %q, want h1:AAA=", got)
	}
	if got := s.Manifest("example.com/one", "v1.0.0"); got != "h1:BBB=" {
		t.Errorf("manifest = %q, want h1:BBB=", got)
	}
	if got := string(s.Format()); got != text {
		t.Errorf("format =\n%s\nwant\n%s", got, text)
	}
}

func TestSumOrdersItsLines(t *testing.T) {
	s, err := ParseSum([]byte("example.com/two v1.0.0 h1:B=\nexample.com/one v1.0.0 h1:A=\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := "example.com/one v1.0.0 h1:A=\nexample.com/two v1.0.0 h1:B=\n"
	if got := string(s.Format()); got != want {
		t.Errorf("format =\n%s\nwant\n%s", got, want)
	}
}

func TestSumRejectsMalformedLines(t *testing.T) {
	for _, text := range []string{
		"example.com/one v1.0.0\n",
		"example.com/one v1.0.0 md5:AAA=\n",
		"example.com/one h1:AAA=\n",
	} {
		if _, err := ParseSum([]byte(text)); err == nil {
			t.Errorf("a checksum file holding %q was accepted", text)
		}
	}
	// A comment and a blank line carry nothing.
	if _, err := ParseSum([]byte("// a note\n\n")); err != nil {
		t.Errorf("a comment was refused: %v", err)
	}
}

func TestSumAddRemovePrune(t *testing.T) {
	s := &Sum{}
	s.Add("example.com/one", "v1.0.0", "h1:AAA=", "h1:BBB=")
	s.Add("example.com/two", "v1.2.3", "h1:CCC=", "h1:DDD=")
	if s.Content("example.com/two", "v1.2.3") != "h1:CCC=" {
		t.Fatal("the second module was not recorded")
	}
	// Recording the same module version again replaces what was there.
	s.Add("example.com/one", "v1.0.0", "h1:EEE=", "h1:FFF=")
	if got := s.Content("example.com/one", "v1.0.0"); got != "h1:EEE=" {
		t.Errorf("content after a second add = %q, want h1:EEE=", got)
	}
	if got := s.Manifest("example.com/one", "v1.0.0"); got != "h1:FFF=" {
		t.Errorf("manifest after a second add = %q, want h1:FFF=", got)
	}

	s.Remove("example.com/one", "v1.0.0")
	if got := s.Content("example.com/one", "v1.0.0"); got != "" {
		t.Errorf("the removed module is still recorded: %q", got)
	}
	if got := s.Paths(); len(got) != 1 || got[0] != "example.com/two" {
		t.Errorf("paths after remove = %v, want the second module", got)
	}

	s.Remove("example.com/two", "v1.2.3")
	if !s.Empty() {
		t.Errorf("removing every module left %d lines", len(s.lines))
	}

	s.Add("example.com/one", "v1.0.0", "h1:AAA=", "h1:BBB=")
	s.Add("example.com/two", "v1.2.3", "h1:CCC=", "h1:DDD=")
	s.Prune(func(path string) bool { return path == "example.com/two" })
	if got := s.Paths(); len(got) != 1 || got[0] != "example.com/two" {
		t.Errorf("paths after prune = %v, want the second module", got)
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

func TestHashContent(t *testing.T) {
	one := zipOf(t, map[string]string{"m@v1/zyx.mod": "module m\n", "m@v1/a.zyx": "package a\n"})
	same := zipOf(t, map[string]string{"m@v1/a.zyx": "package a\n", "m@v1/zyx.mod": "module m\n"})
	other := zipOf(t, map[string]string{"m@v1/zyx.mod": "module m\n", "m@v1/a.zyx": "package b\n"})

	first, err := HashContent(one)
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashContent(same)
	if err != nil {
		t.Fatal(err)
	}
	third, err := HashContent(other)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("the order the files were added in changed the hash")
	}
	if first == third {
		t.Error("a changed file did not change the hash")
	}
	if !strings.HasPrefix(first, "h1:") {
		t.Errorf("hash = %q, want the h1 form", first)
	}
	if _, err := HashContent([]byte("not a zip")); err == nil {
		t.Error("content that is not a zip was accepted")
	}
}

func TestHashManifest(t *testing.T) {
	one := HashManifest("example.com/one", "v1.0.0", []byte("module example.com/one\n"))
	same := HashManifest("example.com/one", "v1.0.0", []byte("module example.com/one\n"))
	other := HashManifest("example.com/two", "v1.0.0", []byte("module example.com/one\n"))
	if one != same {
		t.Error("the same manifest hashed differently")
	}
	if one == other {
		t.Error("a different module path did not change the hash")
	}
}

func TestModuleCachePutAndRead(t *testing.T) {
	c, err := OpenModuleCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	archive := zipOf(t, map[string]string{
		"example.com/dep@v1.0.0/zyx.mod":       "module example.com/dep\n\nzyx 0.1\n",
		"example.com/dep@v1.0.0/util/util.zyx": "package util\n",
	})
	content, err := HashContent(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Put("example.com/dep", "v1.0.0", content, archive); err != nil {
		t.Fatal(err)
	}
	if !c.Has("example.com/dep", "v1.0.0") {
		t.Fatal("the module is not in the cache after a put")
	}
	f, err := c.Manifest("example.com/dep", "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if f.Module != "example.com/dep" {
		t.Errorf("module = %q, want example.com/dep", f.Module)
	}
	tree := filepath.Join(c.Dir("example.com/dep", "v1.0.0"), "util", "util.zyx")
	if _, err := os.Stat(tree); err != nil {
		t.Errorf("the tree was not unpacked: %v", err)
	}
	hash, err := c.ManifestHash("example.com/dep", "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if hash != HashManifest("example.com/dep", "v1.0.0", []byte("module example.com/dep\n\nzyx 0.1\n")) {
		t.Errorf("manifest hash = %q, want the hash of what is stored", hash)
	}
}

func TestModuleCacheRejectsAWrongHash(t *testing.T) {
	c, err := OpenModuleCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	archive := zipOf(t, map[string]string{"example.com/dep@v1.0.0/zyx.mod": "module example.com/dep\n"})
	if err := c.Put("example.com/dep", "v1.0.0", "h1:wrong=", archive); err == nil {
		t.Fatal("content that does not match the recorded hash was stored")
	}
	if c.Has("example.com/dep", "v1.0.0") {
		t.Error("a refused module reached the cache")
	}
}

func TestModuleCacheRejectsANameOutsideTheModule(t *testing.T) {
	c, err := OpenModuleCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	archive := zipOf(t, map[string]string{
		"example.com/dep@v1.0.0/zyx.mod":       "module example.com/dep\n",
		"example.com/dep@v1.0.0/../escape.zyx": "package escape\n",
	})
	if err := c.Put("example.com/dep", "v1.0.0", "", archive); err == nil {
		t.Fatal("an archive that climbs out of the module was stored")
	}
	if _, err := os.Stat(filepath.Join(c.Root(), "escape.zyx")); err == nil {
		t.Error("a file was written outside the module directory")
	}
}

func TestModuleCacheRejectsAnArchiveWithoutAManifest(t *testing.T) {
	c, err := OpenModuleCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	archive := zipOf(t, map[string]string{"example.com/dep@v1.0.0/a.zyx": "package a\n"})
	if err := c.Put("example.com/dep", "v1.0.0", "", archive); err == nil {
		t.Fatal("an archive with no manifest was stored")
	}
}

func TestEscapePath(t *testing.T) {
	// Two paths that differ only in case stay two directories on a file
	// system that does not tell them apart.
	one := escapePath("example.com/Foo/Bar")
	two := escapePath("example.com/foo/bar")
	if one == two {
		t.Errorf("the two paths share the directory %q", one)
	}
	if want := "example.com/!foo/!bar"; one != want {
		t.Errorf("escapePath = %q, want %q", one, want)
	}
}

// putModule stores a module in a cache the way a fetch would, so tests can
// build a dependency graph without the network.
func putModule(t *testing.T, c *ModuleCache, path, version, manifest string) {
	t.Helper()
	archive := zipOf(t, map[string]string{path + "@" + version + "/zyx.mod": manifest})
	content, err := HashContent(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Put(path, version, content, archive); err != nil {
		t.Fatal(err)
	}
}

func TestBuildListClosesOverTheGraph(t *testing.T) {
	c, err := OpenModuleCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	putModule(t, c, "example.com/one", "v1.0.0",
		"module example.com/one\n\nzyx 0.1\n\nrequire example.com/two v1.2.0\n")
	putModule(t, c, "example.com/two", "v1.2.0", "module example.com/two\n\nzyx 0.1\n")
	putModule(t, c, "example.com/two", "v1.4.0", "module example.com/two\n\nzyx 0.1\n")

	main := &File{
		Module: "example.com/app",
		Require: []Requirement{
			{Path: "example.com/one", Version: "v1.0.0"},
			{Path: "example.com/two", Version: "v1.4.0"},
		},
	}
	list, err := BuildList(main, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Minimal version selection takes the higher of the two versions the graph
	// reaches, and the module only the dependency asked for.
	want := map[string]string{"example.com/one": "v1.0.0", "example.com/two": "v1.4.0"}
	if len(list) != len(want) {
		t.Fatalf("build list = %+v, want %d modules", list, len(want))
	}
	for _, m := range list {
		if want[m.Path] != m.Version {
			t.Errorf("%s is %s, want %s", m.Path, m.Version, want[m.Path])
		}
	}

	// An import path belongs to the longest module that is a prefix of it.
	dep, ok := ModuleOf(list, "example.com/two/util")
	if !ok || dep.Path != "example.com/two" {
		t.Errorf("ModuleOf = %+v %v, want example.com/two", dep, ok)
	}
	if _, ok := ModuleOf(list, "example.com/other"); ok {
		t.Error("an unrelated path resolved to a module")
	}
}

func TestBuildListStopsWhenAModuleIsMissing(t *testing.T) {
	c, err := OpenModuleCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main := &File{Module: "example.com/app", Require: []Requirement{{Path: "example.com/gone", Version: "v1.0.0"}}}
	if _, err := BuildList(main, c, nil); err == nil {
		t.Error("selection succeeded with a module the cache does not hold")
	}
}

// proxyServer serves the files of a module over the proxy protocol, so a
// fetch can be tested without the network.
func proxyServer(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
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

// moduleFixture is one module version as a proxy serves it.
type moduleFixture struct {
	manifest string
	archive  []byte
	content  string
	sum      string
}

// newModuleFixture builds a module version and the hashes a checksum file
// would record for it.
func newModuleFixture(t *testing.T, path, version string) moduleFixture {
	t.Helper()
	manifest := "module " + path + "\n\nzyx 0.1\n"
	archive := zipOf(t, map[string]string{
		path + "@" + version + "/zyx.mod":       manifest,
		path + "@" + version + "/util/util.zyx": "package util\n\nfunc Use() {\n}\n",
	})
	content, err := HashContent(archive)
	if err != nil {
		t.Fatal(err)
	}
	return moduleFixture{
		manifest: manifest,
		archive:  archive,
		content:  content,
		sum:      HashManifest(path, version, []byte(manifest)),
	}
}

// serve returns the protocol paths a proxy serves for a fixture.
func (f moduleFixture) serve(path, version string) map[string][]byte {
	return map[string][]byte{
		path + "/@v/list":                 []byte(version + "\n"),
		path + "/@v/" + version + ".info": []byte(`{"Version":"` + version + `"}`),
		path + "/@v/" + version + ".mod":  []byte(f.manifest),
		path + "/@v/" + version + ".zip":  f.archive,
	}
}

func TestFetchIntoStoresAModule(t *testing.T) {
	fixture := newModuleFixture(t, "example.com/dep", "v1.0.0")
	server := proxyServer(t, fixture.serve("example.com/dep", "v1.0.0"))

	cache, err := OpenModuleCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sum := &Sum{}
	sum.Add("example.com/dep", "v1.0.0", fixture.content, fixture.sum)
	sources, err := ParseSources(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := &Fetcher{Sources: sources}
	if err := fetcher.FetchInto(cache, sum, "example.com/dep", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if !cache.Has("example.com/dep", "v1.0.0") {
		t.Fatal("the module is not in the cache after a fetch")
	}
	tree := filepath.Join(cache.Dir("example.com/dep", "v1.0.0"), "util", "util.zyx")
	if _, err := os.Stat(tree); err != nil {
		t.Errorf("the tree was not unpacked: %v", err)
	}

	// The proxy client reads the same paths on its own.
	proxy := &Proxy{Base: server.URL}
	versions, err := proxy.Versions("example.com/dep")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0] != "v1.0.0" {
		t.Errorf("versions = %v, want v1.0.0", versions)
	}
	info, err := proxy.InfoOf("example.com/dep", "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "v1.0.0" {
		t.Errorf("info version = %q, want v1.0.0", info.Version)
	}
}

func TestFetchIntoRefusesWhatTheSumDoesNotFix(t *testing.T) {
	fixture := newModuleFixture(t, "example.com/dep", "v1.0.0")
	server := proxyServer(t, fixture.serve("example.com/dep", "v1.0.0"))
	cache, err := OpenModuleCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sources, err := ParseSources(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := &Fetcher{Sources: sources}
	// A module no checksum file records is refused rather than trusted.
	if err := fetcher.FetchInto(cache, &Sum{}, "example.com/dep", "v1.0.0"); err == nil {
		t.Fatal("a module with no recorded hash was fetched")
	}
	// A recorded hash the proxy does not match is refused too.
	wrong := &Sum{}
	wrong.Add("example.com/dep", "v1.0.0", "h1:wrong=", fixture.sum)
	if err := fetcher.FetchInto(cache, wrong, "example.com/dep", "v1.0.0"); err == nil {
		t.Fatal("a module whose hash did not match was stored")
	}
	if cache.Has("example.com/dep", "v1.0.0") {
		t.Error("a refused module reached the cache")
	}
}

func TestFetchIntoMovesOnPastAMissingModule(t *testing.T) {
	fixture := newModuleFixture(t, "example.com/dep", "v1.0.0")
	empty := proxyServer(t, map[string][]byte{})
	serving := proxyServer(t, fixture.serve("example.com/dep", "v1.0.0"))

	cache, err := OpenModuleCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sum := &Sum{}
	sum.Add("example.com/dep", "v1.0.0", fixture.content, fixture.sum)
	// A comma moves on when a source answers that it does not have the
	// module, which is what a fallback proxy is for.
	sources, err := ParseSources(empty.URL + "," + serving.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&Fetcher{Sources: sources}).FetchInto(cache, sum, "example.com/dep", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if !cache.Has("example.com/dep", "v1.0.0") {
		t.Error("the second source was not reached")
	}
}

func TestFetchIntoRefusesAPrivateModule(t *testing.T) {
	fixture := newModuleFixture(t, "example.com/dep", "v1.0.0")
	server := proxyServer(t, fixture.serve("example.com/dep", "v1.0.0"))
	cache, err := OpenModuleCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sum := &Sum{}
	sum.Add("example.com/dep", "v1.0.0", fixture.content, fixture.sum)
	sources, err := ParseSources(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := &Fetcher{Sources: sources, Private: []string{"example.com"}}
	if err := fetcher.FetchInto(cache, sum, "example.com/dep", "v1.0.0"); err == nil {
		t.Fatal("a private module was fetched from a proxy")
	}
	off, err := ParseSources("off")
	if err != nil {
		t.Fatal(err)
	}
	if err := (&Fetcher{Sources: off}).FetchInto(cache, sum, "example.com/dep", "v1.0.0"); err == nil {
		t.Fatal("a source list of off fetched a module")
	}
}

func TestParseSources(t *testing.T) {
	sources, err := ParseSources("https://one.example,https://two.example|direct")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 3 {
		t.Fatalf("sources = %+v, want three", sources)
	}
	if sources[0].Proxy != "https://one.example" || sources[0].FallbackOnAny {
		t.Errorf("first source = %+v", sources[0])
	}
	if !sources[1].FallbackOnAny {
		t.Errorf("second source = %+v, want a pipe after it", sources[1])
	}
	if !sources[2].Direct {
		t.Errorf("third source = %+v, want direct", sources[2])
	}
	if _, err := ParseSources("not-a-url"); err == nil {
		t.Error("an entry that is neither a URL nor a keyword was accepted")
	}
	if got, err := ParseSources(""); err != nil || got != nil {
		t.Errorf("an empty list = %v %v, want nothing", got, err)
	}
}

func TestMatchPatterns(t *testing.T) {
	patterns := []string{"example.com/private", "*.corp.example.com"}
	for _, path := range []string{"example.com/private", "example.com/private/sub", "git.corp.example.com/x"} {
		if !MatchPatterns(patterns, path) {
			t.Errorf("%q did not match", path)
		}
	}
	for _, path := range []string{"example.com/public", "example.com/privatex"} {
		if MatchPatterns(patterns, path) {
			t.Errorf("%q matched and should not have", path)
		}
	}
}

func TestModulesTxtRoundTrips(t *testing.T) {
	modules := []VendoredModule{
		{Module: Module{Path: "example.com/dep", Version: "v1.0.0"}, Explicit: true, Lang: "0.1",
			Packages: []string{"example.com/dep/util", "example.com/dep/util/sub"}},
		{Module: Module{Path: "example.com/other", Version: "v2.0.0"}},
	}
	text := string(FormatModulesTxt(modules))
	want := "# example.com/dep v1.0.0\n## explicit; zyx 0.1\nexample.com/dep/util\nexample.com/dep/util/sub\n" +
		"# example.com/other v2.0.0\n"
	if text != want {
		t.Fatalf("modules.txt =\n%s\nwant\n%s", text, want)
	}
	parsed, err := ParseModulesTxt([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 2 {
		t.Fatalf("parsed %d modules, want 2", len(parsed))
	}
	if !parsed[0].Explicit || parsed[0].Lang != "0.1" || len(parsed[0].Packages) != 2 {
		t.Errorf("first module = %+v", parsed[0])
	}
	if parsed[1].Explicit || parsed[1].Version != "v2.0.0" || len(parsed[1].Packages) != 0 {
		t.Errorf("second module = %+v", parsed[1])
	}
	// The text is stable, so a mirror can be written again without a diff.
	if again := string(FormatModulesTxt(parsed)); again != text {
		t.Errorf("a second format =\n%s\nwant\n%s", again, text)
	}
}

func TestParseModulesTxtRejectsMalformedText(t *testing.T) {
	for _, text := range []string{
		"## explicit\n",
		"example.com/dep/util\n",
		"# example.com/dep\n",
	} {
		if _, err := ParseModulesTxt([]byte(text)); err == nil {
			t.Errorf("modules.txt holding %q was accepted", text)
		}
	}
}

func TestHashTreeMatchesTheArchive(t *testing.T) {
	// A module fetched from a repository and the same module fetched from a
	// proxy must hash alike, or a checksum file could not record one hash for
	// both and switching a source would look like tampering.
	files := map[string]string{
		"zyx.mod":       "module example.com/dep\n\nzyx 0.1\n",
		"util/util.zyx": "package util\n\nfunc Use() {\n}\n",
	}
	archive := zipOf(t, map[string]string{
		"example.com/dep@v1.0.0/zyx.mod":       files["zyx.mod"],
		"example.com/dep@v1.0.0/util/util.zyx": files["util/util.zyx"],
	})
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A repository keeps its history beside the files, which is not part of
	// the module and must not enter the hash.
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fromArchive, err := HashContent(archive)
	if err != nil {
		t.Fatal(err)
	}
	fromTree, err := HashTree("example.com/dep", "v1.0.0", dir)
	if err != nil {
		t.Fatal(err)
	}
	if fromArchive != fromTree {
		t.Errorf("the archive hashes to %s and the tree to %s", fromArchive, fromTree)
	}
}

func TestPutTreeStoresAModule(t *testing.T) {
	cache, err := OpenModuleCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zyx.mod"), []byte("module example.com/dep\n\nzyx 0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content, err := HashTree("example.com/dep", "v1.0.0", dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.PutTree("example.com/dep", "v1.0.0", content, dir); err != nil {
		t.Fatal(err)
	}
	if !cache.Has("example.com/dep", "v1.0.0") {
		t.Fatal("the module is not in the cache after a put")
	}
	// A tree that does not match the recorded hash is refused, the same way an
	// archive is.
	if err := cache.PutTree("example.com/dep", "v1.0.1", "h1:wrong=", dir); err == nil {
		t.Error("a tree that does not match the recorded hash was stored")
	}
	if cache.Has("example.com/dep", "v1.0.1") {
		t.Error("a refused tree reached the cache")
	}
}

func TestImportMeta(t *testing.T) {
	page := `<html><head>` +
		`<meta name="zyx-import" content="example.com/dep git https://example.com/dep.git">` +
		`</head></html>`
	repo, ok := importMeta(page, "example.com/dep")
	if !ok || repo != "https://example.com/dep.git" {
		t.Errorf("importMeta = %q %v, want the repository", repo, ok)
	}
	if _, ok := importMeta(page, "example.com/other"); ok {
		t.Error("a page answered for a module it does not serve")
	}
	if _, ok := importMeta("<html></html>", "example.com/dep"); ok {
		t.Error("a page with no meta tag answered")
	}
	if _, ok := importMeta(`<meta name="zyx-import" content="example.com/dep svn https://x">`, "example.com/dep"); ok {
		t.Error("a repository this build cannot read answered")
	}
}

func TestNoteRoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	db := &SumDB{Name: "sum.example.com"}
	db.Key, db.KeyHash = pub, keyHash("sum.example.com", pub)

	text := formatTreeHead(treeHead{Size: 3, Hash: tlogEmpty()})
	note := SignNote(text, db, priv)
	got, err := VerifyNote(note, db)
	if err != nil {
		t.Fatal(err)
	}
	if got != text {
		t.Errorf("verified text = %q, want %q", got, text)
	}

	// The signature covers the text and nothing else, so a changed text does
	// not verify.
	tampered := bytes.Replace(note, []byte("\n3\n"), []byte("\n4\n"), 1)
	if _, err := VerifyNote(tampered, db); err == nil {
		t.Error("a tampered note verified")
	}
	// A note signed by another key does not verify against this one.
	otherKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	other := &SumDB{Name: "sum.example.com"}
	other.Key, other.KeyHash = otherKey, keyHash("sum.example.com", otherKey)
	if _, err := VerifyNote(note, other); err == nil {
		t.Error("a note verified against a key that did not sign it")
	}
	// A message with no signature block is not a note.
	if _, err := VerifyNote([]byte("a message\n"), db); err == nil {
		t.Error("a message with no signature verified")
	}
}

func TestKeyRoundTrips(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded := EncodeKey("sum.example.com", pub)
	key, hash, err := ParseKey("sum.example.com", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key, pub) {
		t.Error("the key did not survive the round trip")
	}
	if !bytes.Equal(hash, keyHash("sum.example.com", pub)) {
		t.Error("the key hash did not survive the round trip")
	}
	// The name is part of the key, so a key does not verify under another
	// name.
	if _, _, err := ParseKey("other.example.com", encoded); err == nil {
		t.Error("a key was accepted under a name it was not made for")
	}
}

func TestParseSumDB(t *testing.T) {
	if db, err := ParseSumDB("off"); err != nil || db != nil {
		t.Errorf("off = %v %v, want nothing", db, err)
	}
	if _, err := ParseSumDB("sum.example.com"); err == nil {
		t.Error("a database with no public key was accepted")
	}
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	setting := "sum.example.com+" + EncodeKey("sum.example.com", pub) + " https://sum.example.com"
	db, err := ParseSumDB(setting)
	if err != nil {
		t.Fatal(err)
	}
	if db.Name != "sum.example.com" || db.Base != "https://sum.example.com" || !bytes.Equal(db.Key, pub) {
		t.Errorf("parsed = %+v", db)
	}
	// A database named without a URL is reached at its own name.
	plain, err := ParseSumDB("sum.example.com+" + EncodeKey("sum.example.com", pub))
	if err != nil {
		t.Fatal(err)
	}
	if plain.Base != "https://sum.example.com" {
		t.Errorf("base = %q, want the database's own name", plain.Base)
	}
}

func TestTreeHeadRoundTrips(t *testing.T) {
	head := treeHead{Size: 12345, Hash: tlogEmpty()}
	parsed, err := parseTreeHead(formatTreeHead(head))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Size != head.Size || !bytes.Equal(parsed.Hash, head.Hash) {
		t.Errorf("parsed = %+v, want %+v", parsed, head)
	}
	if _, err := parseTreeHead("go.sum database tree\n"); err == nil {
		t.Error("a description with no size was accepted")
	}
}

func TestLogHashesFollowTheLogRules(t *testing.T) {
	// The two hashes every transparency log starts from, which a reader can
	// check against any other implementation of the same rules.
	if got := fmt.Sprintf("%x", tlogEmpty()); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("an empty log hashes to %s", got)
	}
	if got := fmt.Sprintf("%x", tlogLeaf(nil)); got != "6e340b9cffb37a989ca544e6bb780a2c78901d3fb33738768511a30617afa01d" {
		t.Errorf("an empty record hashes to %s", got)
	}
	// A record and an interior node are told apart by the byte in front of
	// them, so no record can be made to stand in for a node.
	if bytes.Equal(tlogLeaf([]byte("x")), tlogNode(tlogEmpty(), tlogEmpty())) {
		t.Error("a record and a node hash alike")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0.0", "v1.0.1", -1},
		{"v1.2.0", "v1.1.9", 1},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.0.0", "v1.0.0-rc1", 1},
		{"v1.0.0-alpha", "v1.0.0-beta", -1},
		{"v1.0.0+build", "v1.0.0", 0},
	}
	for _, c := range cases {
		got := Compare(c.a, c.b)
		if (got < 0) != (c.want < 0) || (got > 0) != (c.want > 0) {
			t.Errorf("Compare(%q, %q) = %d, want sign %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParseVersionRejectsMalformed(t *testing.T) {
	for _, s := range []string{"1.0.0", "v1.0", "v1.0.0.0", "vx.0.0", "v-1.0.0"} {
		if _, err := ParseVersion(s); err == nil {
			t.Errorf("%q must not parse", s)
		}
	}
}

// testGraph is a fixed dependency graph for the selection tests.
type testGraph map[string][]Requirement

func (g testGraph) Requirements(path, version string) ([]Requirement, error) {
	return g[path+"@"+version], nil
}

func TestSelectPicksTheHighestRequiredVersion(t *testing.T) {
	main := &File{
		Module: "example.com/app",
		Require: []Requirement{
			{Path: "example.com/a", Version: "v1.0.0"},
			{Path: "example.com/b", Version: "v1.0.0"},
		},
	}
	graph := testGraph{
		"example.com/a@v1.0.0": {{Path: "example.com/c", Version: "v1.2.0"}},
		"example.com/b@v1.0.0": {{Path: "example.com/c", Version: "v1.1.0"}},
	}
	list, err := Select(main, graph)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range list {
		got[m.Path] = m.Version
	}
	if got["example.com/c"] != "v1.2.0" {
		t.Errorf("c = %q, want v1.2.0", got["example.com/c"])
	}
	if len(list) != 3 {
		t.Errorf("build list = %d modules, want 3", len(list))
	}
	// The list is sorted by path.
	for i := 1; i < len(list); i++ {
		if list[i-1].Path >= list[i].Path {
			t.Fatalf("build list is not sorted: %v", list)
		}
	}
}

func TestSelectAppliesReplacements(t *testing.T) {
	// A replacement that names a path in the working tree keeps the module's
	// own path, because that is what programs import, and carries the
	// directory instead.
	main := &File{
		Module:  "example.com/app",
		Require: []Requirement{{Path: "example.com/a", Version: "v1.0.0"}},
		Replace: []Replacement{{Path: "example.com/a", NewPath: "../local/a"}},
	}
	list, err := Select(main, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Path != "example.com/a" || list[0].Dir != "../local/a" {
		t.Errorf("build list = %+v, want example.com/a from ../local/a", list)
	}

	// A replacement that names another module changes the path, because the
	// other module is what the build reads.
	other := &File{
		Module:  "example.com/app",
		Require: []Requirement{{Path: "example.com/a", Version: "v1.0.0"}},
		Replace: []Replacement{{Path: "example.com/a", NewPath: "example.com/b", NewVersion: "v2.0.0"}},
	}
	list, err = Select(other, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Path != "example.com/b" || list[0].Version != "v2.0.0" {
		t.Errorf("build list = %+v, want example.com/b v2.0.0", list)
	}
}

func TestLocalPath(t *testing.T) {
	local := []string{".", "..", "./a", "../a", t.TempDir()}
	for _, path := range local {
		if !LocalPath(path) {
			t.Errorf("%q is a path in the working tree and was not read as one", path)
		}
	}
	for _, path := range []string{"example.com/a", "github.com/foo/bar"} {
		if LocalPath(path) {
			t.Errorf("%q is a module path and was read as a directory", path)
		}
	}
}

func TestSelectRejectsAnExcludedVersion(t *testing.T) {
	main := &File{
		Module:  "example.com/app",
		Require: []Requirement{{Path: "example.com/a", Version: "v1.0.0"}},
		Exclude: []Module{{Path: "example.com/a", Version: "v1.0.0"}},
	}
	if _, err := Select(main, nil); err == nil {
		t.Fatal("an excluded version must be reported")
	} else if !strings.Contains(err.Error(), "excluded") {
		t.Errorf("error = %v, want an exclusion", err)
	}
}
