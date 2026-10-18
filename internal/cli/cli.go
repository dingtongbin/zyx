// Package cli implements the zyx command-line surface: option parsing,
// module discovery, and the dispatch of every command.

package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"zyx/internal/build"
	"zyx/internal/mod"
	"zyx/internal/toolchain"
)

// Exit codes. A usage error is 2, a failed build is 1, and a panic in the
// main coroutine is 101.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// A Session is one invocation of the command line.
type Session struct {
	// Stdout and Stderr receive the output.
	Stdout, Stderr io.Writer
	// Dir is the working directory.
	Dir string
	// Env reads environment variables; nil uses the process environment.
	Env func(string) string
}

// Run parses arguments and runs one command, returning the exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "zyx:", err)
		return exitError
	}
	s := &Session{Stdout: stdout, Stderr: stderr, Dir: dir}
	return s.Run(args)
}

// Run dispatches one command.
func (s *Session) Run(args []string) int {
	if len(args) == 0 {
		s.usage()
		return exitUsage
	}
	command, rest := args[0], args[1:]
	switch command {
	case "check":
		return s.check(rest)
	case "emit-zyxc":
		return s.emitZyxc(rest)
	case "emit-llvm", "emit-asm":
		return s.emitBackend(command, rest)
	case "build":
		return s.build(rest)
	case "run":
		return s.run(rest)
	case "fmt":
		return s.fmt(rest)
	case "clean":
		return s.clean(rest)
	case "get":
		return s.get(rest)
	case "mod":
		return s.mod(rest)
	case "new":
		return s.newProject(rest)
	case "version":
		return s.version(rest)
	case "help", "-h", "--help":
		s.usage()
		return exitOK
	}
	fmt.Fprintf(s.Stderr, "zyx: unknown command %q\n", command)
	s.usage()
	return exitUsage
}

// usage prints the command list.
func (s *Session) usage() {
	fmt.Fprint(s.Stdout, `zyx compiles and checks zyx packages.

usage: zyx <command> [options] [paths]

commands:
  check      analyze packages without emitting object code
  build      compile packages to executables
  run        build and run immediately, at -O0 unless a level is given
  fmt        rewrite sources into canonical form
  clean      remove the build tree, and the cache with --cache
  get        add or upgrade a module requirement
  mod        manage the module: init, tidy, vendor
  new        scaffold a project skeleton
  version    print compiler and toolchain versions
  emit-zyxc  write the intermediate representation
  emit-llvm  write the LLVM IR before optimization
  emit-asm   write the target assembly after optimization

options:
  -o <path>            output file or directory
  --target <triple>    target triple (default the host)
  -O0 -O1 -O2 -O3      optimization level (build defaults to -O2)
  -g                   emit debug information
  --jobs N             parallel jobs (default the CPU count)
  --cache <dir>        cache directory override
  --no-cache           disable the cache for this run
  -v, --verbose        report cache hits and misses
  --json               machine-readable output
  --freestanding       do not link zyxruntime
  --mono               write the intermediate form after monomorphization
  -l <lib> -L <dir>    link a C library and add a search path
  --fast-math          allow floating-point reassociation
`)
}

// An options holds the parsed flags of one command.
type options struct {
	output       string
	target       toolchain.Target
	targetSet    bool
	level        string
	levelSet     bool
	debug        bool
	jobs         int
	cacheDir     string
	noCache      bool
	verbose      bool
	json         bool
	freestanding bool
	fastMath     bool
	// mono asks emit-zyxc for the form after monomorphization.
	mono       bool
	libs       []string
	libDirs    []string
	runPattern string
	bench      bool
	timeout    string
	failFast   bool
	cover      bool
	paths      []string
	// programArgs holds the arguments after `--`, which a run passes to
	// the program.
	programArgs []string
}

// parseOptions parses the flags every command shares.
func (s *Session) parseOptions(args []string) (*options, error) {
	o := &options{level: "", jobs: 0}
	i := 0
	for i < len(args) {
		arg := args[i]
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("option %s needs a value", arg)
			}
			i++
			return args[i], nil
		}
		switch {
		case arg == "-o":
			value, err := next()
			if err != nil {
				return nil, err
			}
			o.output = value
		case arg == "--target":
			value, err := next()
			if err != nil {
				return nil, err
			}
			t, err := toolchain.ParseTarget(value)
			if err != nil {
				return nil, err
			}
			o.target, o.targetSet = t, true
		case arg == "-O0" || arg == "-O1" || arg == "-O2" || arg == "-O3":
			o.level, o.levelSet = arg, true
		case arg == "-g":
			o.debug = true
		case arg == "--jobs":
			value, err := next()
			if err != nil {
				return nil, err
			}
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return nil, fmt.Errorf("--jobs needs a positive number")
			}
			o.jobs = n
		case arg == "--cache":
			value, err := next()
			if err != nil {
				return nil, err
			}
			o.cacheDir = value
		case arg == "--no-cache":
			o.noCache = true
		case arg == "-v" || arg == "--verbose":
			o.verbose = true
		case arg == "--json":
			o.json = true
		case arg == "--freestanding":
			o.freestanding = true
		case arg == "--fast-math":
			o.fastMath = true
		case arg == "--mono":
			o.mono = true
		case arg == "-l":
			value, err := next()
			if err != nil {
				return nil, err
			}
			o.libs = append(o.libs, value)
		case arg == "-L":
			value, err := next()
			if err != nil {
				return nil, err
			}
			o.libDirs = append(o.libDirs, value)
		case arg == "--run":
			value, err := next()
			if err != nil {
				return nil, err
			}
			o.runPattern = value
		case arg == "--bench":
			o.bench = true
		case arg == "--timeout":
			value, err := next()
			if err != nil {
				return nil, err
			}
			o.timeout = value
		case arg == "--fail-fast":
			o.failFast = true
		case arg == "--cover":
			o.cover = true
		case arg == "--":
			o.programArgs = append(o.programArgs, args[i+1:]...)
			i = len(args)
			continue
		case strings.HasPrefix(arg, "-") && arg != "-":
			return nil, fmt.Errorf("unknown option %q", arg)
		default:
			o.paths = append(o.paths, arg)
		}
		i++
	}
	if !o.targetSet {
		o.target = toolchain.HostTarget()
	}
	if o.level == "" {
		o.level = "-O2"
	}
	return o, nil
}

