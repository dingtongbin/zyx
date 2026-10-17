// The backend driver: from the front end's trees to an executable.

package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"zyx/internal/cache"
	"zyx/internal/llvmgen"
	"zyx/internal/mono"
	"zyx/internal/toolchain"
	"zyx/internal/zyxc"
)

// openCache opens the cache a build reuses artifacts from. An empty
// directory means the build neither reads nor writes one, and a cache that
// cannot be opened only costs speed, so the build goes on without it.
func openCache(root string) *cache.Cache {
	if root == "" {
		return nil
	}
	c, err := cache.Open(root)
	if err != nil {
		return nil
	}
	return c
}

// buildScratch returns the directory the intermediates of one build go in. It
// lives under the cache when there is one, so a build keeps its files in one
// place, and the caller removes it when the build is done.
func buildScratch(c *cache.Cache) (string, error) {
	if c != nil {
		return c.Scratch()
	}
	return os.MkdirTemp("", "zyx-build-")
}

// BackendOptions carries the settings of one link.
type BackendOptions struct {
	// Output is the path of the executable.
	Output string
	// Level is the optimization level, spelled -O0 through -O3.
	Level string
	// Debug keeps debug information.
	Debug bool
	// FastMath allows floating-point reassociation.
	FastMath bool
	// Freestanding leaves the runtime out of the link.
	Freestanding bool
	// Libs and LibDirs name the C libraries the program links against.
	Libs    []string
	LibDirs []string
	// CacheDir holds the cache the build reuses artifacts from.
	CacheDir string
	// Compiler identifies the compiler, which every cache key of an artifact
	// it produced carries.
	Compiler string
	// KeepIR keeps the generated LLVM IR next to the executable.
	KeepIR bool
}

// Link compiles the trees of one entry package and links them with the
// runtime. It returns the path of the executable.
func (s *Session) Link(entry string, results []*Result, opts BackendOptions) (string, error) {
	prog, err := s.monoProgram(entry, results)
	if err != nil {
		return "", err
	}
	t, err := toolchain.Locate()
	if err != nil {
		return "", err
	}
	c := openCache(opts.CacheDir)
	dir, err := buildScratch(c)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	copts := toolchain.CompileOptions{
		Target:   s.Loader.Target,
		Level:    opts.Level,
		Debug:    opts.Debug,
		FastMath: opts.FastMath,
	}

	// Every package becomes an object of its own, so a change to one package
	// is a change to one object: the module defines the package and declares
	// what the others hold, and the link brings them together. An object the
	// cache holds is reused rather than compiled again.
	var objects []string
	for i, pkg := range prog.Order {
		file := prog.Files[pkg]
		if file == nil {
			continue
		}
		where := filepath.Join(dir, fmt.Sprintf("p%d", i))
		out, err := s.packageObject(t, c, where, file, prog, pkg, opts, copts)
		if err != nil {
			return "", err
		}
		objects = append(objects, out)
	}
	// A package's own C sources are translation units of their own: a header
	// declares what a C file defines, and the two meet at the link.
	at := 0
	for _, res := range results {
		for _, src := range res.Package.CSources {
			out := filepath.Join(dir, fmt.Sprintf("c%d.o", at))
			at++
			copts.Includes = []string{res.Package.Dir}
			if err := t.CompileC(src, out, copts); err != nil {
				return "", err
			}
			objects = append(objects, out)
		}
	}
	if !opts.Freestanding {
		// The runtime is one library for every program of a module, so it is
		// built once and reused rather than recompiled for each program. It
		// comes last, because a linker searches an archive once, at the
		// position it holds: a reference from an object after it would find
		// nothing.
		lib, err := t.RuntimeLibrary(s.Loader.Target, copts, c, dir)
		if err != nil {
			return "", err
		}
		objects = append(objects, lib)
	}
	if err := os.MkdirAll(filepath.Dir(opts.Output), 0o755); err != nil {
		return "", err
	}
	// The executable is a function of the objects, the runtime, and the tools
	// that put them together, so an unchanged program links once and the
	// result is copied to wherever the caller asked for it: the output path
	// does not change what the executable is.
	if c != nil {
		key, err := s.linkKey(t, objects, opts)
		if err != nil {
			return "", err
		}
		if published, ok := c.Lookup(key); ok {
			return opts.Output, copyFile(filepath.Join(published, cacheExecutableName(s.Loader.Target)), opts.Output)
		}
		release, err := c.Lock(key)
		if err == nil {
			defer release()
			if published, ok := c.Lookup(key); ok {
				return opts.Output, copyFile(filepath.Join(published, cacheExecutableName(s.Loader.Target)), opts.Output)
			}
			if entry, err := c.New(key); err == nil {
				name := cacheExecutableName(s.Loader.Target)
				if err := t.Link(s.Loader.Target, objects, filepath.Join(entry.Out(), name), opts.Libs, opts.LibDirs); err == nil {
					if err := entry.Add(name); err == nil {
						if published, won, err := entry.Publish(); err == nil && won {
							return opts.Output, copyFile(filepath.Join(published, name), opts.Output)
						}
					}
				}
				entry.Discard()
			}
		}
	}
	if err := t.Link(s.Loader.Target, objects, opts.Output, opts.Libs, opts.LibDirs); err != nil {
		return "", err
	}
	return opts.Output, nil
}

// executableName returns the name an executable takes in the cache, which is
// the target's own name for one.
func cacheExecutableName(target toolchain.Target) string {
	return "app" + target.ExecutableSuffix()
}

