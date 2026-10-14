// Package mod implements the go-style module system: the zyx.mod manifest,
// minimal version selection, and the checksum file. The rules match the go
// module system with only the manifest and prefix names changed.

package mod

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// A File is a parsed zyx.mod.
type File struct {
	// Module is the module path this manifest declares.
	Module string
	// Lang is the language version the zyx directive declares.
	Lang string
	// Require lists the required modules.
	Require []Requirement
	// Replace lists the replacements, applied after selection.
	Replace []Replacement
	// Exclude lists the module versions this module refuses to use.
	Exclude []Module
	// Retract lists the version ranges of this module that are withdrawn.
	Retract []Retraction
}

// A Requirement is one require directive.
type Requirement struct {
	Path    string
	Version string
}

// A Replacement is one replace directive. An empty Version replaces every
// version of Path.
type Replacement struct {
	Path       string
	Version    string
	NewPath    string
	NewVersion string
}

// A Module names a module at a version.
type Module struct {
	Path    string
	Version string
}

// A Retraction marks a range of this module's versions as withdrawn.
type Retraction struct {
	Low, High string
	Rationale string
}

// Parse reads a zyx.mod. Directives may be written one per line or inside a
// parenthesized block, and a trailing comment starts with two slashes.
func Parse(data []byte) (*File, error) {
	f := &File{}
	lines := strings.Split(string(data), "\n")
	for i := 0; i < len(lines); i++ {
		fields, comment := directiveLine(lines[i])
		if len(fields) == 0 {
			continue
		}
		verb := fields[0]
		if len(fields) > 1 && fields[1] == "(" {
			entries, end, err := blockLines(lines, i, verb)
			if err != nil {
				return nil, err
			}
			i = end
			for _, entry := range entries {
				if err := f.apply(verb, entry.fields, entry.comment); err != nil {
					return nil, err
				}
			}
			continue
		}
		if err := f.apply(verb, fields[1:], comment); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// directiveLine splits one line into its fields and its trailing comment.
func directiveLine(line string) ([]string, string) {
	comment := ""
	if idx := strings.Index(line, "//"); idx >= 0 {
		comment = strings.TrimSpace(line[idx+2:])
		line = line[:idx]
	}
	return strings.Fields(line), comment
}

// A blockEntry is one line inside a parenthesized directive block.
type blockEntry struct {
	fields  []string
	comment string
}

// blockLines collects the entries of a `verb (` block, which ends at a line
// holding a closing parenthesis.
func blockLines(lines []string, start int, verb string) ([]blockEntry, int, error) {
	var entries []blockEntry
	for i := start + 1; i < len(lines); i++ {
		fields, comment := directiveLine(lines[i])
		if len(fields) == 0 {
			continue
		}
		if fields[0] == ")" {
			return entries, i, nil
		}
		entries = append(entries, blockEntry{fields: fields, comment: comment})
	}
	return nil, 0, fmt.Errorf("zyx.mod: %s block is not closed", verb)
}

// apply records one directive.
func (f *File) apply(verb string, fields []string, comment string) error {
	switch verb {
	case "module":
		if len(fields) != 1 {
			return fmt.Errorf("zyx.mod: module needs one path")
		}
		f.Module = fields[0]
	case "zyx":
		if len(fields) != 1 {
			return fmt.Errorf("zyx.mod: zyx needs one version")
		}
		f.Lang = fields[0]
	case "require":
		req, err := parseRequirement(fields)
		if err != nil {
			return err
		}
		f.Require = append(f.Require, req)
	case "exclude":
		req, err := parseRequirement(fields)
		if err != nil {
			return err
		}
		f.Exclude = append(f.Exclude, Module{Path: req.Path, Version: req.Version})
	case "replace":
		rep, err := parseReplacement(fields)
		if err != nil {
			return err
		}
		f.Replace = append(f.Replace, rep)
	case "retract":
		ret, err := parseRetraction(fields)
		if err != nil {
			return err
		}
		ret.Rationale = comment
		f.Retract = append(f.Retract, ret)
	default:
		return fmt.Errorf("zyx.mod: unknown directive %q", verb)
	}
	return nil
}

// parseRequirement reads `path version`.
func parseRequirement(fields []string) (Requirement, error) {
	if len(fields) != 2 {
		return Requirement{}, fmt.Errorf("zyx.mod: require needs a path and a version")
	}
	if _, err := ParseVersion(fields[1]); err != nil {
		return Requirement{}, err
	}
	return Requirement{Path: fields[0], Version: fields[1]}, nil
}

// parseReplacement reads `old (version)? => new (version)?`.
func parseReplacement(fields []string) (Replacement, error) {
	arrow := -1
	for i, field := range fields {
		if field == "=>" {
			arrow = i
			break
		}
	}
	if arrow < 0 {
		return Replacement{}, fmt.Errorf("zyx.mod: replace needs =>")
	}
	left, right := fields[:arrow], fields[arrow+1:]
	rep := Replacement{}
	switch len(left) {
	case 1:
		rep.Path = left[0]
	case 2:
		rep.Path, rep.Version = left[0], left[1]
	default:
		return Replacement{}, fmt.Errorf("zyx.mod: replace needs an old module")
	}
	switch len(right) {
	case 1:
		rep.NewPath = right[0]
	case 2:
		rep.NewPath, rep.NewVersion = right[0], right[1]
	default:
		return Replacement{}, fmt.Errorf("zyx.mod: replace needs a new module")
	}
	return rep, nil
}

// parseRetraction reads `version` or `[low, high]`, with an optional
// rationale comment that the field splitter has already dropped.
func parseRetraction(fields []string) (Retraction, error) {
	if len(fields) == 0 {
		return Retraction{}, fmt.Errorf("zyx.mod: retract needs a version")
	}
	if strings.HasPrefix(fields[0], "[") {
		joined := strings.Join(fields, " ")
		inner := strings.TrimSuffix(strings.TrimPrefix(joined, "["), "]")
		parts := strings.Split(inner, ",")
		if len(parts) != 2 {
			return Retraction{}, fmt.Errorf("zyx.mod: retract range needs two versions")
		}
		low, high := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if _, err := ParseVersion(low); err != nil {
			return Retraction{}, err
		}
		if _, err := ParseVersion(high); err != nil {
			return Retraction{}, err
		}
		return Retraction{Low: low, High: high}, nil
	}
	if _, err := ParseVersion(fields[0]); err != nil {
		return Retraction{}, err
	}
	return Retraction{Low: fields[0], High: fields[0]}, nil
}

// Format renders the manifest in canonical form: one directive per line,
// requirements, exclusions, and replacements sorted by path, and no
// trailing space.
func (f *File) Format() []byte {
	var b strings.Builder
	if f.Module != "" {
		fmt.Fprintf(&b, "module %s\n", f.Module)
	}
	if f.Lang != "" {
		fmt.Fprintf(&b, "zyx %s\n", f.Lang)
	}
	reqs := append([]Requirement(nil), f.Require...)
	sort.Slice(reqs, func(i, j int) bool { return reqs[i].Path < reqs[j].Path })
	for _, r := range reqs {
		fmt.Fprintf(&b, "require %s %s\n", r.Path, r.Version)
	}
	excludes := append([]Module(nil), f.Exclude...)
	sort.Slice(excludes, func(i, j int) bool { return excludes[i].Path < excludes[j].Path })
	for _, m := range excludes {
		fmt.Fprintf(&b, "exclude %s %s\n", m.Path, m.Version)
	}
	reps := append([]Replacement(nil), f.Replace...)
	sort.Slice(reps, func(i, j int) bool { return reps[i].Path < reps[j].Path })
	for _, r := range reps {
		old, next := r.Path, r.NewPath
		if r.Version != "" {
			old += " " + r.Version
		}
		if r.NewVersion != "" {
			next += " " + r.NewVersion
		}
		fmt.Fprintf(&b, "replace %s => %s\n", old, next)
	}
	for _, r := range f.Retract {
		line := "retract " + r.Low
		if r.Low != r.High {
			line = fmt.Sprintf("retract [%s, %s]", r.Low, r.High)
		}
		if r.Rationale != "" {
			line += " // " + r.Rationale
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// A Version is a semantic version with an optional pre-release and build
// metadata.
type Version struct {
	// Major, Minor, and Patch are the numeric parts.
	Major, Minor, Patch int
	// Pre holds the pre-release identifier, empty when there is none.
	Pre string
	// Build holds the build metadata, empty when there is none.
	Build string
}

// ParseVersion parses a semantic version, including the pseudo-version form
// an untagged commit uses.
func ParseVersion(s string) (Version, error) {
	v := Version{}
	rest := strings.TrimPrefix(s, "v")
	if rest == s {
		return v, fmt.Errorf("version %q must start with v", s)
	}
	if i := strings.IndexByte(rest, '+'); i >= 0 {
		v.Build, rest = rest[i+1:], rest[:i]
	}
	if i := strings.IndexByte(rest, '-'); i >= 0 {
		v.Pre, rest = rest[i+1:], rest[:i]
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("version %q needs major, minor, and patch", s)
	}
	nums := []*int{&v.Major, &v.Minor, &v.Patch}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return v, fmt.Errorf("version %q has a malformed number", s)
		}
		*nums[i] = n
	}
	return v, nil
}

// Compare orders two versions: it returns a negative number when a sorts
// before b, zero when they are equal, and a positive number otherwise. A
// pre-release sorts before the release it belongs to.
func Compare(a, b string) int {
	va, erra := ParseVersion(a)
	vb, errb := ParseVersion(b)
	switch {
	case erra != nil || errb != nil:
		return strings.Compare(a, b)
	}
	if c := compareInt(va.Major, vb.Major); c != 0 {
		return c
	}
	if c := compareInt(va.Minor, vb.Minor); c != 0 {
		return c
	}
	if c := compareInt(va.Patch, vb.Patch); c != 0 {
		return c
	}
	switch {
	case va.Pre == vb.Pre:
		return 0
	case va.Pre == "":
		return 1
	case vb.Pre == "":
		return -1
	}
	return strings.Compare(va.Pre, vb.Pre)
}

// compareInt orders two integers.
func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
