// The version command prints compiler, LLVM, and zig versions.

package cli

import (
	"fmt"

	"zyx/internal/toolchain"
)

// Version is the compiler version this build reports.
const Version = "0.1.0"

// version prints the compiler version and the host target.
func (s *Session) version(args []string) int {
	if _, err := s.parseOptions(args); err != nil {
		return s.usageError(err)
	}
	host := toolchain.HostTarget()
	fmt.Fprintf(s.Stdout, "zyx %s\n", Version)
	fmt.Fprintf(s.Stdout, "target %s\n", host.Triple)
	if t, err := toolchain.Locate(); err == nil {
		fmt.Fprintf(s.Stdout, "clang %s\n", t.Clang)
		fmt.Fprintf(s.Stdout, "runtime %s\n", t.Runtime)
	} else {
		fmt.Fprintf(s.Stdout, "toolchain unavailable: %v\n", err)
	}
	return exitOK
}
