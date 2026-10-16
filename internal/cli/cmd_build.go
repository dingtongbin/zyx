// The build, run, and emit commands drive the backend.

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"zyx/internal/build"
	"zyx/internal/cache"
	"zyx/internal/toolchain"
)

// compilerIdentity returns what every cache key of an artifact this compiler
// produced carries: the version it reports and the hash of the binary, so a
// development build invalidates what it made without anyone having to bump a
// version.
var compilerIdentity = sync.OnceValue(func() string {
	identity := Version
	exe, err := os.Executable()
	if err != nil {
		return identity
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return identity
	}
	return identity + "+" + cache.HashBytes(data)
})

// backendOptions turns the parsed flags into the backend's settings. The
// cache is content-addressed and belongs to the project, so it lives under
// the module root unless the caller named a directory or the environment
// points at a shared one.
func (s *Session) backendOptions(o *options, output, root string) build.BackendOptions {
	cacheDir := o.cacheDir
	if cacheDir == "" {
		cacheDir = s.env("ZYX_CACHE")
	}
	if cacheDir == "" {
		cacheDir = filepath.Join(root, ".zyx-cache")
	}
	if o.noCache {
		// A run that disables the cache neither reads nor writes one.
		cacheDir = ""
	}
	return build.BackendOptions{
		Output:       output,
		Level:        o.level,
		Debug:        o.debug,
		FastMath:     o.fastMath,
		Freestanding: o.freestanding,
		Libs:         o.libs,
		LibDirs:      o.libDirs,
		CacheDir:     cacheDir,
		Compiler:     compilerIdentity(),
	}
}

// build compiles every main package among the targets to an executable.
func (s *Session) build(args []string) int {
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
	paths, err := s.targets(m, o)
	if err != nil {
		return s.usageError(err)
	}
	session := &build.Session{Loader: s.loader(m, o)}
	reported := map[string]bool{}
	// The front end runs before any output is chosen, because where an
	// executable goes depends on how many main packages the targets hold.
	var mains []string
	front := map[string][]*build.Result{}
	for _, path := range paths {
		results, err := session.FrontEnd(path)
		if err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
		if s.reportOnce(results, reported) {
			return exitError
		}
		if results[len(results)-1].Package.Name != "main" {
			continue
		}
		mains = append(mains, path)
		front[path] = results
	}
	if len(mains) == 0 {
		fmt.Fprintln(s.Stderr, "zyx: no package among the targets is a main package")
		return exitError
	}
	outs, err := build.OutputPaths(m.Root, mains, o.output, o.target)
	if err != nil {
		return s.usageError(err)
	}
	for i, path := range mains {
		if _, err := session.Link(path, front[path], s.backendOptions(o, outs[i], m.Root)); err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
		if o.verbose {
			fmt.Fprintf(s.Stdout, "built %s\n", outs[i])
		}
	}
	return exitOK
}

// run builds the target with no optimization and runs it.
func (s *Session) run(args []string) int {
	o, err := s.parseOptions(args)
	if err != nil {
		return s.usageError(err)
	}
	// A run that names no level is a development run, which is not worth
	// optimizing. A level the caller asked for is honored, so a run can
	// check what an optimized build does.
	if !o.levelSet {
		o.level = "-O0"
	}
	m, err := s.findModule(s.Dir)
	if err != nil {
		return s.usageError(err)
	}
	if !s.prepareModule(m) {
		return exitError
	}
	paths, err := s.targets(m, o)
	if err != nil {
		return s.usageError(err)
	}
	session := &build.Session{Loader: s.loader(m, o)}
	reported := map[string]bool{}
	for _, path := range paths {
		results, err := session.FrontEnd(path)
		if err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
		if s.reportOnce(results, reported) {
			return exitError
		}
		if results[len(results)-1].Package.Name != "main" {
			continue
		}
		out := build.ExecutablePath(m.Root, path, o.target)
		if _, err := session.Link(path, results, s.backendOptions(o, out, m.Root)); err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
		return s.exec(out, o.programArgs)
	}
	fmt.Fprintln(s.Stderr, "zyx: no package among the targets is a main package")
	return exitError
}

// exec runs a built program, passing its own output through.
func (s *Session) exec(path string, args []string) int {
	cmd := exec.Command(path, args...)
	cmd.Stdout = s.Stdout
	cmd.Stderr = s.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	return exitOK
}

// emitBackend writes the LLVM IR or the assembly of one package.
func (s *Session) emitBackend(command string, args []string) int {
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
	paths, err := s.targets(m, o)
	if err != nil {
		return s.usageError(err)
	}
	if len(paths) != 1 {
		return s.usageError(fmt.Errorf("%s takes a single package", command))
	}
	session := &build.Session{Loader: s.loader(m, o)}
	results, err := session.FrontEnd(paths[0])
	if err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	if s.reportOnce(results, map[string]bool{}) {
		return exitError
	}
	ir, err := session.IRText(paths[0], paths[0], results)
	if err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	if command == "emit-llvm" {
		if err := s.writeOutput(o.output, ir); err != nil {
			fmt.Fprintln(s.Stderr, "zyx:", err)
			return exitError
		}
		return exitOK
	}
	t, err := toolchain.Locate()
	if err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	out := o.output
	if out == "" {
		out = filepath.Join(m.Root, "build", "module.s")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	copts := toolchain.CompileOptions{Target: o.target, Level: o.level, Debug: o.debug, FastMath: o.fastMath}
	if err := t.CompileAsm(ir, out, copts); err != nil {
		fmt.Fprintln(s.Stderr, "zyx:", err)
		return exitError
	}
	return exitOK
}
