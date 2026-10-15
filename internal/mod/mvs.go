// Minimal version selection.

package mod

import (
	"fmt"
	"sort"
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

// applyReplace applies the replacements of the main module to one selected
// module. A replacement without a version stands for every version.
func applyReplace(main *File, m Module) Module {
	for _, r := range main.Replace {
		if r.Path != m.Path {
			continue
		}
		if r.Version != "" && r.Version != m.Version {
			continue
		}
		m.Path = r.NewPath
		if r.NewVersion != "" {
			m.Version = r.NewVersion
		}
		return m
	}
	return m
}
