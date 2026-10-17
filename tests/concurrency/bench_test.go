package concurrency

import (
	"os"
	"path/filepath"
	"testing"

	"zyx/internal/toolchain"
)

// The benchmarks measure the parts of the runtime that a program spends its
// time in, each one through a program of the corpus that does nothing else.
// Each program counts its operations, so the number of nanoseconds a run takes
// divided by the count is the cost of one operation:
//
//	benchspawn    500 coroutines started, handoffs back, and woken
//	benchchan  200000 handoffs between two coroutines over two channels
//	benchcall  100000 C calls that return at once
//
// A run also checks the program's answer, so a benchmark that stops measuring
// the right thing fails instead of reporting a number.

// benchRun builds a program of the corpus once and answers a function that runs
// it and checks what it printed.
func benchRun(b *testing.B, name string) func() {
	b.Helper()
	if _, err := toolchain.Locate(); err != nil {
		b.Skipf("the toolchain is not available: %v", err)
	}
	src := filepath.Join("testdata", name)
	dir := b.TempDir()
	copyTree(b, src, dir)
	path := builtProgram(b, dir)
	want, err := os.ReadFile(filepath.Join(src, "want.txt"))
	if err != nil {
		b.Fatal(err)
	}
	return func() {
		got, err := runAt(b, path, "")
		if err != nil {
			b.Fatalf("the program did not finish cleanly: %v\n%s", err, got)
		}
		if got != string(want) {
			b.Fatalf("the program printed\n%s\nwant\n%s", got, want)
		}
	}
}

// BenchmarkCoroutineSpawn measures starting a coroutine, the handoff that
// reports it finished, and the wake that hands it back, together.
func BenchmarkCoroutineSpawn(b *testing.B) {
	run := benchRun(b, "benchspawn")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
	}
}

// BenchmarkChannelWake measures a handoff between two coroutines over two
// channels, with nothing else mixed in.
func BenchmarkChannelWake(b *testing.B) {
	run := benchRun(b, "benchchan")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
	}
}

// BenchmarkCCall measures a C call that may block and returns at once, which is
// the whole cost of the bracket around it.
func BenchmarkCCall(b *testing.B) {
	run := benchRun(b, "benchcall")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
	}
}
