package toolchain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindToolPrefersTheCandidateDirectories(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "clang.exe")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findTool("clang", []string{dir}); got != fake {
		t.Errorf("findTool = %q, want %q", got, fake)
	}
	if got := findTool("definitely-not-a-program", []string{dir}); got != "" {
		t.Errorf("findTool = %q, want nothing", got)
	}
}

func TestCandidateDirsStartWithTheEnvironment(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	t.Setenv("ZYX_LLVM_BIN", first+string(os.PathListSeparator)+second)
	dirs := candidateDirs()
	if len(dirs) < 2 {
		t.Fatalf("candidate directories = %v", dirs)
	}
	if dirs[0] != first || dirs[1] != second {
		t.Errorf("candidate directories = %v, want the environment first", dirs)
	}
}

func TestCandidateDirsLookBesideTheCompiler(t *testing.T) {
	t.Setenv("ZYX_LLVM_BIN", "")
	exe, err := os.Executable()
	if err != nil {
		t.Skip("the test binary has no path")
	}
	want := filepath.Join(filepath.Dir(exe), "toolchain", "bin")
	for _, dir := range candidateDirs() {
		if dir == want {
			return
		}
	}
	t.Errorf("the bundled toolchain directory %q is not searched", want)
}

func TestLocateFindsTheToolchain(t *testing.T) {
	tc, err := Locate()
	if err != nil {
		t.Skipf("no toolchain on this machine: %v", err)
	}
	if tc.Clang == "" && tc.LLC == "" {
		t.Error("a located toolchain compiles LLVM IR with one of clang or llc")
	}
	if tc.CC == "" {
		t.Error("a located toolchain has a C compiler")
	}
	if _, err := os.Stat(filepath.Join(tc.Runtime, "include", "zyxrt.h")); err != nil {
		t.Errorf("the runtime header is missing: %v", err)
	}
}

func TestSourcesNameTheRuntimeUnits(t *testing.T) {
	tc, err := Locate()
	if err != nil {
		t.Skipf("no runtime tree on this machine: %v", err)
	}
	target, err := ParseTarget("amd64-unknown-linux-gnu")
	if err != nil {
		t.Fatal(err)
	}
	sources := tc.Sources(target)
	if len(sources) == 0 {
		t.Fatal("a build needs at least one runtime unit")
	}
	var names []string
	for _, src := range sources {
		names = append(names, filepath.Base(src))
		if _, err := os.Stat(src); err != nil {
			t.Errorf("%s is missing: %v", src, err)
		}
	}
	joined := strings.Join(names, " ")
	for _, want := range []string{"start.c", "panic.c", "abi.c", "region.c", "sched.c", "chan.c", "linux.c"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the runtime units are missing %s: %v", want, names)
		}
	}
}

func TestSourcesFallBackToAPortablePlatform(t *testing.T) {
	tc, err := Locate()
	if err != nil {
		t.Skipf("no runtime tree on this machine: %v", err)
	}
	target, err := ParseTarget("amd64-unknown-linux-gnu")
	if err != nil {
		t.Fatal(err)
	}
	// A platform the runtime does not name yet must still compile.
	target.OS = "plan9"
	sources := tc.Sources(target)
	last := sources[len(sources)-1]
	if filepath.Base(last) != "linux.c" {
		t.Errorf("the platform unit is %q, want a portable fallback", last)
	}
}

func TestIncludeFlagsNameTheRuntimeHeaders(t *testing.T) {
	tc, err := Locate()
	if err != nil {
		t.Skipf("no runtime tree on this machine: %v", err)
	}
	flags := tc.IncludeFlags()
	if len(flags) == 0 {
		t.Fatal("the runtime headers need a search path")
	}
	for _, flag := range flags {
		if !strings.HasPrefix(flag, "-I") {
			t.Errorf("include flag %q does not start with -I", flag)
		}
		if _, err := os.Stat(strings.TrimPrefix(flag, "-I")); err != nil {
			t.Errorf("%s is missing: %v", flag, err)
		}
	}
}

func TestCompileIRCoversBothPaths(t *testing.T) {
	tc, err := Locate()
	if err != nil {
		t.Skipf("no toolchain on this machine: %v", err)
	}
	ir := []byte("define i32 @main() {\n  ret i32 0\n}\n")
	dir := t.TempDir()
	cases := []struct {
		name  string
		clear func(*Toolchain)
		skip  bool
	}{
		{"clang", func(c *Toolchain) { c.LLC = "" }, tc.Clang == ""},
		{"llc", func(c *Toolchain) { c.Clang = "" }, tc.LLC == ""},
	}
	for _, c := range cases {
		if c.skip {
			continue
		}
		clone := *tc
		c.clear(&clone)
		out := filepath.Join(dir, c.name+".o")
		opts := CompileOptions{Target: HostTarget(), Level: "-O2"}
		if err := clone.CompileIR(ir, out, opts); err != nil {
			t.Errorf("compiling with %s: %v", c.name, err)
			continue
		}
		info, err := os.Stat(out)
		if err != nil || info.Size() == 0 {
			t.Errorf("%s produced no object: %v", c.name, err)
		}
	}
}
