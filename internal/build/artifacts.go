// Output artifact placement.

package build

import (
	"path/filepath"
	"strings"
)

// ExecutablePath returns where an executable built from a package goes. The
// command line places it under build/ at the module root, named after the
// package directory.
func ExecutablePath(root, importPath string) string {
	return filepath.Join(root, "build", executableName(importPath))
}

// executableName returns the file name of the executable a package builds.
func executableName(importPath string) string {
	name := importPath
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		name = "app"
	}
	return name
}
