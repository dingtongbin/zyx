package toolchain

import (
	"os"
	"path/filepath"
	"sort"
)

// stdCLibs records the C library each standard package binds. The design says
// that a binding ships with the toolchain rather than with the program, so a
// program names no library flag of its own: the packages it reaches decide what
// the link takes.
//
// A name is what the linker resolves. Where a distribution carries the
// libraries, they are the files it ships; where it does not, the same names
// resolve from the C toolchain the compiler itself uses, which is where they
// come from on a machine that installed them. A name written with a colon is a
// file rather than a search: the MariaDB and PostgreSQL client libraries are
// shared libraries here, because their licences and their own imports make a
// static link the wrong one, and the file names the import library exactly.
var stdCLibs = map[string][]string{
	"std/compress": {"zstd"},
	"std/mysql":    {":libmariadb.dll.a"},
	"std/pg":       {":libpq.dll.a"},
	"std/redis":    {"hiredis", "ws2_32"},
	"std/sqlite":   {"sqlite3"},
	"std/tls":      {"ssl", "crypto", "crypt32"},
}

// StdCLibs answers the libraries the given import paths need, each name once and
// in a fixed order, so that the same program links to the same bytes every time.
func StdCLibs(importPaths []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, path := range importPaths {
		for _, lib := range stdCLibs[path] {
			if seen[lib] {
				continue
			}
			seen[lib] = true
			out = append(out, lib)
		}
	}
	sort.Strings(out)
	return out
}

// CLibDirs answers the directories a distribution keeps its own C libraries in,
// which hold the libraries a program would otherwise have to install: the
// directory the environment names, and the one beside the compiler. A
// distribution that carries none answers none, and the names above then resolve
// from the C toolchain's own directories.
func CLibDirs() []string {
	var out []string
	if dir := os.Getenv("ZYX_C_LIBS"); dir != "" {
		out = append(out, dir)
	}
	exe, err := os.Executable()
	if err != nil {
		return out
	}
	bundle := filepath.Join(filepath.Dir(exe), "c", "lib")
	if info, err := os.Stat(bundle); err == nil && info.IsDir() {
		out = append(out, bundle)
	}
	return out
}
