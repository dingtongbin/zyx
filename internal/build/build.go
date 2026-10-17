// Package build orchestrates the compilation pipeline: it collects a
// package's source files from a directory, parses them, and walks the
// import graph in dependency order.

package build

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"zyx/internal/ast"
	"zyx/internal/cheader"
	"zyx/internal/check"
	"zyx/internal/coro"
	"zyx/internal/diag"
	"zyx/internal/marks"
	"zyx/internal/mod"
	"zyx/internal/resolve"
	"zyx/internal/source"
	"zyx/internal/toolchain"
	"zyx/internal/zyxc"
)

// A Package is one package's collected sources.
type Package struct {
	// ImportPath is the path other packages import this one by.
	ImportPath string
	// Dir is the directory the files were read from.
	Dir string
	// Name is the package name every file declares.
	Name string
	// Files holds the sources in file-name byte order.
	Files []*source.File
	// Syntax holds the parsed files, parallel to Files.
	Syntax []*ast.File
	// Diags holds the diagnostics of reading, lexing, and parsing.
	Diags []diag.Diagnostic
	// Imports lists the ordinary import paths, sorted and deduplicated.
	Imports []string
	// CHeaders lists the C header paths, sorted and deduplicated.
	CHeaders []string
	// CSources lists the C sources of the package, as full paths in
	// file-name byte order. Each is a translation unit of its own, compiled
	// by the same compiler and linked beside the objects of the package.
	CSources []string
}

// A Loader collects packages from a module tree.
type Loader struct {
	// Root is the module root directory.
	Root string
	// ModulePath is the module path declared by zyx.mod.
	ModulePath string
	// StdRoot is the directory that holds the standard library tree. A
	// std/ import path resolves there and needs no requirement.
	StdRoot string
	// Target selects which platform-suffixed files take part.
	Target toolchain.Target
	// Testing includes test files, which an ordinary build leaves out.
	Testing bool
	// Requires holds the modules a build reads, keyed by module path. An
	// import path outside the module and the standard library resolves to the
	// longest requirement that is a prefix of it.
	Requires map[string]string
	// Local holds the module paths a replacement points at a directory of the
	// working tree for, relative to the module root.
	Local map[string]string
	// Modules is the cache the required modules were fetched into.
	Modules *mod.ModuleCache
	// Vendor is the module's vendor directory, which takes precedence over
	// the cache when it exists.
	Vendor string
}

// A Graph is a module's packages in dependency order.
type Graph struct {
	// Entry is the import path the load started from.
	Entry string
	// Order lists the packages so that every package follows the packages
	// it imports.
	Order []*Package
}

// Load collects the entry package and everything it imports.
func (l *Loader) Load(importPath string) (*Graph, error) {
	g := &Graph{Entry: importPath}
	state := map[string]loadState{}
	var order []*Package
	var visit func(string, []string) error
	visit = func(p string, stack []string) error {
		switch state[p] {
		case loadDone:
			return nil
		case loadLoading:
			return fmt.Errorf("import cycle: %s", strings.Join(append(stack, p), " -> "))
		}
		state[p] = loadLoading
		pkg, err := l.LoadPackage(p)
		if err != nil {
			return err
		}
		for _, dep := range pkg.Imports {
			l.checkImport(pkg, dep)
			if err := visit(dep, append(stack, p)); err != nil {
				return err
			}
		}
		state[p] = loadDone
		order = append(order, pkg)
		return nil
	}
	if err := visit(importPath, nil); err != nil {
		return nil, err
	}
	g.Order = order
	return g, nil
}

// loadState tracks one package while the import graph is walked.
type loadState uint8

const (
	loadUnvisited loadState = iota
	loadLoading
	loadDone
)

