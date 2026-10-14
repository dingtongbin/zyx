// The mod command manages the module manifest.

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zyx/internal/mod"
)

// mod dispatches the module subcommands.
func (s *Session) mod(args []string) int {
	if len(args) == 0 {
		return s.usageError(fmt.Errorf("mod needs a subcommand: init or tidy"))
	}
	switch args[0] {
	case "init":
		return s.modInit(args[1:])
	case "tidy":
		return s.modTidy(args[1:])
	}
	return s.usageError(fmt.Errorf("unknown mod subcommand %q", args[0]))
}

// modInit writes a manifest for a new module.
func (s *Session) modInit(args []string) int {
	o, err := s.parseOptions(args)
	if err != nil {
		return s.usageError(err)
	}
	if len(o.paths) > 1 {
		return s.usageError(fmt.Errorf("mod init takes at most one module path"))
	}
	path := ""
	if len(o.paths) == 1 {
		path = o.paths[0]
	}
	if path == "" {
		path = filepath.Base(s.Dir)
	}
	if !strings.Contains(path, ".") && !strings.Contains(path, "/") {
		return s.usageError(fmt.Errorf("module path %q is not a full path such as example.com/app", path))
	}
	target := filepath.Join(s.Dir, "zyx.mod")
	if _, err := os.Stat(target); err == nil {
		fmt.Fprintf(s.Stderr, "zyx: %s already exists\n", target)
		return exitError
	}
	f := &mod.File{Module: path, Lang: "0.1"}
	if err := os.WriteFile(target, f.Format(), 0o644); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	return exitOK
}

// modTidy rewrites the requirements to the ones the module's own packages
// import. A requirement that is not already in the manifest needs a version
// from the network, which a build without a module proxy cannot settle.
func (s *Session) modTidy(args []string) int {
	o, err := s.parseOptions(args)
	if err != nil {
		return s.usageError(err)
	}
	m, err := s.findModule(s.Dir)
	if err != nil {
		return s.usageError(err)
	}
	loader := s.loader(m, o)
	all, err := loader.All("")
	if err != nil {
		return s.usageError(err)
	}
	needed := map[string]bool{}
	for _, path := range all {
		pkg, err := loader.LoadPackage(path)
		if err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
		if s.report(pkg.Diags) {
			return exitError
		}
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, "std/") || imp == "std" {
				continue
			}
			if imp == m.File.Module || strings.HasPrefix(imp, m.File.Module+"/") {
				continue
			}
			needed[imp] = true
		}
	}

	known := map[string]string{}
	for _, req := range m.File.Require {
		known[req.Path] = req.Version
	}
	var missing []string
	var kept []mod.Requirement
	for path := range needed {
		version, ok := known[path]
		if !ok {
			missing = append(missing, path)
			continue
		}
		kept = append(kept, mod.Requirement{Path: path, Version: version})
	}
	if len(missing) != 0 {
		for _, path := range missing {
			fmt.Fprintf(s.Stderr, "zyx: %s is imported but not required; add `require %s <version>` to zyx.mod\n", path, path)
		}
		return exitError
	}
	// An indirect requirement stays only while a kept requirement still
	// needs it.
	final := kept
	for _, req := range m.File.Require {
		if containsRequirement(kept, req.Path) {
			continue
		}
		if needed[req.Path] {
			final = append(final, req)
		}
	}
	m.File.Require = final
	if err := os.WriteFile(filepath.Join(m.Root, "zyx.mod"), m.File.Format(), 0o644); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	return exitOK
}

// containsRequirement reports whether a requirement list holds a path.
func containsRequirement(list []mod.Requirement, path string) bool {
	for _, r := range list {
		if r.Path == path {
			return true
		}
	}
	return false
}
