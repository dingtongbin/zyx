package concurrency

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"zyx/internal/toolchain"
)

// The benchmarks measure the parts of the runtime that a program spends its
// time in, each one through a program of the corpus that does nothing else.
// Each program counts its operations, so the number of nanoseconds a run takes
// divided by the count is the cost of one operation:
//
//	benchspawn    20000 coroutines started, handoffs back, and woken
//	benchchurn    20000 coroutines started and finished one at a time
//	benchchurnlanes  many coroutines churning at once, one on each thread
//	benchchan     a million handoffs between two coroutines over two channels
//	benchchanready  two million channel operations that never park
//	benchloop      two million loop iterations
//	benchcall     100000 C calls that return at once
//
// A run is a whole process, and a process costs about twenty milliseconds
// before it does anything, most of it the operating system's: BenchmarkProcess
// measures that floor through the smallest program of the corpus, and every
// figure above is on top of it. The four that count in the millions decompose
// each other as well: a handoff is the cost of a handoff benchmark less the one
// that never parks, and the channel operation that never parks is its own
// benchmark less the loop.
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

// BenchmarkProcess measures what a process costs before and after the work its
// program does, which is the floor every other benchmark here stands on.
func BenchmarkProcess(b *testing.B) {
	run := benchRun(b, "tiny")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
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

// BenchmarkChannelReady measures a channel operation that finds its channel
// ready, so no coroutine parks and the scheduler is not involved.
func BenchmarkChannelReady(b *testing.B) {
	run := benchRun(b, "benchchanready")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
	}
}

// BenchmarkLoop measures two million iterations of a loop, which is what the
// benchmarks above do not measure about themselves.
func BenchmarkLoop(b *testing.B) {
	run := benchRun(b, "benchloop")
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

// BenchmarkCoroutineChurn measures a program that starts a coroutine for every
// piece of work and lets each one finish before the next begins, which is what a
// program pays for a coroutine it uses and gives up.
func BenchmarkCoroutineChurn(b *testing.B) {
	run := benchRun(b, "benchchurn")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
	}
}

// BenchmarkCoroutineChurnLanes measures the same thing with every scheduler
// thread churning at once, which is where the tables a thread keeps for its own
// finished coroutines and stacks show: one program churning alone only ever
// touches the table of the thread it runs on. The program prints how many
// coroutines it started and finished, so what one of them costs is the time of
// a whole run divided by that count.
func BenchmarkCoroutineChurnLanes(b *testing.B) {
	if _, err := toolchain.Locate(); err != nil {
		b.Skipf("the toolchain is not available: %v", err)
	}
	src := filepath.Join("testdata", "benchchurnlanes")
	dir := b.TempDir()
	copyTree(b, src, dir)
	path := builtProgram(b, dir)
	want, err := os.ReadFile(filepath.Join(src, "want.txt"))
	if err != nil {
		b.Fatal(err)
	}
	count := number(b, string(want), "coroutines=")
	if count == 0 {
		b.Fatal("the program started no coroutines")
	}
	b.ResetTimer()
	var spent time.Duration
	for i := 0; i < b.N; i++ {
		begin := time.Now()
		out, err := runAt(b, path, "")
		spent += time.Since(begin)
		if err != nil {
			b.Fatalf("the program did not finish cleanly: %v\n%s", err, out)
		}
		if out != string(want) {
			b.Fatalf("the program printed\n%s\nwant\n%s", out, want)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(spent.Nanoseconds())/float64(int64(b.N)*count), "ns/coroutine")
}

// BenchmarkCoroutineScale measures what a coroutine costs when a program keeps
// many of them alive at once: the bytes each of them commits, which is what
// decides whether ten million fit in memory at all, and the time the program
// takes to start them. The number of bytes is what one coroutine costs and not
// what the program's count makes it, so the program here is the one the scale
// test runs, and the count only has to be large enough to be read.
func BenchmarkCoroutineScale(b *testing.B) {
	if _, err := toolchain.Locate(); err != nil {
		b.Skipf("the toolchain is not available: %v", err)
	}
	dir := b.TempDir()
	copyTree(b, "scale", dir)
	path := builtProgram(b, dir)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := runAt(b, path, "")
		if err != nil {
			b.Fatalf("the program did not finish: %v\n%s", err, out)
		}
		count := number(b, out, "coroutines=")
		committed := number(b, out, "committed=")
		if count == 0 {
			b.Fatal("the program kept no coroutines")
		}
		b.ReportMetric(float64(committed)/float64(count), "bytes/coroutine")
	}
}
