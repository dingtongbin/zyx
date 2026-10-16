// The mod command manages the module manifest.

package cli

import (
	"fmt"
	"io/fs"
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
	case "vendor":
		return s.modVendor(args[1:])
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

// modTidy rewrites the requirements to the ones the module's packages reach,
// fetches what is not there, and records the hashes a build fixes the result
// by. The reachable graph is the build graph together with the test graph,
// closed over the imports of everything it reaches, so a module only a
// dependency's package imports is required just the same.
func (s *Session) modTidy(args []string) int {
	o, err := s.parseOptions(args)
	if err != nil {
		return s.usageError(err)
	}
	m, err := s.findModule(s.Dir)
	if err != nil {
		return s.usageError(err)
	}
	if err := mod.CheckLang(m.File, Version); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	fetcher, err := s.fetcher()
	if err != nil {
		return s.usageError(err)
	}
	cache := s.moduleCache(m)
	checks := s.readSum(m)
	if checks == nil {
		checks = &mod.Sum{}
	}

	// The requirements as they stand, and the modules a replacement points at
	// a directory of the working tree.
	required := map[string]string{}
	var list []mod.Module
	for _, req := range m.File.Require {
		required[req.Path] = req.Version
		list = append(list, mod.Module{Path: req.Path, Version: req.Version})
	}
	local := map[string]string{}
	for _, rep := range m.File.Replace {
		if mod.LocalPath(rep.NewPath) {
			local[rep.Path] = rep.NewPath
		}
	}

	// findModule asks the source list for each prefix of an import path,
	// longest first, which is how a module the manifest does not name is
	// found.
	findModule := func(importPath string) (mod.Module, error) {
		if fetcher == nil {
			return mod.Module{}, fmt.Errorf("%s is imported but no module provides it, and no source is configured to find one", importPath)
		}
		fields := strings.Split(importPath, "/")
		for i := len(fields); i > 0; i-- {
			candidate := strings.Join(fields[:i], "/")
			version, err := fetcher.Latest(candidate)
			if err != nil {
				continue
			}
			return mod.Module{Path: candidate, Version: version}, nil
		}
		return mod.Module{}, fmt.Errorf("%s is imported but no module provides it", importPath)
	}

	// ensure fills the cache and the checksum file for one module version,
	// fetching what is missing. A module a replacement points at a directory
	// needs neither.
	ensure := func(dep mod.Module) error {
		if local[dep.Path] != "" {
			return nil
		}
		if checks.Content(dep.Path, dep.Version) != "" && checks.Manifest(dep.Path, dep.Version) != "" {
			if cache != nil && !cache.Has(dep.Path, dep.Version) {
				return fetcher.FetchInto(cache, checks, dep.Path, dep.Version)
			}
			return nil
		}
		if fetcher == nil {
			return fmt.Errorf("%s %s has no recorded hash in zyx.sum, and no source is configured to fetch it", dep.Path, dep.Version)
		}
		content, manifest, err := fetcher.FetchNew(cache, dep.Path, dep.Version)
		if err != nil {
			return err
		}
		checks.Add(dep.Path, dep.Version, content, manifest)
		return nil
	}

	needed := map[string]bool{}
	seen := map[string]bool{}
	var queue []string
	for _, testing := range []bool{false, true} {
		load := s.loaderFor(m, o, required, local)
		load.Testing = testing
		paths, err := load.All("")
		if err != nil {
			return s.usageError(err)
		}
		queue = append(queue, paths...)
	}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		if seen[path] {
			continue
		}
		seen[path] = true
		loader := s.loaderFor(m, o, required, local)
		pkg, err := loader.LoadPackage(path)
		if err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
		if s.report(pkg.Diags) {
			return exitError
		}
		for _, imp := range pkg.Imports {
			if imp == "std" || strings.HasPrefix(imp, "std/") {
				continue
			}
			if imp == m.File.Module || strings.HasPrefix(imp, m.File.Module+"/") {
				queue = append(queue, imp)
				continue
			}
			dep, ok := mod.ModuleOf(list, imp)
			if !ok {
				dep, err = findModule(imp)
				if err != nil {
					fmt.Fprintln(s.Stderr, "zyx:", err)
					return exitError
				}
				required[dep.Path] = dep.Version
				list = append(list, dep)
			}
			if err := ensure(dep); err != nil {
				fmt.Fprintln(s.Stderr, "zyx:", err)
				return exitError
			}
			needed[dep.Path] = true
			queue = append(queue, imp)
		}
	}

	// Version selection settles the version every requirement is written at,
	// so a module the graph raises to a higher version is written at the
	// version the build reads. A module nothing reads is dropped.
	//
	// Selection reads every requirement's manifest, including one the walk
	// found nothing to read from, so a requirement that is about to be dropped
	// is fetched and recorded like any other and then pruned away.
	grown := &mod.File{Module: m.File.Module, Lang: m.File.Lang}
	for path, version := range required {
		grown.Require = append(grown.Require, mod.Requirement{Path: path, Version: version})
	}
	settled, err := mod.BuildList(grown, cache, func(path, version string) error {
		return ensure(mod.Module{Path: path, Version: version})
	})
	if err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	var final []mod.Requirement
	for _, dep := range settled {
		if !needed[dep.Path] {
			continue
		}
		final = append(final, mod.Requirement{Path: dep.Path, Version: dep.Version})
	}

	// A hash whose requirement is gone is dropped, so the file says exactly
	// what the build depends on.
	checks.Prune(func(path string) bool { return containsRequirement(final, path) })

	m.File.Require = final
	sumPath := filepath.Join(m.Root, "zyx.sum")
	if err := writeAtomically(sumPath, checks.Format()); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	if err := writeAtomically(filepath.Join(m.Root, "zyx.mod"), m.File.Format()); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	return exitOK
}

