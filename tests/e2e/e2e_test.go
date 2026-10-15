// Package e2e drives the whole compiler over a corpus of real programs:
// every program under testdata is checked, built, run, and compared against
// the output it declares.
package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"zyx/internal/cli"
	"zyx/internal/toolchain"
	"zyx/internal/zyxc"
)

// env gives the compiler the standard library and the runtime tree of the
// checkout, so a test needs no installation.
func env(name string) string {
	switch name {
	case "ZYX_STD":
		return filepath.Join(repoRoot(), "std")
	case "ZYX_RUNTIME":
		return filepath.Join(repoRoot(), "runtime")
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

// run invokes the command line in a directory and returns its output.
func run(dir string, args ...string) (stdout, stderr string, code int) {
	var out, errBuf bytes.Buffer
	s := &cli.Session{Stdout: &out, Stderr: &errBuf, Dir: dir, Env: env}
	code = s.Run(args)
	return out.String(), errBuf.String(), code
}

// copyTree copies a directory of sources into a temporary module.
func copyTree(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// programs lists the corpus directories, in byte order.
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

// TestProgramExitCode checks that the process exit code is the value the
// language main returns.
func TestProgramExitCode(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	dir := t.TempDir()
	files := map[string]string{
		"zyx.mod":  "module example.com/exitcode\n\nzyx 0.1\n",
		"main.zyx": "package main\n\nfunc main() -> i32 {\n\treturn 42\n}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, stderr, code := run(dir, "run", "./...")
	if code != 42 {
		t.Errorf("exit code = %d, want 42\n%s", code, stderr)
	}
}

// TestPrograms checks, builds, and runs every program of the corpus.
func TestPrograms(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	for _, name := range programs(t) {
		t.Run(name, func(t *testing.T) {
			dir := copyTree(t, filepath.Join("testdata", name))
			want, err := os.ReadFile(filepath.Join(dir, "want.txt"))
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, code := run(dir, "run", "./...")
			if code != 0 {
				t.Fatalf("run failed with %d:\n%s", code, stderr)
			}
			if stderr != "" {
				t.Errorf("the compiler wrote to stderr:\n%s", stderr)
			}
			if got := stdout; got != string(want) {
				t.Errorf("output =\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestCorpusChecks runs the front end over every program of the corpus, so
// that the checker sees the whole language even where the backend does not
// reach it yet.
func TestCorpusChecks(t *testing.T) {
	for _, name := range corpus(t) {
		t.Run(name, func(t *testing.T) {
			dir := copyTree(t, filepath.Join("corpus", name))
			_, stderr, code := run(dir, "check", "./...")
			if code != 0 {
				t.Fatalf("check failed with %d:\n%s", code, stderr)
			}
			if stderr != "" {
				t.Errorf("the compiler wrote to stderr:\n%s", stderr)
			}
		})
	}
}

// corpus lists the front-end corpus directories, in byte order.
func corpus(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("corpus")
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

// TestFormatIsIdempotent rewrites every program and rewrites the result
// again, which must leave the text alone.
func TestFormatIsIdempotent(t *testing.T) {
	for _, name := range programs(t) {
		t.Run(name, func(t *testing.T) {
			dir := copyTree(t, filepath.Join("testdata", name))
			if _, stderr, code := run(dir, "fmt", "./..."); code != 0 {
				t.Fatalf("fmt failed with %d:\n%s", code, stderr)
			}
			first := readSources(t, dir)
			if _, stderr, code := run(dir, "fmt", "./..."); code != 0 {
				t.Fatalf("the second fmt failed with %d:\n%s", code, stderr)
			}
			second := readSources(t, dir)
			if first != second {
				t.Errorf("fmt is not idempotent:\n--- first ---\n%s\n--- second ---\n%s", first, second)
			}
		})
	}
}

// readSources returns the text of every source of a module, in byte order.
func readSources(t *testing.T, dir string) string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".zyx") {
			names = append(names, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString("--- " + name + " ---\n")
		b.Write(data)
	}
	return b.String()
}

// TestEmitZyxcRoundTrips writes the intermediate representation of every
// program and reads it back.
func TestEmitZyxcRoundTrips(t *testing.T) {
	for _, name := range programs(t) {
		t.Run(name, func(t *testing.T) {
			dir := copyTree(t, filepath.Join("testdata", name))
			stdout, stderr, code := run(dir, "emit-zyxc", ".")
			if code != 0 {
				t.Fatalf("emit-zyxc failed with %d:\n%s", code, stderr)
			}
			if stdout == "" {
				t.Fatal("emit-zyxc wrote nothing")
			}
			if _, err := zyxc.Parse([]byte(stdout)); err != nil {
				t.Fatalf("the emitted representation does not read back: %v", err)
			}
		})
	}
}
