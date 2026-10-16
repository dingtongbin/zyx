// Package concurrency drives the corpus of concurrent programs at several
// scheduler thread counts. A program whose result depends on how many threads
// the scheduler runs is wrong, so every program here must print the same bytes
// at one thread and at many, and must finish.
package concurrency

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"zyx/internal/cli"
	"zyx/internal/toolchain"
)

// threadCounts are the scheduler sizes a program is run at. One is the layout
// a program has when nothing asks for more; the rest exercise handoffs between
// scheduler threads, which is where a race hides.
var threadCounts = []string{"1", "2", "4", "8"}

// env gives the compiler the standard library and the runtime tree of the
// checkout, so a test needs no installation.
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

var sharedCache = sync.OnceValue(func() string {
	dir, err := os.MkdirTemp("", "zyx-concurrency-cache-")
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

// copyTree copies a directory of sources into a directory of its own.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
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
}

// builtProgram builds a corpus program and returns the executable it wrote.
func builtProgram(t *testing.T, dir string) string {
	t.Helper()
	var out, errBuf bytes.Buffer
	s := &cli.Session{Stdout: &out, Stderr: &errBuf, Dir: dir, Env: env}
	if code := s.Run([]string{"build", "./..."}); code != 0 {
		t.Fatalf("build failed with %d:\n%s", code, errBuf.String())
	}
	entries, err := os.ReadDir(filepath.Join(dir, "build"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".exe") || !strings.Contains(entry.Name(), ".") {
			return filepath.Join(dir, "build", entry.Name())
		}
	}
	t.Fatal("the build wrote no executable")
	return ""
}

// runAt runs a program with a scheduler of the given size and returns what it
// printed. A program that does not finish is a failure of its own.
func runAt(t *testing.T, path, threads string) (string, error) {
	t.Helper()
	cmd := exec.Command(path)
	cmd.Env = append(os.Environ(), "ZYX_THREADS="+threads, "ZYX_BLOCKPOOL=8")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return out.String(), err
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		return out.String(), os.ErrDeadlineExceeded
	}
}

// TestThreadCountDoesNotChangeOutput runs every program of the corpus at each
// scheduler size and requires the same bytes every time, which is the property
// a concurrent runtime must have and the one an intermittent race breaks.
func TestThreadCountDoesNotChangeOutput(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	for _, name := range programs(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := filepath.Join("testdata", name)
			dir := t.TempDir()
			copyTree(t, src, dir)
			path := builtProgram(t, dir)
			want, err := os.ReadFile(filepath.Join(src, "want.txt"))
			if err != nil {
				t.Fatal(err)
			}
			for _, threads := range threadCounts {
				t.Run("threads="+threads, func(t *testing.T) {
					got, err := runAt(t, path, threads)
					if err != nil {
						t.Fatalf("at %s threads the program did not finish cleanly: %v\n%s", threads, err, got)
					}
					if got != string(want) {
						t.Errorf("at %s threads the output was\n%s\nwant\n%s", threads, got, want)
					}
				})
			}
		})
	}
}

// TestThreadCountDoesNotChangeOutputRepeated runs the same programs again at
// four threads a few times each, because a race that hides in one run appears
// in the next. It stays short: the first test is what covers the thread
// counts, and this one only asks whether the answer holds when it is asked
// again.
func TestThreadCountDoesNotChangeOutputRepeated(t *testing.T) {
	if testing.Short() {
		t.Skip("the repeated runs are skipped in a short test run")
	}
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	for _, name := range programs(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := filepath.Join("testdata", name)
			dir := t.TempDir()
			copyTree(t, src, dir)
			path := builtProgram(t, dir)
			want, err := os.ReadFile(filepath.Join(src, "want.txt"))
			if err != nil {
				t.Fatal(err)
			}
			for run := 0; run < 5; run++ {
				got, err := runAt(t, path, "4")
				if err != nil {
					t.Fatalf("at 4 threads run %d did not finish cleanly: %v\n%s", run, err, got)
				}
				if got != string(want) {
					t.Fatalf("at 4 threads run %d printed\n%s\nwant\n%s", run, got, want)
				}
			}
		})
	}
}
