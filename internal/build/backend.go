// The backend driver: from the front end's trees to an executable.

package build

import (
	"fmt"
	"os"
	"path/filepath"

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
	// CacheDir holds the intermediate files.
	CacheDir string
	// KeepIR keeps the generated LLVM IR next to the executable.
	KeepIR bool
}

// Link compiles the trees of one entry package and links them with the
// runtime. It returns the path of the executable.
func (s *Session) Link(entry string, results []*Result, opts BackendOptions) (string, error) {
	ir, err := s.IRText(entry, results)
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

	irPath := filepath.Join(dir, "module.ll")
	if err := os.WriteFile(irPath, ir, 0o644); err != nil {
		return "", err
	}
	if opts.Level != "-O0" {
		optimized, err := t.Optimize(ir, copts)
		if err != nil {
			return "", err
		}
		ir = optimized
		if err := os.WriteFile(irPath, ir, 0o644); err != nil {
			return "", err
		}
	}
	if opts.KeepIR {
		if err := os.WriteFile(opts.Output+".ll", ir, 0o644); err != nil {
			return "", err
		}
	}

	object := filepath.Join(dir, "module.o")
	if err := t.CompileIR(ir, object, copts); err != nil {
		return "", err
	}
	objects := []string{object}
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
	if err := t.Link(s.Loader.Target, objects, opts.Output, opts.Libs, opts.LibDirs); err != nil {
		return "", err
	}
	return opts.Output, nil
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

// IRText returns the LLVM IR of an entry package without compiling it. The
// trees pass through monomorphization first, so the backend sees only
// concrete declarations.
func (s *Session) IRText(entry string, results []*Result) ([]byte, error) {
	prog, err := s.monoProgram(entry, results)
	if err != nil {
		return nil, err
	}
	return llvmgen.Generate(prog, "", s.Loader.Target)
}
