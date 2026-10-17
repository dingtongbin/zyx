// The front-end cache: one entry per package, holding the .zyxc text the
// front end produced and the diagnostics it reported. A run whose packages all
// find their entry skips the front end, reads every tree back from the text,
// and prints the diagnostics the entries hold, so a warm build is a cold build
// byte for byte.

package build

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"zyx/internal/cache"
)

// A FrontEndCache reuses the front-end product of a package. Its key is the
// one §9.2 settles: the compiler, the target, the freestanding switch, the
// package's effective sources, the exports hashes of the packages it imports,
// and the module files.
type FrontEndCache struct {
	opts  FrontEndOptions
	cache *cache.Cache
	// root is the module root, which holds zyx.mod and zyx.sum.
	root    string
	modHash string
	sumHash string
	// exports maps an import path onto the hash of the interface it exported,
	// which this run computed or read back.
	exports map[string]string
	hits    int
	misses  int
}

// FrontEndOptions carries what a front-end key is made of beyond the sources.
type FrontEndOptions struct {
	// CacheDir is the directory the cache lives in. An empty directory means
	// the run neither reads nor writes one.
	CacheDir string
	// Compiler identifies the compiler, which every key carries.
	Compiler string
	// Freestanding marks a build that leaves the runtime out. The front end
	// folds the switch, so two builds that differ in it are two products.
	Freestanding bool
}

// OpenFrontEnd prepares the front-end cache of a module. A run with no cache
// directory answers nil, which runs the front end every time.
func OpenFrontEnd(root string, opts FrontEndOptions) *FrontEndCache {
	if opts.CacheDir == "" {
		return nil
	}
	c, err := cache.Open(opts.CacheDir)
	if err != nil {
		// A cache that cannot be opened only costs speed.
		return nil
	}
	f := &FrontEndCache{opts: opts, cache: c, root: root, exports: map[string]string{}}
	f.modHash = hashFile(filepath.Join(root, "zyx.mod"))
	f.sumHash = hashFile(filepath.Join(root, "zyx.sum"))
	return f
}

// hashFile returns the hash of a file's contents, or the empty string when
// there is no file. A module without a checksum file has none to contribute.
func hashFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return cache.HashBytes(data)
}

// begin prepares a run: the interface hashes of an earlier run must not be
// read as the ones this one computed.
func (f *FrontEndCache) begin() {
	f.exports = map[string]string{}
	f.hits, f.misses = 0, 0
}

// key returns the key of one package's entry: everything the front end read
// that decides what its tree holds, in a fixed order, so the same inputs give
// the same key.
func (f *FrontEndCache) key(pkg *Package, target string, imports []string) string {
	k := cache.NewKey()
	k.Add("frontend", f.opts.Compiler, target, strconv.FormatBool(f.opts.Freestanding))
	for _, file := range pkg.Files {
		k.Add("file", file.Name(), cache.HashBytes(file.Data()))
	}
	for _, path := range imports {
		k.Add("exports", path, f.exports[path])
	}
	k.Add("mod", f.modHash, "sum", f.sumHash)
	// The C sources and headers of the package take part in the package as much
	// as its sources do: a header that does not parse is the front end's failure,
	// so its bytes decide the product and belong in the key. Everything of the C
	// family beside them is taken rather than only the headers a source names,
	// because a header includes others and a header of this package may be read
	// that way too.
	for _, input := range cInputs(pkg.Dir) {
		data, err := os.ReadFile(input)
		if err != nil {
			continue
		}
		k.Add("c", filepath.ToSlash(strings.TrimPrefix(input, pkg.Dir)), cache.HashBytes(data))
	}
	return k.Sum()
}

// cInputs answers the C sources and headers of a package directory, in a fixed
// order, so that the same inputs give the same key. A directory that holds none
// answers none.
func cInputs(dir string) []string {
	var out []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".c") || strings.HasSuffix(name, ".h") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil
	}
	sort.Strings(out)
	return out
}

// lookup answers a package's product: the tree's text and the report, when the
// entry is there. An entry that is there but holds no tree is a package whose
// front end reported diagnostics, which is a hit too.
func (f *FrontEndCache) lookup(key string) (text, report []byte, ok bool) {
	out, ok := f.cache.Lookup(key)
	if !ok {
		return nil, nil, false
	}
	if data, err := os.ReadFile(filepath.Join(out, frontEndArtifact)); err == nil {
		text = data
	}
	for _, line := range f.cache.Diagnostics(key) {
		report = append(report, line...)
	}
	return text, report, true
}

// store publishes a package's product. An entry that is already there is left
// alone: its key says its contents are the ones this run produced.
func (f *FrontEndCache) store(key string, text, report []byte, diags []string) {
	if _, _, ok := f.lookup(key); ok {
		return
	}
	entry, err := f.cache.New(key)
	if err != nil {
		return
	}
	if len(text) != 0 {
		if err := os.WriteFile(filepath.Join(entry.Out(), frontEndArtifact), text, 0o644); err != nil {
			entry.Discard()
			return
		}
		if err := entry.Add(frontEndArtifact); err != nil {
			entry.Discard()
			return
		}
	}
	for _, diag := range diags {
		entry.Diagnostic(diag)
	}
	if _, _, err := entry.Publish(); err != nil {
		entry.Discard()
	}
}

// frontEndArtifact is the name of the tree one package's entry holds.
const frontEndArtifact = "module.zyxc"

// Stats answers how many entries this run read and how many it had to build.
func (f *FrontEndCache) Stats() (hits, misses int) {
	if f == nil {
		return 0, 0
	}
	return f.hits, f.misses
}

// transitiveImports returns the import paths a package reaches, itself
// excluded, in byte order. The key covers the whole set, so a package also
// depends on the interface of what its imports import.
func transitiveImports(pkg *Package, packages map[string]*Package) []string {
	seen := map[string]bool{}
	var walk func(path string)
	walk = func(path string) {
		next, ok := packages[path]
		if !ok {
			return
		}
		for _, dep := range next.Imports {
			if seen[dep] {
				continue
			}
			seen[dep] = true
			walk(dep)
		}
	}
	for _, dep := range pkg.Imports {
		if seen[dep] {
			continue
		}
		seen[dep] = true
		walk(dep)
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
