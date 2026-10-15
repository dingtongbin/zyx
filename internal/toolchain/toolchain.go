// Package toolchain invokes the bundled clang, LLVM, and zig components.
package toolchain

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// A Toolchain holds the programs that turn generated code into an
// executable. A distribution ships clang and uses it for every step; a
// system that provides only the LLVM tools compiles IR with llc and runs
// the middle end with opt.
type Toolchain struct {
	// Clang compiles LLVM IR and C sources when it is present.
	Clang string
	// LLC compiles LLVM IR to an object or assembly file.
	LLC string
	// Opt runs the middle-end pipeline over LLVM IR.
	Opt string
	// CC compiles the C runtime and performs the link.
	CC string
	// Runtime is the runtime tree, which holds include/ and the sources.
	Runtime string
}

// CompileOptions carries the settings every step shares.
type CompileOptions struct {
	// Target selects the machine the output runs on.
	Target Target
	// Level is the optimization level, spelled -O0 through -O3.
	Level string
	// Debug keeps debug information.
	Debug bool
	// FastMath allows floating-point reassociation.
	FastMath bool
}

// Locate finds the compiler programs and the runtime tree. It reports the
// first component that is missing.
//
// A distribution ships the toolchain next to the compiler, so the search
// looks there first, then at the directories the environment names, then
// beside whichever LLVM tool the host already provides, and finally along
// the search path.
func Locate() (*Toolchain, error) {
	dirs := candidateDirs()
	t := &Toolchain{
		Clang:   findTool("clang", dirs),
		LLC:     findTool("llc", dirs),
		Opt:     findTool("opt", dirs),
		CC:      findTool("clang", dirs),
		Runtime: findRuntime(),
	}
	if t.CC == "" {
		t.CC = firstOf([]string{"gcc", "cc"}, dirs)
	}
	if t.Clang == "" && t.LLC == "" {
		return nil, fmt.Errorf("no LLVM compiler found: install clang or the LLVM tools")
	}
	if t.CC == "" {
		return nil, fmt.Errorf("no C compiler found for the runtime and the link")
	}
	if t.Runtime == "" {
		return nil, fmt.Errorf("the zyx runtime tree was not found; set ZYX_RUNTIME")
	}
	return t, nil
}

// candidateDirs lists the directories the toolchain search visits, in
// order. ZYX_LLVM_ROOT names the toolchain root the distribution ships, and
// ZYX_LLVM_BIN names a directory of programs directly.
func candidateDirs() []string {
	var dirs []string
	if root := os.Getenv("ZYX_LLVM_ROOT"); root != "" {
		dirs = append(dirs, filepath.Join(root, "bin"), root)
	}
	if dir := os.Getenv("ZYX_LLVM_BIN"); dir != "" {
		dirs = append(dirs, filepath.SplitList(dir)...)
	}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		dirs = append(dirs, filepath.Join(base, "toolchain", "bin"), filepath.Join(base, "bin"))
	}
	bindir := llvmBindir()
	if bindir == "" {
		return dirs
	}
	dirs = append(dirs, bindir)
	// Several systems install one LLVM toolchain per target environment,
	// laid out as prefix/environment/bin, so the sibling environments of
	// the one llvm-config reports may hold the programs it lacks.
	prefix := filepath.Dir(filepath.Dir(bindir))
	entries, err := os.ReadDir(prefix)
	if err != nil {
		return dirs
	}
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(prefix, entry.Name(), "bin"))
		}
	}
	return dirs
}