// LoadPackage collects and parses the package at a directory.
func (l *Loader) LoadPackage(importPath string) (*Package, error) {
	dir, err := l.Resolve(importPath)
	if err != nil {
		return nil, err
	}
	names, err := sourceNames(dir, l.Target, l.Testing)
	if err != nil {
		return nil, err
	}
	cnames, err := cSourceNames(dir, l.Target)
	if err != nil {
		return nil, err
	}
	pkg := &Package{ImportPath: importPath, Dir: dir}
	for _, name := range cnames {
		pkg.CSources = append(pkg.CSources, filepath.Join(dir, name))
	}
	if len(names) == 0 {
		// A package whose every file is excluded by the target is empty,
		// which the language allows. It may still carry C sources.
		return pkg, nil
	}
	files, err := l.readFiles(dir, names)
	if err != nil {
		return nil, err
	}
	pkg.Files = files
	pkg.Syntax, pkg.Diags = parseFiles(files, nil)
	pkg.Name, pkg.Imports, pkg.CHeaders = summarize(pkg.Syntax)
	return pkg, nil
}

// readFiles reads the named files of one package.
func (l *Loader) readFiles(dir string, names []string) ([]*source.File, error) {
	files := make([]*source.File, 0, len(names))
	for _, name := range names {
		full := filepath.Join(dir, name)
		data, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", name, err)
		}
		files = append(files, source.NewFile(l.relName(full), data))
	}
	return files, nil
}

// relName returns the path a diagnostic and the intermediate form name a
// file by. It is relative to the tree the file was found in, never absolute
// and never reaching above that tree, so the same source names itself the
// same way wherever the checkout sits.
func (l *Loader) relName(full string) string {
	if l.StdRoot != "" {
		if rel, err := filepath.Rel(l.StdRoot, full); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	if rel, err := filepath.Rel(l.Root, full); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(full)
}

// summarize returns the package name and the import lists the parsed files
// declare. The first file wins for the name, and resolution reports a
// mismatch between files.
func summarize(files []*ast.File) (string, []string, []string) {
	name := ""
	imports := map[string]bool{}
	headers := map[string]bool{}
	for _, f := range files {
		if f == nil {
			continue
		}
		if name == "" && f.Package != nil && f.Package.Name != nil {
			name = f.Package.Name.Name
		}
		for _, imp := range f.Imports {
			if imp.CHeader {
				headers[imp.Path] = true
				continue
			}
			imports[imp.Path] = true
		}
	}
	return name, sortedKeys(imports), sortedKeys(headers)
}

// sortedKeys returns the keys of a set in byte order.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// Resolve maps an import path to a directory: a std path resolves inside
// the standard library tree, and every other path must name a directory
// inside the module.
func (l *Loader) Resolve(importPath string) (string, error) {
	if !validImportPath(importPath) {
		return "", fmt.Errorf("import path %q is not a full module path", importPath)
	}
	if isStdPath(importPath) {
		if l.StdRoot == "" {
			return "", fmt.Errorf("import %q needs the standard library tree", importPath)
		}
		return filepath.Join(l.StdRoot, filepath.FromSlash(strings.TrimPrefix(importPath, "std/"))), nil
	}
	if l.ModulePath != "" && (importPath == l.ModulePath || strings.HasPrefix(importPath, l.ModulePath+"/")) {
		rel := strings.TrimPrefix(strings.TrimPrefix(importPath, l.ModulePath), "/")
		return filepath.Join(l.Root, filepath.FromSlash(rel)), nil
	}
	// A vendor directory is a mirror of the modules the build needs, so it
	// answers first and a build with one runs without the cache (§12.3).
	if dir, ok := l.vendorDir(importPath); ok {
		return dir, nil
	}
	if dir, ok := l.requiredDir(importPath); ok {
		return dir, nil
	}
	return "", fmt.Errorf("import %q is not provided by module %s", importPath, l.ModulePath)
}

// vendorDir returns where an import path resolves in the module's vendor
// directory, if the module has one and the package is in it.
func (l *Loader) vendorDir(importPath string) (string, bool) {
	if l.Vendor == "" {
		return "", false
	}
	dir := filepath.Join(l.Vendor, filepath.FromSlash(importPath))
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, true
	}
	return "", false
}

// requiredDir returns where an import path resolves among the required
// modules, which live in the module cache under their path and version. A
// module a replacement points at a directory of the working tree resolves
// there instead.
func (l *Loader) requiredDir(importPath string) (string, bool) {
	path, version, ok := l.moduleOf(importPath)
	if !ok {
		return "", false
	}
	rel := filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(importPath, path), "/"))
	if dir, ok := l.Local[path]; ok {
		return resolveUnder(l.Root, dir, rel)
	}
	if l.Modules == nil {
		return "", false
	}
	return resolveUnder("", l.Modules.Dir(path, version), rel)
}

