package concurrency

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"zyx/internal/toolchain"
)

// A program whose coroutines can never be woken is a deadlock, and the runtime
// reports it rather than leaving the program to hang or ending it without a
// word. The two shapes below are the ones a program has: one coroutine waiting
// for a value that never comes, and two waiting for each other.

const deadlockModule = "module example.com/deadlock\n\nzyx 0.1\n"

// deadlockWait is a coroutine that receives from a channel nobody sends on.
const deadlockWait = `package main

import "std/fmt"

func main() {
    var ch = new chan<i64>(1)
    var v, ok = ch.Recv()
    if ok {
        fmt.PrintI64(v)
    }
}
`

// deadlockMutual is a coroutine waiting for another one that waits in turn for
// it: the region waits for the child, and the child waits for a value the
// program will only send after the region returns.
const deadlockMutual = `package main

import "std/fmt"

func waiter(replies: chan<i64>) {
    var v, ok = replies.Recv()
    if ok {
        fmt.PrintI64(v)
    }
}

func main() {
    var replies = new chan<i64>(1)
    region {
        spawn waiter(replies)
    }
    fmt.Print("done")
    fmt.PrintNl()
}
`

// builtSource writes a program of one file into a directory of its own and
// builds it.
func builtSource(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.zyx"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zyx.mod"), []byte(deadlockModule), 0o644); err != nil {
		t.Fatal(err)
	}
	return builtProgram(t, dir)
}

// TestDeadlockIsReported requires a program that cannot continue to say so and
// to exit with the code the runtime reports a fault with, at every scheduler
// size: a thread count that hides a deadlock would leave it to hang.
func TestDeadlockIsReported(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	cases := []struct {
		name   string
		source string
	}{
		{"waiting for a value that never comes", deadlockWait},
		{"two coroutines waiting for each other", deadlockMutual},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := builtSource(t, tc.source)
			for _, threads := range threadCounts {
				name := threads
				if name == "" {
					name = "default"
				}
				t.Run("threads="+name, func(t *testing.T) {
					out, err := runAt(t, path, threads)
					if errors.Is(err, os.ErrDeadlineExceeded) {
						t.Fatalf("at %s threads the program did not finish:\n%s", threads, out)
					}
					if err == nil {
						t.Fatalf("at %s threads the program ended without a report:\n%s", threads, out)
					}
					var exit *exec.ExitError
					if errors.As(err, &exit) && exit.ExitCode() != 101 {
						t.Errorf("at %s threads the program exited with %d, want 101:\n%s",
							threads, exit.ExitCode(), out)
					}
					if !strings.Contains(out, "fatal: all coroutines are asleep: deadlock") {
						t.Errorf("at %s threads the program printed\n%s\nwant a deadlock report",
							threads, out)
					}
				})
			}
		})
	}
}