// fetcher returns the source list a module command fetches from, or nil when
// nothing is configured to fetch.
func (s *Session) fetcher() (*mod.Fetcher, error) {
	sources, err := mod.ParseSources(s.env("ZYXPROXY"))
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, nil
	}
	return &mod.Fetcher{
		Sources: sources,
		NoProxy: mod.ParsePatterns(s.env("ZYXNOPROXY")),
		Private: mod.ParsePatterns(s.env("ZYXPRIVATE")),
	}, nil
}

// writeAtomically replaces a file: a reader sees the whole old file or the
// whole new one, never a half-written one, and a failure leaves the file as
// it was.
func writeAtomically(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-")
	if err != nil {
		return err
	}
	name := temp.Name()
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		os.Remove(name)
		return err
	}
	if err := temp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// prepareModule checks what a build needs of a module before it starts: the
// language version its manifest asks for, and the modules its requirements
// resolve to. Both are build errors rather than usage errors: the module is
// well formed, the build cannot serve it.
func (s *Session) prepareModule(m *module) bool {
	if err := mod.CheckLang(m.File, Version); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return false
	}
	// A vendor directory describes what the build reads, so a module with one
	// needs neither the cache nor the network.
	if held, ok := s.vendoredModules(m); ok {
		m.Vendored = true
		m.Build = held
		return true
	}
	list, err := mod.BuildList(m.File, s.moduleCache(m), s.moduleFetch(m))
	if err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return false
	}
	m.Build = list
	return true
}

// vendoredModules returns the modules a vendor directory holds, and whether
// the module has one that describes them.
func (s *Session) vendoredModules(m *module) ([]mod.Module, bool) {
	data, err := os.ReadFile(filepath.Join(m.Root, "vendor", "modules.txt"))
	if err != nil {
		return nil, false
	}
	held, err := mod.ParseModulesTxt(data)
	if err != nil {
		return nil, false
	}
	list := make([]mod.Module, 0, len(held))
	for _, v := range held {
		list = append(list, v.Module)
	}
	return list, true
}

// readSum returns the checksum file of a module, or nil when it has none.
func (s *Session) readSum(m *module) *mod.Sum {
	data, err := os.ReadFile(filepath.Join(m.Root, "zyx.sum"))
	if err != nil {
		return nil
	}
	sum, err := mod.ParseSum(data)
	if err != nil {
		return nil
	}
	return sum
}

// moduleFetch returns the function that fills a module the cache does not
// hold. A build fetches only what zyx.sum already fixes by hash, so nothing a
// proxy serves is used before something has recorded its hash.
func (s *Session) moduleFetch(m *module) func(path, version string) error {
	sum := s.readSum(m)
	if sum == nil {
		return nil
	}
	fetcher, err := s.fetcher()
	if err != nil || fetcher == nil {
		return nil
	}
	cache := s.moduleCache(m)
	if cache == nil {
		return nil
	}
	return func(path, version string) error {
		return fetcher.FetchInto(cache, sum, path, version)
	}
}

