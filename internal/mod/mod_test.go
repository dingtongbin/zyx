package mod

import (
	"archive/zip"
	"bytes"
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
	main := &File{
		Module:  "example.com/app",
		Require: []Requirement{{Path: "example.com/a", Version: "v1.0.0"}},
		Replace: []Replacement{{Path: "example.com/a", NewPath: "../local/a"}},
	}
	list, err := Select(main, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Path != "../local/a" {
		t.Errorf("build list = %+v, want the replacement", list)
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
