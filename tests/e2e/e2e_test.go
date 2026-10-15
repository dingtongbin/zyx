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

// TestCoroutinePanicIsContained checks that a panic in a spawned coroutine
// is reported, recorded for whoever joins it, and does not end the process.
func TestCoroutinePanicIsContained(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	dir := t.TempDir()
	files := map[string]string{
		"zyx.mod": "module example.com/panics\n\nzyx 0.1\n",
		"main.zyx": "package main\n\nimport \"std/fmt\"\n\nfunc boom() {\n\tpanic(\"boom\")\n}\n\n" +
			"func calm() {\n\tfmt.Println(\"calm ran\")\n}\n\n" +
			"func main() {\n\tfmt.Println(\"start\")\n\tregion {\n\t\tspawn boom()\n\t\tspawn calm()\n\t}\n\tfmt.Println(\"end\")\n}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, code := run(dir, "run", "./...")
	if code != 0 {
		t.Errorf("the process ended with %d, want 0: a coroutine's panic belongs to the coroutine", code)
	}
	if !strings.Contains(stdout, "start") || !strings.Contains(stdout, "calm ran") || !strings.Contains(stdout, "end") {
		t.Errorf("the program did not run to the end:\n%s", stdout)
	}
	if !strings.Contains(stderr, "panic: boom") {
		t.Errorf("the panic was not reported:\n%s", stderr)
	}
}

// TestInsertedChecksPanic checks the checks the language inserts. Arithmetic
// that overflows, a divisor of zero, a shift amount that does not fit the
// width, and an index outside its sequence all panic with a message rather
// than producing a wrong value. Every case runs at every optimization level,
// because the language defines a check as part of the construct and no
// optimizer may remove it.
func TestInsertedChecksPanic(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	cases := []struct {
		name string
		body string
		want string
	}{
		{"addition overflows", "var big: i64 = 9223372036854775807\n\tfmt.PrintI64(big + 1)", "panic: integer overflow"},
		{"subtraction overflows", "var low: i64 = -9223372036854775807 - 1\n\tfmt.PrintI64(low - 1)", "panic: integer overflow"},
		{"multiplication overflows", "var big: i64 = 9223372036854775807\n\tfmt.PrintI64(big * 2)", "panic: integer overflow"},
		{"unsigned addition overflows", "var big: u64 = 18446744073709551615\n\tfmt.PrintU64(big + 1)", "panic: integer overflow"},
		{"negation overflows", "var low: i64 = -9223372036854775807 - 1\n\tfmt.PrintI64(-low)", "panic: integer overflow"},
		{"division by zero", "var one: i64 = 1\n\tvar zero: i64 = 0\n\tfmt.PrintI64(one / zero)", "panic: division by zero"},
		{"remainder by zero", "var one: i64 = 1\n\tvar zero: i64 = 0\n\tfmt.PrintI64(one % zero)", "panic: division by zero"},
		{"unsigned division by zero", "var one: u64 = 1\n\tvar zero: u64 = 0\n\tfmt.PrintU64(one / zero)", "panic: division by zero"},
		{"signed division has no result", "var low: i64 = -9223372036854775807 - 1\n\tvar minus: i64 = -1\n\tfmt.PrintI64(low / minus)", "panic: integer overflow"},
		{"shift amount does not fit", "var one: i64 = 1\n\tvar wide: i64 = 64\n\tfmt.PrintI64(one << wide)", "panic: shift amount out of range"},
		{"shift amount does not fit on the right", "var one: u64 = 1\n\tvar wide: u64 = 64\n\tfmt.PrintU64(one >> wide)", "panic: shift amount out of range"},
		{"index leaves the array", "var xs: [i64; 4] = [1, 2, 3, 4]\n\tfmt.PrintI64(xs[7])", "panic: index out of range"},
		{"index leaves the view", "var xs: [i64; 4] = [1, 2, 3, 4]\n\tvar view = xs[1..3]\n\tfmt.PrintI64(view[9])", "panic: index out of range"},
		{"a negative index reads as a large one", "var xs: [i64; 4] = [1, 2, 3, 4]\n\tvar at: i64 = -1\n\tfmt.PrintI64(xs[usize(at)])", "panic: index out of range"},
	}
	for _, level := range []string{"-O0", "-O2", "-O3"} {
		for _, c := range cases {
			t.Run(level+"/"+c.name, func(t *testing.T) {
				dir := t.TempDir()
				writeProgram(t, dir,
					"package main\n\nimport \"std/fmt\"\n\nfunc main() {\n\t"+c.body+"\n}\n")
				_, stderr, code := run(dir, "run", level, "./...")
				if code != 101 {
					t.Errorf("exit code = %d, want 101\n%s", code, stderr)
				}
				if !strings.Contains(stderr, c.want) {
					t.Errorf("standard error =\n%s\nwant it to hold %q", stderr, c.want)
				}
			})
		}
	}
}

// writeProgram writes a one-file program into a directory.
func writeProgram(t *testing.T, dir, main string) {
	t.Helper()
	files := map[string]string{
		"zyx.mod":  "module example.com/program\n\nzyx 0.1\n",
		"main.zyx": main,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
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
			if want, err := os.ReadFile(filepath.Join(dir, "stderr.txt")); err == nil {
				if stderr != string(want) {
					t.Errorf("standard error =\n%s\nwant\n%s", stderr, want)
				}
			} else if stderr != "" {
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
