// The clean command removes the artifacts of a build.

package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

// clean removes the build tree of the module and the cache its intermediates
// went to, and with --cache the whole cache directory, which may be shared.
func (s *Session) clean(args []string) int {
	o, err := s.parseOptions(args)
	if err != nil {
		return s.usageError(err)
	}
	m, err := s.findModule(s.Dir)
	if err != nil {
		return s.usageError(err)
	}
	dirs := []string{
		filepath.Join(m.Root, "build"),
		filepath.Join(m.Root, ".zyx-cache"),
	}
	if o.cacheDir != "" {
		dirs = append(dirs, o.cacheDir)
	}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
		if o.verbose {
			fmt.Fprintf(s.Stdout, "removed %s\n", dir)
		}
	}
	return exitOK
}
