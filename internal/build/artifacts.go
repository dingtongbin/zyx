// Output artifact placement.

package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zyx/internal/toolchain"
)

// ExecutablePath returns where an executable built from a package goes. The
// command line places it under build/ at the module root, named after the
// package directory and carrying the target's own extension.
func ExecutablePath(root, importPath string, target toolchain.Target) string {
	return filepath.Join(root, "build", executableName(importPath)+target.ExecutableSuffix())
}

// OutputPaths returns where each main package's executable goes.
//
// Without -o every executable goes under build/ at the module root, named
// after its package directory; two main packages whose directories share a
// name need -o to tell them apart. With -o naming a directory, which is what
// a trailing separator or an existing directory means, each executable goes
// there under its own name. With -o naming a file there is room for one
// executable only.
func OutputPaths(root string, importPaths []string, output string, target toolchain.Target) ([]string, error) {
	dir := output
	if output == "" {
		dir = filepath.Join(root, "build")
	} else if !isDir(output) {
		if len(importPaths) > 1 {
			return nil, fmt.Errorf("%d main packages cannot share the output file %s; name a directory instead", len(importPaths), output)
		}
		return []string{output}, nil
	}
	out := make([]string, len(importPaths))
	seen := map[string]string{}
	for i, path := range importPaths {
		full := filepath.Join(dir, executableName(path)+target.ExecutableSuffix())
		if other, ok := seen[full]; ok {
			return nil, fmt.Errorf("%s and %s would both be written to %s", other, path, full)
		}
		seen[full] = path
		out[i] = full
	}
	return out, nil
}

// isDir reports whether a path names a directory: one that ends in a
// separator, or one that is already a directory.
func isDir(path string) bool {
	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, string(filepath.Separator)) {
		return true
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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