// linkKey returns the key of the executable: the objects, the runtime, and
// the tools that put them together. The output path is not part of it.
func (s *Session) linkKey(t *toolchain.Toolchain, objects []string, opts BackendOptions) (string, error) {
	key := cache.NewKey()
	key.Add("link", opts.Compiler, t.Identity(), s.Loader.Target.Triple,
		strconv.FormatBool(opts.Freestanding))
	for _, object := range objects {
		hash, err := cache.HashFile(object)
		if err != nil {
			return "", err
		}
		key.Add(hash)
	}
	key.Add("libs", strings.Join(opts.Libs, ","), "libdirs", strings.Join(opts.LibDirs, ","))
	return key.Sum(), nil
}

// copyFile copies a file, publishing it the way every other artifact is
// published: a reader sees the whole file or none of it.
func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(to), "."+filepath.Base(to)+"-")
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
	if err := os.Chmod(name, 0o755); err != nil {
		os.Remove(name)
		return err
	}
	if err := cache.ReplaceFile(name, to); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// packageObject returns the object file of one package, compiling it when the
// cache does not hold it. The object is a function of the package's tree, the
// options, and the compiler, so a package that did not change is compiled
// once and reused.
func (s *Session) packageObject(t *toolchain.Toolchain, c *cache.Cache, where string, file *zyxc.File, prog *llvmgen.Program, pkg string, opts BackendOptions, copts toolchain.CompileOptions) (string, error) {
	compile := func(into string) (string, error) {
		ir, err := llvmgen.Generate(prog, pkg, s.Loader.Target)
		if err != nil {
			return "", err
		}
		if opts.Level != "-O0" {
			ir, err = t.Optimize(ir, copts)
			if err != nil {
				return "", err
			}
		}
		if opts.KeepIR && pkg == prog.Entry {
			if err := os.WriteFile(opts.Output+".ll", ir, 0o644); err != nil {
				return "", err
			}
		}
		if err := os.MkdirAll(into, 0o755); err != nil {
			return "", err
		}
		out := filepath.Join(into, "module.o")
		if err := t.CompileIR(ir, out, copts); err != nil {
			return "", err
		}
		return out, nil
	}

	if c == nil {
		return compile(where)
	}
	key := s.backendKey(file, opts)
	if published, ok := c.Lookup(key); ok {
		return filepath.Join(published, "module.o"), nil
	}
	release, err := c.Lock(key)
	if err != nil {
		// A cache that cannot be written only costs speed.
		return compile(where)
	}
	defer release()
	if published, ok := c.Lookup(key); ok {
		return filepath.Join(published, "module.o"), nil
	}
	entry, err := c.New(key)
	if err != nil {
		return compile(where)
	}
	object, err := compile(entry.Out())
	if err != nil {
		entry.Discard()
		return "", err
	}
	if err := entry.Add("module.o"); err != nil {
		entry.Discard()
		return compile(where)
	}
	published, won, err := entry.Publish()
	if err != nil || !won {
		return compile(where)
	}
	return filepath.Join(published, filepath.Base(object)), nil
}

// backendKey returns the key of one package's object: everything that decides
// what the object holds. The package's tree after monomorphization carries the
// signature of every instance it references, so one hash covers the package
// and what it instantiates.
func (s *Session) backendKey(file *zyxc.File, opts BackendOptions) string {
	key := cache.NewKey()
	key.Add("backend", opts.Compiler, s.Loader.Target.Triple, opts.Level,
		strconv.FormatBool(opts.Debug), strconv.FormatBool(opts.FastMath),
		strconv.FormatBool(opts.Freestanding))
	key.Add("zyxc", cache.HashBytes(zyxc.Format(file)))
	key.Add("sum", s.sumHash())
	return key.Sum()
}

// sumHash returns the hash of the module's checksum file, which fixes every
// dependency the package reads. A module with no checksum file has none.
func (s *Session) sumHash() string {
	data, err := os.ReadFile(filepath.Join(s.Loader.Root, "zyx.sum"))
	if err != nil {
		return ""
	}
	return cache.HashBytes(data)
}

// monoProgram returns the trees of a program after monomorphization, which is
// what the backend generates from.
func (s *Session) monoProgram(entry string, results []*Result) (*llvmgen.Program, error) {
	prog := &mono.Program{
		Target: s.Loader.Target.Triple,
		Entry:  entry,
		Files:  map[string]*zyxc.File{},
	}
	for _, res := range results {
		if res.IR == nil {
			continue
		}
		path := res.Package.ImportPath
		prog.Order = append(prog.Order, path)
		prog.Files[path] = res.IR
	}
	if prog.Files[entry] == nil {
		return nil, fmt.Errorf("package %s did not reach the backend", entry)
	}
	if _, err := mono.Instantiate(prog); err != nil {
		return nil, err
	}
	return &llvmgen.Program{Entry: prog.Entry, Order: prog.Order, Files: prog.Files}, nil
}

// IRText returns the LLVM IR of one package of a program, before
// optimization. The trees pass through monomorphization first, so the backend
// sees only concrete declarations.
func (s *Session) IRText(entry, pkg string, results []*Result) ([]byte, error) {
	prog, err := s.monoProgram(entry, results)
	if err != nil {
		return nil, err
	}
	return llvmgen.Generate(prog, pkg, s.Loader.Target)
}

// MonoText returns the intermediate form of one package after
// monomorphization: the same format the front end writes, with every generic
// definition replaced by the instances the program uses, one per signature
// and in source order.
func (s *Session) MonoText(entry, pkg string, results []*Result) ([]byte, error) {
	prog, err := s.monoProgram(entry, results)
	if err != nil {
		return nil, err
	}
	file := prog.Files[pkg]
	if file == nil {
		return nil, fmt.Errorf("package %s has no tree", pkg)
	}
	return zyxc.Format(file), nil
}