// resolveUnder returns a directory under a base, and whether it is there. A
// relative base is taken from the module root, which is where a replacement
// writes it.
func resolveUnder(root, base, rel string) (string, bool) {
	if base == "" {
		return "", false
	}
	if !filepath.IsAbs(base) {
		base = filepath.Join(root, filepath.FromSlash(base))
	}
	dir := filepath.Join(base, rel)
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, true
	}
	return "", false
}

// moduleOf returns the module an import path belongs to: the longest
// requirement whose path is a prefix of it, which is what tells
// example.com/dep/tool apart from example.com/dep.
func (l *Loader) moduleOf(importPath string) (path, version string, ok bool) {
	for candidate, candidateVersion := range l.Requires {
		if importPath != candidate && !strings.HasPrefix(importPath, candidate+"/") {
			continue
		}
		if len(candidate) > len(path) {
			path, version, ok = candidate, candidateVersion, true
		}
	}
	for candidate := range l.Local {
		if importPath != candidate && !strings.HasPrefix(importPath, candidate+"/") {
			continue
		}
		if len(candidate) > len(path) {
			path, version, ok = candidate, "", true
		}
	}
	return path, version, ok
}

// validImportPath reports whether a path is a full module path. The
// language has no relative or absolute file imports.
func validImportPath(p string) bool {
	if p == "" || strings.HasPrefix(p, ".") || strings.HasPrefix(p, "/") {
		return false
	}
	return !strings.ContainsAny(p, "\\:")
}

// isStdPath reports whether an import path names the standard library,
// which needs no requirement.
func isStdPath(p string) bool {
	return p == "std" || strings.HasPrefix(p, "std/")
}

// Importable reports whether a package may import another one. A path with
// an internal segment is only importable from inside the subtree that
// holds it.
func Importable(fromDir, toPath string) bool {
	segments := strings.Split(toPath, "/")
	cut := -1
	for i, seg := range segments {
		if seg == "internal" {
			cut = i
			break
		}
	}
	if cut < 0 {
		return true
	}
	prefix := path.Join(segments[:cut]...)
	from := path.Clean(fromDir)
	if prefix == "" || prefix == "." {
		return true
	}
	return from == prefix || strings.HasPrefix(from, prefix+"/")
}

// checkImport reports an import a package may not make. The internal rule is
// the one that can fail: the import is not there for the checker to see,
// because the loader refuses to reach the package at all.
func (l *Loader) checkImport(pkg *Package, dep string) {
	if Importable(pkg.ImportPath, dep) {
		return
	}
	at := pkg.ImportSpan(dep)
	pkg.Diags = append(pkg.Diags, diag.New(diag.CodeImportInvalid, at,
		"%s is internal to %s", dep, internalRoot(dep)).
		WithHelp("move the import inside that subtree, or move the package out of it"))
}

// ImportSpan returns where a package writes an import of a path, so a
// diagnostic about it points at the line the program wrote.
func (p *Package) ImportSpan(path string) source.Span {
	for _, file := range p.Syntax {
		if file == nil {
			continue
		}
		for _, imp := range file.Imports {
			if !imp.CHeader && imp.Path == path {
				return imp.Span()
			}
		}
	}
	for _, file := range p.Syntax {
		if file != nil && file.Package != nil && file.Package.Name != nil {
			return file.Package.Name.Sp
		}
	}
	return source.Span{}
}

// internalRoot returns the subtree a path with an internal segment belongs
// to, which is the part before the segment.
func internalRoot(path string) string {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		if seg == "internal" {
			return strings.Join(segments[:i], "/")
		}
	}
	return path
}

// A Result is the front-end result of one package.
type Result struct {
	// Package is the collected package.
	Package *Package
	// Resolved is the resolution result, nil when resolution failed.
	Resolved *resolve.Info
	// Checked is the type-check result, nil when checking failed.
	Checked *check.Package
	// Marks is the memory mark model, nil when marking failed.
	Marks *marks.Info
	// IR is the .zyxc tree, nil when the package did not reach emission.
	IR *zyxc.File
	// Text is the canonical .zyxc text.
	Text []byte
	// Diags holds every diagnostic the package reported.
	Diags []diag.Diagnostic
	// Report holds the diagnostics as the command line prints them, so a
	// result that came from the cache replays them byte for byte.
	Report []byte
}

