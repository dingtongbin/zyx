// The emit commands dump .zyxc, LLVM IR, and assembly for inspection.

package cli

import (
	"fmt"
)

// emitZyxc writes the intermediate representation of one package. --mono
// writes the view after monomorphization, which is what the backend is given.
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
	session := s.session(m, o)
	results, err := session.FrontEnd(paths[0])
	if err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	entry := results[len(results)-1]
	if len(entry.Report) != 0 {
		fmt.Fprint(s.Stderr, string(entry.Report))
		return exitError
	}
	text := entry.Text
	if o.mono {
		text, err = session.MonoText(paths[0], paths[0], results)
		if err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
	}
	if entry.IR == nil && !o.mono {
		return exitError
	}
	if err := s.writeOutput(o.output, text); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	return exitOK
}
