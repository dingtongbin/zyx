package concurrency

import (
	"strings"
	"testing"

	"zyx/internal/toolchain"
)

// TestReleasedDescriptorIsWatchableAgain holds the runtime to what a release
// means for the number it names. A descriptor number is what the system hands
// out again as soon as it can, so when a released descriptor is closed and a
// new one takes its number, a wait on that number must watch the descriptor
// that names it now. A runtime that kept a mark of the release on the entry
// answered the new descriptor with "the descriptor was released", which is a
// spurious Closed, and this test fails on that answer.
//
// The program in loopfd/ is built together with a C helper that hands it the
// sockets: it releases a descriptor that is still open and waits on it again,
// and it releases one, closes it, has the system give the number back, and
// waits on the descriptor that took it.
func TestReleasedDescriptorIsWatchableAgain(t *testing.T) {
	if _, err := toolchain.Locate(); err != nil {
		t.Skipf("the toolchain is not available: %v", err)
	}
	dir := t.TempDir()
	copyTree(t, "loopfd", dir)
	path := builtProgram(t, dir)
	for _, threads := range []string{"", "1"} {
		name := threads
		if name == "" {
			name = "default"
		}
		t.Run("threads="+name, func(t *testing.T) {
			out, err := runAt(t, path, threads)
			if err != nil {
				t.Fatalf("the program did not finish cleanly: %v\n%s", err, out)
			}
			if strings.Contains(out, "refused") {
				t.Fatalf("a wait answered with the release that belonged to the descriptor before:\n%s", out)
			}
			if !strings.Contains(out, "released descriptor waitable again") {
				t.Fatalf("the program printed\n%s\nwant a released descriptor to be watchable again", out)
			}
			if strings.Contains(out, "the number did not come back") {
				t.Logf("the system did not hand the number back, so only the open descriptor was tried:\n%s", out)
			} else if !strings.Contains(out, "new descriptor waitable") {
				t.Fatalf("the program printed\n%s\nwant a wait on the new descriptor to answer", out)
			}
			// A release must still wake a coroutine parked on the descriptor:
			// it answers that the descriptor is gone, and does not stay parked.
			if !strings.Contains(out, "released descriptor woke") {
				t.Fatalf("the program printed\n%s\nwant a release to wake a coroutine parked on the descriptor", out)
			}
		})
	}
}
