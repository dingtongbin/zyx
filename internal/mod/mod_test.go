package mod

import (
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
