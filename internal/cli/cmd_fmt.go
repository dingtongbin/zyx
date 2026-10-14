// The fmt command rewrites sources into canonical form.

package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	zyxfmt "zyx/internal/fmt"
)

// fmt rewrites every source of the target packages. A file that does not
// parse is reported and left untouched.
func (s *Session) fmt(args []string) int {
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
	loader := s.loader(m, o)
	failed := false
	for _, path := range paths {
		pkg, err := loader.LoadPackage(path)
		if err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
		if s.report(pkg.Diags) {
			failed = true
			continue
		}
		for _, file := range pkg.Files {
			out := zyxfmt.Source(file)
			if bytes.Equal(out, file.Data()) {
				continue
			}
			full := filepath.Join(m.Root, filepath.FromSlash(file.Name()))
			if err := os.WriteFile(full, out, 0o644); err != nil {
				fmt.Fprintln(s.Stderr, "zyx:", err)
				return exitError
			}
			if o.verbose {
				fmt.Fprintf(s.Stdout, "formatted %s\n", file.Name())
			}
		}
	}
	if failed {
		return exitError
	}
	return exitOK
}