// Exports returns the hash of the package's export interface, or the empty
// string when the package produced no tree.
func (r *Result) Exports() string {
	if r == nil || r.IR == nil {
		return ""
	}
	return r.IR.Exports
}

// A Session runs the front end over a module tree.
type Session struct {
	// Loader collects the packages.
	Loader *Loader
	// Cache reuses the front-end product of a package. A nil cache runs the
	// front end every time.
	Cache *FrontEndCache
	// Freestanding marks a build that leaves the runtime out, which the
	// checker rejects constructs against. It is carried here rather than read
	// off the cache, because a run without one still has to reach the checker.
	Freestanding bool
}

// FrontEnd runs the front end over the entry package and everything it
// imports, in dependency order, and returns one result per package. A
// package whose earlier stage failed keeps its diagnostics and stops there,
// so the importer sees a missing interface rather than a cascade.
//
// A session with a cache reads the product of a package whose inputs did not
// change: the tree comes back from the text the front end wrote, and the
// diagnostics come back as they were printed. A run whose packages all find
// their entry therefore skips the front end entirely, which is what makes a
// warm build the same bytes as a cold one.
func (s *Session) FrontEnd(entry string) ([]*Result, error) {
	g, err := s.Loader.Load(entry)
	if err != nil {
		return nil, err
	}
	packages := make(map[string]*Package, len(g.Order))
	for _, pkg := range g.Order {
		packages[pkg.ImportPath] = pkg
	}
	if s.Cache != nil {
		if out, ok := s.frontEndCached(g, packages); ok {
			return out, nil
		}
	}
	resolveIfaces := map[string]*resolve.Interface{}
	checkIfaces := map[string]*check.Interface{}
	out := make([]*Result, 0, len(g.Order))
	for _, pkg := range g.Order {
		res := s.frontEndPackage(pkg, resolveIfaces, checkIfaces)
		res.Report = renderReport(res.Diags)
		if s.Cache != nil {
			s.storeResult(pkg, packages, res)
		}
		out = append(out, res)
		if res.Resolved != nil {
			resolveIfaces[pkg.ImportPath] = res.Resolved.Interface
		}
		if res.Checked != nil {
			checkIfaces[pkg.ImportPath] = res.Checked.Interface
		}
	}
	return out, nil
}

// frontEndCached answers the whole run from the cache, and reports whether
// every package's entry was there. A package whose entry is missing makes the
// run the front end's own: a package's analysis needs the interfaces of the
// packages it imports, and an entry holds a tree rather than an interface.
func (s *Session) frontEndCached(g *Graph, packages map[string]*Package) ([]*Result, bool) {
	f := s.Cache
	f.begin()
	out := make([]*Result, 0, len(g.Order))
	for _, pkg := range g.Order {
		key := f.key(pkg, s.Loader.Target.Triple, transitiveImports(pkg, packages))
		text, report, ok := f.lookup(key)
		if !ok {
			return nil, false
		}
		res := &Result{Package: pkg, Text: text, Report: report}
		if len(text) != 0 {
			file, err := zyxc.Parse(text)
			if err != nil {
				// A damaged entry is a miss: the run builds the tree again,
				// and the entry is replaced by what that run produced.
				return nil, false
			}
			res.IR = file
		}
		f.hits++
		f.exports[pkg.ImportPath] = res.Exports()
		out = append(out, res)
	}
	return out, true
}

// storeResult publishes one package's product. A key that already has an entry
// is left alone: an unchanged package is not written again, so a downstream
// entry keeps the exports hash the interface it was built against has.
func (s *Session) storeResult(pkg *Package, packages map[string]*Package, res *Result) {
	f := s.Cache
	key := f.key(pkg, s.Loader.Target.Triple, transitiveImports(pkg, packages))
	f.misses++
	diags := make([]string, 0, len(res.Diags))
	for _, d := range res.Diags {
		diags = append(diags, d.RenderString())
	}
	f.store(key, res.Text, res.Report, diags)
	f.exports[pkg.ImportPath] = res.Exports()
}

