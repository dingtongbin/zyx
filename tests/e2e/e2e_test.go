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
	"sync"
	"testing"

	"zyx/internal/cli"
	"zyx/internal/toolchain"
	"zyx/internal/zyxc"
)

// env gives the compiler the standard library and the runtime tree of the
// checkout, so a test needs no installation. The cache is one directory for
// the whole test binary, so every corpus reuses the runtime library and the
// artifacts it shares rather than building its own copy.
func env(name string) string {
	switch name {
	case "ZYX_STD":
		return filepath.Join(repoRoot(), "std")
	case "ZYX_RUNTIME":
		return filepath.Join(repoRoot(), "runtime")
	case "ZYX_CACHE":
		return sharedCache()
	}
	return ""
}

// sharedCache returns the cache directory the tests share, creating it the
// first time it is asked for.
var sharedCache = sync.OnceValue(func() string {
	dir, err := os.MkdirTemp("", "zyx-test-cache-")
	if err != nil {
		panic(err)
	}
	return dir
})

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
		{"index leaves the string", "var text = \"abc\"\n\tfmt.PrintU64(u64(text[9]))", "panic: index out of range"},
		{"a negative index reads as a large one", "var xs: [i64; 4] = [1, 2, 3, 4]\n\tvar at: i64 = -1\n\tfmt.PrintI64(xs[usize(at)])", "panic: index out of range"},
		{"a map key that is not there", "var m = new Map<str, i64>()\n\tm[\"a\"] = 1\n\tfmt.PrintI64(m[\"b\"])", "panic: map key not found"},
		{"a compound assignment to a key that is not there", "var m = new Map<str, i64>()\n\tm[\"a\"] = 1\n\tm[\"b\"] += 1\n\tfmt.PrintI64(m[\"a\"])", "panic: map key not found"},
		{"a container modified during an iteration", "var xs = new Array<i64>(0)\n\txs.Push(1)\n\tvar ys = xs\n\tfor x in xs {\n\t\tfmt.PrintI64(x)\n\t\tys.Push(2)\n\t}", "panic: the container was modified while it was being iterated"},
		{"a map modified during an iteration", "var m = new Map<i64, i64>()\n\tm.Insert(1, 10)\n\tvar n = m\n\tfor k, v in m {\n\t\tfmt.PrintI64(k + v)\n\t\tn.Insert(2, 20)\n\t}", "panic: the container was modified while it was being iterated"},
	}
	for _, level := range []string{"-O0", "-O2", "-O3"} {
		for _, c := range cases {
			t.Run(level+"/"+c.name, func(t *testing.T) {
				t.Parallel()
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

// TestForeignFaultIsReported checks what happens when a fault happens in code
// that was not compiled with the runtime. The report names what happened, names
// the functions of the program that the stack passed through, and ends the
// process the way a panic ends it. The fault is made in a C function the package
// carries the source of, which is the case the report exists for: no runtime can
// unwind such a frame, and what a person needs is to read where it happened.
func TestForeignFaultIsReported(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	t.Parallel()
	dir := t.TempDir()
	writeProgram(t, dir, "package main\n\nimport \"std/fmt\"\n\nimport c \"fault.h\"\n\n"+
		"func main() {\n\tfmt.Println(\"before\")\n\tc.touch_null()\n\tfmt.Println(\"after\")\n}\n")
	extra := map[string]string{
		"fault.h": "void touch_null(void);\n",
		"fault.c": "void touch_null(void) {\n  volatile int *p = 0;\n  *p = 1;\n}\n",
	}
	for name, body := range extra {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, code := run(dir, "run", "./...")
	if code != 101 {
		t.Errorf("the process ended with %d, want 101\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "before") {
		t.Errorf("standard output =\n%s\nwant it to hold %q", stdout, "before")
	}
	if strings.Contains(stdout, "after") {
		t.Errorf("standard output =\n%s\nwant it to stop where the fault is", stdout)
	}
	for _, want := range []string{"fatal:", "frames:", "  at ", "example.com/program.main"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("standard error =\n%s\nwant it to hold %q", stderr, want)
		}
	}
}

// TestPanicReportNamesTheStack checks the shape of a panic report: the message
// first, then one line per frame from the inside out, each naming the function
// and where it was declared. The frames are taken where the panic is raised,
// because the stack a panic unwinds is gone by the time it is reported.
func TestPanicReportNamesTheStack(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	t.Parallel()
	dir := t.TempDir()
	writeProgram(t, dir, "package main\n\nimport \"std/fmt\"\n\n"+
		"func divide(a: i64, b: i64) -> i64 {\n\treturn a / b\n}\n\n"+
		"func main() {\n\tfmt.Println(\"before\")\n\tfmt.PrintI64(divide(1, 0))\n}\n")
	stdout, stderr, code := run(dir, "run", "./...")
	if code != 101 {
		t.Errorf("the process ended with %d, want 101\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "before") {
		t.Errorf("standard output =\n%s\nwant it to hold %q", stdout, "before")
	}
	for _, want := range []string{"panic: division by zero", "  at ", "main.zyx:", "example.com/program.main"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("standard error =\n%s\nwant it to hold %q", stderr, want)
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
			// A corpus program is built in a directory of its own and shares
			// nothing with the others but the cache, so the corpus runs in
			// parallel: a build spends most of its time in the toolchain, and
			// one program at a time would leave the machine idle.
			t.Parallel()
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
			t.Parallel()
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
			t.Parallel()
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
			t.Parallel()
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
