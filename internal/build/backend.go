// The backend driver: from the front end's trees to an executable.

package build

import (
	"fmt"
	"os"
	"path/filepath"

	"zyx/internal/llvmgen"
	"zyx/internal/toolchain"
	"zyx/internal/zyxc"
)

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
	prog := &llvmgen.Program{Entry: entry, Files: map[string]*zyxc.File{}}
	for _, res := range results {
		if res.IR == nil {
			continue
		}
		path := res.Package.ImportPath
		prog.Order = append(prog.Order, path)
		prog.Files[path] = res.IR
	}
	if prog.Files[entry] == nil {
		return "", fmt.Errorf("package %s did not reach the backend", entry)
	}
	ir, err := llvmgen.Generate(prog, s.Loader.Target)
	if err != nil {
		return "", err
	}
	t, err := toolchain.Locate()
	if err != nil {
		return "", err
	}
	dir := opts.CacheDir
	if dir == "" {
		dir = filepath.Join(s.Loader.Root, "build", ".cache")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
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
	if !opts.Freestanding {
		sources := t.Sources(s.Loader.Target)
		for i, src := range sources {
			out := filepath.Join(dir, fmt.Sprintf("runtime%d.o", i))
			if err := t.CompileC(src, out, copts); err != nil {
				return "", err
			}
			objects = append(objects, out)
		}
	}
	if err := os.MkdirAll(filepath.Dir(opts.Output), 0o755); err != nil {
		return "", err
	}
	if err := t.Link(objects, opts.Output, opts.Libs, opts.LibDirs); err != nil {
		return "", err
	}
	return opts.Output, nil
}

// IRText returns the LLVM IR of an entry package without compiling it.
func (s *Session) IRText(entry string, results []*Result) ([]byte, error) {
	prog := &llvmgen.Program{Entry: entry, Files: map[string]*zyxc.File{}}
	for _, res := range results {
		if res.IR == nil {
			continue
		}
		path := res.Package.ImportPath
		prog.Order = append(prog.Order, path)
		prog.Files[path] = res.IR
	}
	return llvmgen.Generate(prog, s.Loader.Target)
}