// renderReport renders the diagnostics the way the command line prints them,
// ordered by source position so that two runs of the same program report the
// same bytes.
func renderReport(diags []diag.Diagnostic) []byte {
	if len(diags) == 0 {
		return nil
	}
	sorted := make([]diag.Diagnostic, len(diags))
	copy(sorted, diags)
	sort.SliceStable(sorted, func(i, j int) bool {
		afi, ali, aci := position(sorted[i])
		bfi, bli, bci := position(sorted[j])
		if afi != bfi {
			return afi < bfi
		}
		if ali != bli {
			return ali < bli
		}
		return aci < bci
	})
	var b []byte
	for _, d := range sorted {
		b = append(b, d.RenderString()...)
	}
	return b
}

// position returns the source position of a diagnostic, and zeroes for one
// that covers a whole build rather than one place in a file.
func position(d diag.Diagnostic) (file string, line, col int) {
	if !d.Span.IsValid() {
		return "", 0, 0
	}
	line, col = d.Span.StartPos()
	return d.Span.File.Name(), line, col
}

// frontEndPackage runs every front-end stage over one package.
func (s *Session) frontEndPackage(pkg *Package, resolveIfaces map[string]*resolve.Interface, checkIfaces map[string]*check.Interface) *Result {
	res := &Result{Package: pkg}
	res.Diags = append(res.Diags, pkg.Diags...)
	if len(pkg.Diags) != 0 {
		// A file the lexer or the parser could not read leaves a tree that is
		// only partly there, so the stages that read it are not run. The
		// diagnostics already say what is wrong.
		return res
	}

	rpkg := &resolve.Package{Path: pkg.ImportPath, Files: pkg.Syntax}
	rinfo, rdiags := resolve.Resolve(rpkg, resolveDeps(resolveIfaces))
	res.Resolved = rinfo
	res.Diags = append(res.Diags, rdiags...)
	if len(rdiags) != 0 {
		return res
	}

	cdecls, hdiags := s.cTables(pkg)
	res.Diags = append(res.Diags, hdiags...)
	if len(hdiags) != 0 {
		return res
	}

	checked, cdiags := check.Check(rpkg, rinfo, checkDeps(checkIfaces), s.Loader.Target, cdecls, check.Options{Freestanding: s.Freestanding})
	res.Checked = checked
	res.Diags = append(res.Diags, cdiags...)
	if len(cdiags) != 0 {
		return res
	}

	cinfo := coro.Build(rpkg)
	model, mdiags := marks.Build(rpkg, rinfo, checked, cinfo)
	res.Marks = model
	res.Diags = append(res.Diags, mdiags...)
	if len(mdiags) != 0 {
		return res
	}

	ir, err := zyxc.Build(rpkg, rinfo, checked, model, cinfo, s.Loader.Target.Triple, cdecls)
	if err != nil {
		res.Diags = append(res.Diags, diag.Diagnostic{Code: diag.CodeEmission, Message: err.Error()})
		return res
	}
	res.IR = ir
	res.Text = zyxc.Format(ir)
	return res
}

// cTables reads the C headers a package imported. The declarations come from
// clang, which owns the C language, so the checker sees what a header
// actually declares rather than what a name suggests.
func (s *Session) cTables(pkg *Package) (map[string]*cheader.Table, []diag.Diagnostic) {
	if len(pkg.CHeaders) == 0 {
		return nil, nil
	}
	tc, err := toolchain.Locate()
	if err != nil {
		return nil, []diag.Diagnostic{{Code: diag.CodeCHeader, Message: err.Error()}}
	}
	includes := append([]string{pkg.Dir}, s.runtimeIncludes()...)
	out := map[string]*cheader.Table{}
	for _, header := range pkg.CHeaders {
		table, err := cheader.Parse(tc.Clang, header, includes)
		if err != nil {
			return nil, []diag.Diagnostic{{Code: diag.CodeCHeader, Message: err.Error()}}
		}
		out[header] = table
	}
	return out, nil
}

