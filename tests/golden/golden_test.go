// Package golden holds the intermediate form and the LLVM IR of a few small
// programs as golden files, so a change to either shows up as a difference in
// a review rather than as a silent drift.
package golden

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"zyx/internal/cli"
	"zyx/internal/toolchain"
)

// update regenerates the golden files from what the compiler produces now.
// A difference is confirmed by a person before the files are written again.
var update = flag.Bool("update", false, "rewrite the golden files from the compiler's output")

// programs lists the programs the golden files are taken from, in byte order.
func programs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out
}

// env gives the compiler the standard library, the runtime, and a cache of
// its own, so a test needs no installation and leaves nothing behind.
func env(name string) string {
	switch name {
	case "ZYX_STD":
		return filepath.Join(repoRoot(), "std")
	case "ZYX_RUNTIME":
		return filepath.Join(repoRoot(), "runtime")
	case "ZYX_CACHE":
		return filepath.Join(repoRoot(), "build", "golden-cache")
	}
	return ""
}

// repoRoot walks up from the working directory to the module root.
func repoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("no go.mod above the working directory")
		}
		dir = parent
	}
}

// run invokes the command line in a directory and answers its output.
func run(dir string, args ...string) (stdout, stderr string, code int) {
	var out, errBuf bytes.Buffer
	s := &cli.Session{Stdout: &out, Stderr: &errBuf, Dir: dir, Env: env}
	code = s.Run(args)
	return out.String(), errBuf.String(), code
}

// emit returns what the compiler writes for a program, and the target the
// program was compiled for.
func emit(t *testing.T, dir, command string) (string, string) {
	t.Helper()
	stdout, stderr, code := run(dir, command, ".")
	if code != 0 {
		t.Fatalf("%s failed with %d:\n%s", command, code, stderr)
	}
	return stdout, goldenTarget(stdout)
}

// TestGoldenFiles checks the .zyxc and the LLVM IR of every program against
// the files the repository holds. The products name the target they were
// compiled for, so a golden file recorded for another target is skipped with
// the reason rather than reported as a difference: recording it again on this
// host is what `-update` does.
func TestGoldenFiles(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	for _, name := range programs(t) {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join("testdata", name)
			for _, product := range []struct{ command, suffix string }{
				{"emit-zyxc", ".zyxc"},
				{"emit-llvm", ".ll"},
			} {
				got, target := emit(t, dir, product.command)
				wantPath := filepath.Join("want", name+product.suffix)
				if *update {
					if err := os.MkdirAll(filepath.Dir(wantPath), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(wantPath, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(wantPath)
				if err != nil {
					t.Fatalf("no golden file for %s: %v (record it with -update)", name+product.suffix, err)
				}
				if recorded := goldenTarget(string(want)); recorded != "" && recorded != target {
					t.Skipf("%s was recorded for %s, and this host compiles for %s: record it again with -update",
						wantPath, recorded, target)
				}
				if got != string(want) {
					t.Errorf("%s differs from %s:\n--- got ---\n%s\n--- want ---\n%s",
						product.command, wantPath, got, want)
				}
			}
		})
	}
}

// goldenTarget returns the target a golden file was recorded for, or the empty
// string when the file does not name one. The intermediate form writes it as
// `target <triple>` and the IR as `target triple = "<triple>"`, both before
// anything else the file holds.
func goldenTarget(data string) string {
	for _, line := range strings.Split(data, "\n") {
		if rest, ok := strings.CutPrefix(line, "target "); ok {
			return strings.Trim(strings.TrimPrefix(rest, "triple = "), "\"")
		}
	}
	return ""
}