// moduleCache returns the cache fetched modules live in. A module with no
// requirements reads no cache, so a build leaves no directory behind for a
// module that has none.
func (s *Session) moduleCache(m *module) *mod.ModuleCache {
	if len(m.File.Require) == 0 {
		return nil
	}
	c, err := mod.OpenModuleCache(s.moduleCacheRoot(m))
	if err != nil {
		return nil
	}
	return c
}

// get adds or upgrades a requirement: it resolves the version, fetches the
// module, records the hashes a build fixes it by, and writes the requirement
// into the manifest. Nothing is written until every path has resolved and
// checked, so a failure leaves the manifest and the checksum file as they
// were.
func (s *Session) get(args []string) int {
	o, err := s.parseOptions(args)
	if err != nil {
		return s.usageError(err)
	}
	if len(o.paths) == 0 {
		return s.usageError(fmt.Errorf("get takes at least one module path"))
	}
	m, err := s.findModule(s.Dir)
	if err != nil {
		return s.usageError(err)
	}
	if err := mod.CheckLang(m.File, Version); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	sources, err := mod.ParseSources(s.env("ZYXPROXY"))
	if err != nil {
		return s.usageError(err)
	}
	if len(sources) == 0 {
		fmt.Fprintln(s.Stderr, "zyx: no module source is configured; set ZYXPROXY to a module proxy")
		return exitError
	}
	fetcher := &mod.Fetcher{
		Sources: sources,
		NoProxy: mod.ParsePatterns(s.env("ZYXNOPROXY")),
		Private: mod.ParsePatterns(s.env("ZYXPRIVATE")),
	}
	cache, err := mod.OpenModuleCache(s.moduleCacheRoot(m))
	if err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	checks := s.readSum(m)
	if checks == nil {
		checks = &mod.Sum{}
	}

	type fetched struct {
		path     string
		version  string
		content  string
		manifest string
	}
	var additions []fetched
	for _, spec := range o.paths {
		path, version, err := splitRequirement(spec)
		if err != nil {
			return s.usageError(err)
		}
		if version == "" {
			version, err = fetcher.Latest(path)
			if err != nil {
				fmt.Fprintln(s.Stderr, "zyx:", err)
				return exitError
			}
		}
		content, manifest, err := fetcher.FetchNew(cache, path, version)
		if err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
		additions = append(additions, fetched{path: path, version: version, content: content, manifest: manifest})
	}

	for _, add := range additions {
		checks.Add(add.path, add.version, add.content, add.manifest)
		setRequirement(m.File, add.path, add.version)
	}
	if err := writeAtomically(filepath.Join(m.Root, "zyx.sum"), checks.Format()); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	if err := writeAtomically(filepath.Join(m.Root, "zyx.mod"), m.File.Format()); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	if isDirectory(filepath.Join(m.Root, "vendor")) {
		fmt.Fprintln(s.Stderr, "zyx: a vendor directory is present; run `zyx mod vendor` to sync it")
	}
	return exitOK
}

// splitRequirement reads one argument of get: a module path, with a version
// after an at sign when one is named. The queries go has, which ask for a
// version relative to another, do not exist here.
func splitRequirement(spec string) (path, version string, err error) {
	at := strings.LastIndex(spec, "@")
	if at < 0 {
		return spec, "", nil
	}
	path, version = spec[:at], spec[at+1:]
	if path == "" {
		return "", "", fmt.Errorf("%q names no module", spec)
	}
	switch version {
	case "upgrade", "patch", "none", "latest":
		return "", "", fmt.Errorf("%q asks for %s, which zyx does not support; name a version or leave it out", spec, version)
	}
	if version == "" {
		return "", "", fmt.Errorf("%q has an empty version", spec)
	}
	return path, version, nil
}

// setRequirement writes a requirement into a manifest, replacing what the
// manifest held for the module. Other requirements are left alone.
func setRequirement(f *mod.File, path, version string) {
	for i := range f.Require {
		if f.Require[i].Path == path {
			f.Require[i].Version = version
			return
		}
	}
	f.Require = append(f.Require, mod.Requirement{Path: path, Version: version})
}