// runtimeIncludes returns the directories the C boundary searches for the
// runtime's own headers.
func (s *Session) runtimeIncludes() []string {
	tc, err := toolchain.Locate()
	if err != nil {
		return nil
	}
	var out []string
	for _, flag := range tc.IncludeFlags() {
		out = append(out, strings.TrimPrefix(flag, "-I"))
	}
	return out
}

// resolveDeps adapts a table of resolved interfaces to the resolver.
type resolveDeps map[string]*resolve.Interface

func (d resolveDeps) Lookup(path string) (*resolve.Interface, bool) {
	itf, ok := d[path]
	return itf, ok
}

// checkDeps adapts a table of checked interfaces to the checker.
type checkDeps map[string]*check.Interface

func (d checkDeps) Lookup(path string) (*check.Interface, bool) {
	itf, ok := d[path]
	return itf, ok
}

// All returns the import paths of every package under a prefix, in byte
// order. The prefix is either a module-relative path or a std path; an
// empty prefix means the whole module.
func (l *Loader) All(prefix string) ([]string, error) {
	root, base := l.Root, l.ModulePath
	if isStdPath(prefix) {
		if l.StdRoot == "" {
			return nil, fmt.Errorf("the standard library tree is not available")
		}
		root, base = l.StdRoot, "std"
	} else if prefix != "" && prefix != "." {
		if !strings.HasPrefix(prefix, l.ModulePath) {
			return nil, fmt.Errorf("%s is outside module %s", prefix, l.ModulePath)
		}
		base = strings.TrimSuffix(prefix, "/")
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(base, l.ModulePath), "/")
	dir := filepath.Join(root, filepath.FromSlash(rel))

	var out []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if path != dir && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor") {
			return filepath.SkipDir
		}
		has, err := hasSource(path)
		if err != nil {
			return err
		}
		if !has {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		importPath := base
		if rel != "." {
			importPath = base + "/" + filepath.ToSlash(rel)
		}
		out = append(out, importPath)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// cSourceNames returns the C source names of a directory that take part in a
// build for the target, in byte order. A C file follows the same platform
// rule a zyx file does, so a package can keep one implementation per target.
func cSourceNames(dir string, target toolchain.Target) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".c") {
			continue
		}
		if include, _ := fileFor(entry.Name(), ".c", target); !include {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// hasSource reports whether a directory holds at least one .zyx file.
func hasSource(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("cannot read %s: %w", dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".zyx") {
			return true, nil
		}
	}
	return false, nil
}

// sourceNames returns the source file names of a directory that take part
// in a build for the target, in byte order.
func sourceNames(dir string, target toolchain.Target, testing bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".zyx") {
			continue
		}
		include, test := fileFor(entry.Name(), ".zyx", target)
		if !include || (test && !testing) {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// fileFor reports whether a source file takes part in a build for the
// target and whether it is a test file. A name whose trailing segment is
// neither a platform nor "test" is an ordinary file name.
func fileFor(name, ext string, target toolchain.Target) (include, test bool) {
	stem := strings.TrimSuffix(name, ext)
	i := strings.LastIndex(stem, "_")
	if i < 0 {
		return true, false
	}
	suffix := stem[i+1:]
	if suffix == "test" {
		test = true
		stem = stem[:i]
		i = strings.LastIndex(stem, "_")
		if i < 0 {
			return true, test
		}
		suffix = stem[i+1:]
	}
	// A pair of segments names a platform and an architecture together, so
	// it is checked before either one alone.
	if j := strings.LastIndex(stem[:i], "_"); j >= 0 {
		os, arch := stem[j+1:i], suffix
		if isOSName(os) && isArchName(arch) {
			return os == target.OS && arch == target.Arch, test
		}
	}
	if isOSName(suffix) {
		return suffix == target.OS, test
	}
	if isArchName(suffix) {
		return suffix == target.Arch, test
	}
	return true, test
}

// isOSName reports whether a segment names one of the platforms.
func isOSName(name string) bool {
	for _, os := range toolchain.OperatingSystems() {
		if os == name {
			return true
		}
	}
	return false
}

// isArchName reports whether a segment names one of the architectures.
func isArchName(name string) bool {
	for _, arch := range toolchain.Architectures() {
		if arch == name {
			return true
		}
	}
	return false
}
