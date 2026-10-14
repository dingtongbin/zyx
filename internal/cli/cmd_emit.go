// The emit commands dump .zyxc, LLVM IR, and assembly for inspection.

package cli

import (
	"fmt"

	"zyx/internal/build"
)

// emitZyxc writes the intermediate representation of one package.
func (s *Session) emitZyxc(args []string) int {
	o, err := s.parseOptions(args)
	if err != nil {
		return s.usageError(err)
	}
	m, err := s.findModule(s.Dir)
	if err != nil {
		return s.usageError(err)
	}
	paths, err := s.targets(m, o)
	if err != nil {
		return s.usageError(err)
	}
	if len(paths) != 1 {
		return s.usageError(fmt.Errorf("emit-zyxc takes a single package"))
	}
	session := &build.Session{Loader: s.loader(m, o)}
	results, err := session.FrontEnd(paths[0])
	if err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	entry := results[len(results)-1]
	if s.report(entry.Diags) {
		return exitError
	}
	if entry.IR == nil {
		return exitError
	}
	if err := s.writeOutput(o.output, entry.Text); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	return exitOK
}
