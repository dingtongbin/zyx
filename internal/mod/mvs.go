// Minimal version selection.

package mod

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// A Graph supplies the requirements of a module version. The module proxy
// fills it in once modules are fetched, so selection runs without the
// network and is testable on its own.
type Graph interface {
	// Requirements returns the requirements a module version declares.
	Requirements(path, version string) ([]Requirement, error)
}

// Select returns the build list of the main module: one version per module
// in the dependency graph, chosen by minimal version selection, with the
// replacements of the main module applied. The result is sorted by path, so
// the same manifest and graph always yield the same list.
func Select(main *File, g Graph) ([]Module, error) {
	if main == nil {
		return nil, fmt.Errorf("zyx.mod: no manifest")
	}
	excluded := map[string]bool{}
	for _, m := range main.Exclude {
		excluded[m.Path+"@"+m.Version] = true
	}
	selected := map[string]string{}
	queue := make([]Module, 0, len(main.Require))
	for _, r := range main.Require {
		queue = append(queue, Module{Path: r.Path, Version: r.Version})
	}
	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]
		if excluded[m.Path+"@"+m.Version] {
			return nil, fmt.Errorf("module %s %s is excluded by this module", m.Path, m.Version)
		}
		if version, ok := selected[m.Path]; ok && Compare(version, m.Version) >= 0 {
			continue
		}
		selected[m.Path] = m.Version
		if g == nil {
			continue
		}
		reqs, err := g.Requirements(m.Path, m.Version)
		if err != nil {
			return nil, err
		}
		for _, r := range reqs {
			queue = append(queue, Module{Path: r.Path, Version: r.Version})
		}
	}
	out := make([]Module, 0, len(selected))
	for path, version := range selected {
		out = append(out, applyReplace(main, Module{Path: path, Version: version}))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// A CacheGraph answers a module version's requirements from the module cache,
// so version selection runs over the whole dependency graph without the
// network: what it reads is what a fetch left there, and what a fetch left
// there was checked against the checksum file.
type CacheGraph struct {
	Cache *ModuleCache
	// Fetch fills a module the cache does not hold. A module it cannot fill
	// stays missing, and selection reports what the build lacks.
	Fetch func(path, version string) error
}

// Requirements returns the requirements a module version declares.
func (g CacheGraph) Requirements(path, version string) ([]Requirement, error) {
	if g.Cache == nil {
		return nil, fmt.Errorf("module %s %s: there is no cache to read it from", path, version)
	}
	f, err := g.Cache.Manifest(path, version)
	if err != nil && g.Fetch != nil {
		if fetchErr := g.Fetch(path, version); fetchErr == nil {
			f, err = g.Cache.Manifest(path, version)
		}
	}
	if err != nil {
		return nil, err
	}
	return f.Require, nil
}

// BuildList returns the modules a build reads: the main module's
// requirements, closed over the requirements of every module they reach,
// chosen by minimal version selection and with the main module's replacements
// applied. Fetch fills a module the cache does not hold, and may be nil. A
// module that is still missing stops selection, because the build would have
// no version to fix it by.
func BuildList(main *File, cache *ModuleCache, fetch func(path, version string) error) ([]Module, error) {
	return Select(main, CacheGraph{Cache: cache, Fetch: fetch})
}

// ModuleOf returns the module an import path belongs to: the longest module
// in a build list whose path is a prefix of it.
func ModuleOf(list []Module, importPath string) (Module, bool) {
	var best Module
	found := false
	for _, m := range list {
		if importPath != m.Path && !strings.HasPrefix(importPath, m.Path+"/") {
			continue
		}
		if !found || len(m.Path) > len(best.Path) {
			best, found = m, true
		}
	}
	return best, found
}

// LocalPath reports whether a module path names a directory rather than a
// module a proxy serves, which is what a replacement to a path in the working
// tree writes.
func LocalPath(path string) bool {
	return path == "." || path == ".." ||
		strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") ||
		filepath.IsAbs(path)
}

// applyReplace applies the replacements of the main module to one selected
// module. A replacement without a version stands for every version. A
// replacement that names a path in the working tree keeps the module's own
// path and carries the directory, so an import path still resolves by the
// name programs write.
func applyReplace(main *File, m Module) Module {
	for _, r := range main.Replace {
		if r.Path != m.Path {
			continue
		}
		if r.Version != "" && r.Version != m.Version {
			continue
		}
		if LocalPath(r.NewPath) {
			m.Dir = r.NewPath
			return m
		}
		m.Path = r.NewPath
		if r.NewVersion != "" {
			m.Version = r.NewVersion
		}
		return m
	}
	return m
}