// modVendor mirrors the modules a build reads into the module's vendor
// directory, so a build with one runs without the module cache and without
// the network. Only the modules the build and its tests reach are mirrored,
// which is the judgement tidy makes, and the mirror is written afresh so a
// module that is no longer read disappears with the directory that held it.
func (s *Session) modVendor(args []string) int {
	o, err := s.parseOptions(args)
	if err != nil {
		return s.usageError(err)
	}
	m, err := s.findModule(s.Dir)
	if err != nil {
		return s.usageError(err)
	}
	if !s.prepareModule(m) {
		return exitError
	}
	loader := s.loader(m, o)
	// The packages the build reads from each module, over the build graph and
	// the test graph together.
	used := map[string][]string{}
	seen := map[string]bool{}
	for _, testing := range []bool{false, true} {
		load := *loader
		load.Testing = testing
		all, err := load.All("")
		if err != nil {
			return s.usageError(err)
		}
		for _, path := range all {
			graph, err := load.Load(path)
			if err != nil {
				fmt.Fprintln(s.Stderr, "zyx:", err)
				return exitError
			}
			for _, pkg := range graph.Order {
				if s.report(pkg.Diags) {
					return exitError
				}
				if pkg.ImportPath == m.File.Module || strings.HasPrefix(pkg.ImportPath, m.File.Module+"/") {
					continue
				}
				if pkg.ImportPath == "std" || strings.HasPrefix(pkg.ImportPath, "std/") {
					continue
				}
				dep, ok := mod.ModuleOf(m.Build, pkg.ImportPath)
				if !ok {
					fmt.Fprintf(s.Stderr, "zyx: %s is imported but no module provides it; run `zyx get`\n", pkg.ImportPath)
					return exitError
				}
				if !seen[pkg.ImportPath] {
					seen[pkg.ImportPath] = true
					used[dep.Path] = append(used[dep.Path], pkg.ImportPath)
				}
			}
		}
	}

	explicit := map[string]bool{}
	for _, req := range m.File.Require {
		explicit[req.Path] = true
	}
	vendor := filepath.Join(m.Root, "vendor")
	if err := os.RemoveAll(vendor); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	var modules []mod.VendoredModule
	for _, dep := range m.Build {
		packages := used[dep.Path]
		if len(packages) == 0 {
			// A module the build reads nothing from is not mirrored.
			continue
		}
		entry := mod.VendoredModule{Module: dep, Explicit: explicit[dep.Path], Packages: packages}
		if manifest, err := s.moduleManifest(m, dep); err == nil {
			entry.Lang = manifest.Lang
		}
		from, err := s.moduleDir(m, dep)
		if err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
		if err := copyTree(from, filepath.Join(vendor, filepath.FromSlash(dep.Path))); err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
		modules = append(modules, entry)
	}
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	if err := os.WriteFile(filepath.Join(vendor, "modules.txt"), mod.FormatModulesTxt(modules), 0o644); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	return exitOK
}

// moduleDir returns the directory a module's tree is in: the one a
// replacement points at, or the one the cache holds.
func (s *Session) moduleDir(m *module, dep mod.Module) (string, error) {
	if dep.Dir != "" {
		if filepath.IsAbs(dep.Dir) {
			return dep.Dir, nil
		}
		return filepath.Join(m.Root, filepath.FromSlash(dep.Dir)), nil
	}
	cache := s.moduleCache(m)
	if cache == nil {
		return "", fmt.Errorf("module %s %s is not in the cache; run `zyx get %s`", dep.Path, dep.Version, dep.Path)
	}
	dir := cache.Dir(dep.Path, dep.Version)
	if !isDirectory(dir) {
		return "", fmt.Errorf("module %s %s is not in the cache; run `zyx get %s`", dep.Path, dep.Version, dep.Path)
	}
	return dir, nil
}

// moduleManifest returns the manifest of a module a build reads.
func (s *Session) moduleManifest(m *module, dep mod.Module) (*mod.File, error) {
	dir, err := s.moduleDir(m, dep)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "zyx.mod"))
	if err != nil {
		return nil, err
	}
	return mod.Parse(data)
}

// copyTree copies a directory tree.
func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
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