// llvmBindir returns the directory llvm-config reports, or the empty
// string when the host has no llvm-config.
func llvmBindir() string {
	program := lookPath("llvm-config")
	if program == "" {
		return ""
	}
	out, err := exec.Command(program, "--bindir").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// findTool returns the path of a program, preferring the candidate
// directories over the search path.
func findTool(name string, dirs []string) string {
	for _, dir := range dirs {
		for _, suffix := range []string{".exe", ""} {
			candidate := filepath.Join(dir, name+suffix)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	return lookPath(name)
}

// firstOf returns the path of the first program that exists.
func firstOf(names []string, dirs []string) string {
	for _, name := range names {
		if p := findTool(name, dirs); p != "" {
			return p
		}
	}
	return ""
}

// lookPath returns the path of a program, or the empty string.
func lookPath(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// findRuntime locates the runtime tree. The environment wins, then the tree
// next to the compiler, then the tree above the working directory, which is
// how a checkout is laid out.
func findRuntime() string {
	if dir := os.Getenv("ZYX_RUNTIME"); dir != "" {
		return dir
	}
	dirs := []string{}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}
	for _, start := range dirs {
		for dir := start; ; {
			candidate := filepath.Join(dir, "runtime")
			if info, err := os.Stat(filepath.Join(candidate, "include", "zyxrt.h")); err == nil && !info.IsDir() {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}

// Sources lists the runtime translation units that take part in a build for
// a target, in link order.
func (t *Toolchain) Sources(target Target) []string {
	platform := filepath.Join(t.Runtime, "platform", target.OS+".c")
	if _, err := os.Stat(platform); err != nil {
		platform = filepath.Join(t.Runtime, "platform", "linux.c")
	}
	sources := []string{
		filepath.Join(t.Runtime, "core", "start.c"),
		filepath.Join(t.Runtime, "core", "panic.c"),
		filepath.Join(t.Runtime, "core", "abi.c"),
		filepath.Join(t.Runtime, "core", "defer.c"),
		filepath.Join(t.Runtime, "core", "str.c"),
		filepath.Join(t.Runtime, "mem", "region.c"),
		filepath.Join(t.Runtime, "mem", "pool.c"),
		filepath.Join(t.Runtime, "mem", "stack.c"),
		filepath.Join(t.Runtime, "sched", "sched.c"),
		filepath.Join(t.Runtime, "sched", "timer.c"),
		filepath.Join(t.Runtime, "chan", "chan.c"),
		filepath.Join(t.Runtime, "container", "array.c"),
		filepath.Join(t.Runtime, "container", "map.c"),
		filepath.Join(t.Runtime, "container", "queue.c"),
		filepath.Join(t.Runtime, "container", "bqueue.c"),
	}
	// A hand-written stack switch replaces the portable context routines on
	// the architectures that have one.
	if asm := t.switchSource(target); asm != "" {
		sources = append(sources, asm)
	}
	return append(sources, platform)
}

// switchSource returns the assembly unit that switches coroutine stacks, or
// the empty string when the target falls back to the portable routines.
func (t *Toolchain) switchSource(target Target) string {
	if target.Arch != "amd64" {
		return ""
	}
	name := "switch_x86_64_sysv.S"
	if target.OS == "windows" {
		name = "switch_x86_64_win.S"
	}
	path := filepath.Join(t.Runtime, "platform", name)
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// IncludeFlags returns the preprocessor search paths the runtime sources
// and generated code need.
func (t *Toolchain) IncludeFlags() []string {
	return []string{
		"-I" + filepath.Join(t.Runtime, "include"),
		"-I" + filepath.Join(t.Runtime, "core"),
		"-I" + filepath.Join(t.Runtime, "platform"),
	}
}

// CompileIR turns LLVM IR text into an object file.
func (t *Toolchain) CompileIR(ir []byte, out string, opts CompileOptions) error {
	src, err := writeTemp(ir, "zyx-*.ll")
	if err != nil {
		return err
	}
	defer os.Remove(src)

	if t.Clang != "" {
		args := []string{"-x", "ir", "-c", "-o", out, src}
		args = append(args, levelFlag(opts.Level)...)
		if opts.Target.Triple != "" {
			args = append(args, "--target="+opts.Target.Triple)
		}
		if opts.FastMath {
			args = append(args, "-ffast-math")
		}
		if opts.Debug {
			args = append(args, "-g")
		}
		return t.run(t.Clang, args)
	}
	args := []string{"-filetype=obj", "-o", out, src}
	if opts.Target.Triple != "" {
		args = append(args, "-mtriple="+opts.Target.Triple)
	}
	args = append(args, llcLevelFlag(opts.Level)...)
	return t.run(t.LLC, args)
}

// Optimize runs the middle-end pipeline over LLVM IR and returns the result.
func (t *Toolchain) Optimize(ir []byte, opts CompileOptions) ([]byte, error) {
	if t.Opt == "" || opts.Level == "-O0" || opts.Level == "" {
		return ir, nil
	}
	src, err := writeTemp(ir, "zyx-*.ll")
	if err != nil {
		return nil, err
	}
	defer os.Remove(src)
	out := src + ".opt"
	defer os.Remove(out)
	if err := t.run(t.Opt, []string{"-S", optLevelFlag(opts.Level), "-o", out, src}); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("reading optimized IR: %w", err)
	}
	return data, nil
}

// CompileAsm turns LLVM IR text into assembly for inspection.
func (t *Toolchain) CompileAsm(ir []byte, out string, opts CompileOptions) error {
	src, err := writeTemp(ir, "zyx-*.ll")
	if err != nil {
		return err
	}
	defer os.Remove(src)
	if t.Clang != "" {
		args := []string{"-x", "ir", "-S", "-o", out, src}
		if opts.Target.Triple != "" {
			args = append(args, "--target="+opts.Target.Triple)
		}
		return t.run(t.Clang, args)
	}
	args := []string{"-filetype=asm", "-o", out, src}
	if opts.Target.Triple != "" {
		args = append(args, "-mtriple="+opts.Target.Triple)
	}
	return t.run(t.LLC, args)
}

// CompileC turns a C or assembly source file into an object file.
func (t *Toolchain) CompileC(src, out string, opts CompileOptions) error {
	args := []string{"-c", "-o", out, src}
	if !strings.HasSuffix(src, ".S") {
		args = append(args, "-std=c11")
	}
	args = append(args, t.IncludeFlags()...)
	args = append(args, levelFlag(opts.Level)...)
	if opts.Debug {
		args = append(args, "-g")
	}
	return t.run(t.CC, args)
}

// Link turns object files into an executable. It appends the runtime
// objects the caller names and the libraries the program needs.
func (t *Toolchain) Link(objects []string, out string, libs, libDirs []string) error {
	args := []string{"-o", out}
	args = append(args, objects...)
	for _, dir := range libDirs {
		args = append(args, "-L"+dir)
	}
	for _, lib := range libs {
		args = append(args, "-l"+lib)
	}
	args = append(args, "-lm")
	// A static link keeps the unwinder and the C runtime inside the
	// executable, so a built program runs wherever it is copied to.
	args = append(args, "-static")
	return t.run(t.CC, args)
}

// run executes a program and reports its output on failure.
func (t *Toolchain) run(program string, args []string) error {
	cmd := exec.Command(program, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("%s: %s", filepath.Base(program), message)
	}
	return nil
}

// writeTemp writes data to a temporary file and returns its path.
func writeTemp(data []byte, pattern string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("creating a temporary file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("writing a temporary file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// levelFlag maps an optimization level onto the clang spelling.
func levelFlag(level string) []string {
	switch level {
	case "-O1", "-O2", "-O3":
		return []string{level}
	case "-O0", "":
		return []string{"-O0"}
	}
	return nil
}

// llcLevelFlag maps an optimization level onto the llc spelling.
func llcLevelFlag(level string) []string {
	switch level {
	case "-O1":
		return []string{"-O=1"}
	case "-O2":
		return []string{"-O=2"}
	case "-O3":
		return []string{"-O=3"}
	}
	return []string{"-O=0"}
}

// optLevelFlag maps an optimization level onto the opt spelling.
func optLevelFlag(level string) string {
	switch level {
	case "-O3":
		return "-O3"
	case "-O1":
		return "-O1"
	default:
		return "-O2"
	}
}
