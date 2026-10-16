// Vendor directory support: the mirror of the modules a build reads.
//
// A vendor directory holds the modules themselves, so a build with one runs
// without the module cache and without the network. The file beside them,
// vendor/modules.txt, says which modules the build reads and which packages it
// reads from each, in the form go's modules.txt uses: a comment line per
// module, a comment marking the modules the main module requires itself, and
// the import paths that follow.

package mod

import (
	"fmt"
	"sort"
	"strings"
)

// A VendoredModule is one module a vendor directory holds.
type VendoredModule struct {
	Module
	// Explicit marks a module the main module requires itself, rather than
	// one the dependency graph reached.
	Explicit bool
	// Lang is the language version the module declares.
	Lang string
	// Packages are the import paths the build reads from it, in byte order.
	Packages []string
}

// FormatModulesTxt returns the text of vendor/modules.txt.
func FormatModulesTxt(modules []VendoredModule) []byte {
	sorted := append([]VendoredModule(nil), modules...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	var b strings.Builder
	for _, m := range sorted {
		fmt.Fprintf(&b, "# %s %s\n", m.Path, m.Version)
		if m.Explicit {
			if m.Lang != "" {
				fmt.Fprintf(&b, "## explicit; zyx %s\n", m.Lang)
			} else {
				b.WriteString("## explicit\n")
			}
		}
		packages := append([]string(nil), m.Packages...)
		sort.Strings(packages)
		for _, path := range packages {
			fmt.Fprintf(&b, "%s\n", path)
		}
	}
	return []byte(b.String())
}

// ParseModulesTxt reads vendor/modules.txt. A line that is not a module, a
// marker, or a package path is an error: a vendor directory whose file cannot
// be read cannot be trusted to describe what it holds.
func ParseModulesTxt(data []byte) ([]VendoredModule, error) {
	var out []VendoredModule
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "## "):
			if len(out) == 0 {
				return nil, fmt.Errorf("modules.txt line %d marks a module before naming one", i+1)
			}
			marker := strings.TrimPrefix(line, "## ")
			fields := strings.Split(marker, ";")
			out[len(out)-1].Explicit = strings.TrimSpace(fields[0]) == "explicit"
			for _, field := range fields[1:] {
				field = strings.TrimSpace(field)
				if version, ok := strings.CutPrefix(field, "zyx "); ok {
					out[len(out)-1].Lang = version
				}
			}
		case strings.HasPrefix(line, "# "):
			fields := strings.Fields(strings.TrimPrefix(line, "# "))
			if len(fields) != 2 {
				return nil, fmt.Errorf("modules.txt line %d does not name a module and a version: %q", i+1, line)
			}
			out = append(out, VendoredModule{Module: Module{Path: fields[0], Version: fields[1]}})
		default:
			if len(out) == 0 {
				return nil, fmt.Errorf("modules.txt line %d names a package before naming a module", i+1)
			}
			out[len(out)-1].Packages = append(out[len(out)-1].Packages, line)
		}
	}
	return out, nil
}