// A module is the module a command runs inside.
type module struct {
	// Root is the module root directory.
	Root string
	// File is the parsed zyx.mod.
	File *mod.File
	// Build is the module list a build reads, settled by version selection
	// over the whole dependency graph.
	Build []mod.Module
	// Vendored marks a module whose build reads a vendor directory rather
	// than the module cache.
	Vendored bool
}

// findModule walks up from a directory to the module root.
func (s *Session) findModule(start string) (*module, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	for {
		path := filepath.Join(dir, "zyx.mod")
		if data, err := os.ReadFile(path); err == nil {
			f, err := mod.Parse(data)
			if err != nil {
				return nil, err
			}
			return &module{Root: dir, File: f}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("no zyx.mod in %s or any parent directory", start)
		}
		dir = parent
	}
}

// loaderFor returns a loader that resolves the modules a walk has settled: the
// requirements it holds, and the modules a replacement points at a directory
// of the working tree.
func (s *Session) loaderFor(m *module, o *options, required, local map[string]string) *build.Loader {
	l := &build.Loader{
		Root:       m.Root,
		ModulePath: m.File.Module,
		StdRoot:    s.stdRoot(),
		Target:     o.target,
		Requires:   required,
		Local:      local,
	}
	if !m.Vendored {
		l.Modules = s.moduleCache(m)
	}
	if vendor := filepath.Join(m.Root, "vendor"); isDirectory(vendor) {
		l.Vendor = vendor
	}
	return l
}

// loader returns a loader for the modules a build reads.
func (s *Session) loader(m *module, o *options) *build.Loader {
	required := map[string]string{}
	local := map[string]string{}
	for _, dep := range m.Build {
		if dep.Dir != "" {
			local[dep.Path] = dep.Dir
			continue
		}
		required[dep.Path] = dep.Version
	}
	return s.loaderFor(m, o, required, local)
}

// moduleCacheRoot returns where fetched modules live: the directory the
// environment names, or one beside the module.
func (s *Session) moduleCacheRoot(m *module) string {
	if dir := s.env("ZYXMODCACHE"); dir != "" {
		return dir
	}
	return filepath.Join(m.Root, ".zyx-mod")
}

// isDirectory reports whether a path is an existing directory.
func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// stdRoot returns the standard library tree that ships with the compiler.
func (s *Session) stdRoot() string {
	if dir := s.env("ZYX_STD"); dir != "" {
		return dir
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(filepath.Dir(exe), "std")
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	return ""
}

// env reads an environment variable.
func (s *Session) env(name string) string {
	if s.Env != nil {
		return s.Env(name)
	}
	return os.Getenv(name)
}

// targets resolves the package paths a command acts on. The `./...` pattern
// stands for every package of the module, and a directory stands for the
// package in it.
func (s *Session) targets(m *module, o *options) ([]string, error) {
	if len(o.paths) == 0 {
		return []string{m.File.Module}, nil
	}
	var out []string
	for _, p := range o.paths {
		if strings.HasSuffix(p, "/...") {
			prefix := strings.TrimSuffix(p, "/...")
			all, err := s.loader(m, o).All(prefix)
			if err != nil {
				return nil, err
			}
			out = append(out, all...)
			continue
		}
		path, err := s.importPath(m, p)
		if err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, nil
}

// importPath maps a command-line path to an import path.
func (s *Session) importPath(m *module, p string) (string, error) {
	if !strings.HasPrefix(p, ".") {
		if p == "std" || strings.HasPrefix(p, "std/") || strings.HasPrefix(p, m.File.Module) {
			return p, nil
		}
	}
	abs, err := filepath.Abs(filepath.Join(s.Dir, p))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(m.Root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%s is outside module %s", p, m.File.Module)
	}
	if rel == "." {
		return m.File.Module, nil
	}
	return m.File.Module + "/" + filepath.ToSlash(rel), nil
}
