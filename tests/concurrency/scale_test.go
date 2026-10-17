package concurrency

import (
	"strconv"
	"strings"
	"testing"

	"zyx/internal/toolchain"
)

// packedBytesPerCoroutine is what a coroutine of the packed tier may commit: the
// window the tier is given plus the step one growth reaches, which is the most a
// coroutine that turns out to need more than the window pays.
const packedBytesPerCoroutine = 12 * 1024

// TestPackedCoroutinesAreSmall keeps a hundred thousand coroutines alive at once
// and holds the runtime to what each of them commits. The same number of bytes
// per coroutine is what ten million of them would cost, so this is what decides
// whether a program of that many fits in memory at all.
func TestPackedCoroutinesAreSmall(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	dir := t.TempDir()
	copyTree(t, "scale", dir)
	path := builtProgram(t, dir)
	out, err := runAt(t, path, "")
	if err != nil {
		t.Fatalf("the program did not finish: %v\n%s", err, out)
	}
	count := number(t, out, "coroutines=")
	committed := number(t, out, "committed=")
	if count == 0 {
		t.Fatal("the program kept no coroutines")
	}
	if per := committed / count; per > packedBytesPerCoroutine {
		t.Errorf("a packed coroutine committed %d bytes, want at most %d\n%s",
			per, packedBytesPerCoroutine, out)
	}
}

// number reads the value after a prefix of what a program printed.
func number(t *testing.T, out, prefix string) int64 {
	t.Helper()
	at := strings.Index(out, prefix)
	if at < 0 {
		t.Fatalf("the output has no %q:\n%s", prefix, out)
	}
	rest := out[at+len(prefix):]
	if end := strings.IndexByte(rest, '\n'); end >= 0 {
		rest = rest[:end]
	}
	value, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
	if err != nil {
		t.Fatalf("the value after %q is not a number: %v", prefix, err)
	}
	return value
}
