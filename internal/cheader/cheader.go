// Package cheader reads a C header through clang and reports the
// declarations the c namespace exposes.
//
// The header is parsed by clang rather than by this package: clang owns the
// C language, so a declaration that clang accepts is the declaration the
// language sees. The package asks for the syntax tree in a machine-readable
// form and keeps the declarations a program can name: functions, records,
// typedefs, enumerators, and globals.
//
// The width of a C type depends on the target (long is eight bytes on
// Windows and four on Linux), so the widths come from the preprocessor of the
// same target rather than from a table written here.

package cheader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"zyx/internal/types"
)

// A Table is the surface one C header exposes.
type Table struct {
	// Functions maps a C function name onto its signature.
	Functions map[string]*Func
	// Records maps a struct or union tag onto its kind.
	Records map[string]*Record
	// Aliases maps a typedef name onto the type it names.
	Aliases map[string]*types.Type
	// Constants maps an enumerator onto its value.
	Constants map[string]int64
	// Globals maps a global object onto its type.
	Globals map[string]*types.Type
}

// A Func is one C function declaration.
type Func struct {
	Name     string
	Params   []*types.Type
	Names    []string
	Result   *types.Type
	Variadic bool
}

// A Record is one C struct or union tag.
type Record struct {
	Name  string
	Union bool
}

// widths holds the sizes the preprocessor reports for the target.
type widths struct {
	char     int
	short    int
	int      int
	long     int
	longLong int
	pointer  int
}

// Parse reads a header through clang and returns the table it exposes.
// clang is the compiler the toolchain located and includes the directories
// the C boundary searches.
//
// The header is read with the compiler's own target rather than the triple
// the program is built for, because the C side of a program is compiled the
// same way: the widths the preprocessor reports are then the widths the C
// objects that will be linked were built with. A cross build's headers wait
// for the sysroot work.
func Parse(clang, header string, includes []string) (*Table, error) {
	if clang == "" {
		return nil, fmt.Errorf("cheader: no C compiler is available")
	}
	dir, err := os.MkdirTemp("", "zyx-header-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	tu := filepath.Join(dir, "header.c")
	if err := os.WriteFile(tu, []byte("#include \""+header+"\"\n"), 0o644); err != nil {
		return nil, err
	}

	sizes, err := measure(clang, tu, includes)
	if err != nil {
		return nil, err
	}
	text, err := dump(clang, tu, includes)
	if err != nil {
		return nil, err
	}
	var root node
	if err := json.Unmarshal(text, &root); err != nil {
		return nil, fmt.Errorf("cheader: the syntax tree of %s does not read: %w", header, err)
	}
	p := &parser{sizes: sizes, table: &Table{
		Functions: map[string]*Func{},
		Records:   map[string]*Record{},
		Aliases:   map[string]*types.Type{},
		Constants: map[string]int64{},
		Globals:   map[string]*types.Type{},
	}}
	p.walk(&root)
	return p.table, nil
}

// Names returns every name the table exposes, in byte order, which is what
// the intermediate form writes.
func (t *Table) Names() []string {
	var out []string
	for name := range t.Functions {
		out = append(out, name)
	}
	for name := range t.Records {
		out = append(out, name)
	}
	for name := range t.Aliases {
		out = append(out, name)
	}
	for name := range t.Constants {
		out = append(out, name)
	}
	for name := range t.Globals {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// run invokes clang and returns what it wrote to standard output.
func run(clang string, args ...string) ([]byte, error) {
	cmd := exec.Command(clang, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("clang: %s", message)
	}
	return stdout.Bytes(), nil
}

// args returns the flags every invocation of clang shares.
func args(tu string, includes []string) []string {
	out := []string{"-x", "c"}
	for _, dir := range includes {
		out = append(out, "-I"+dir)
	}
	return append(out, tu)
}

// dump returns the syntax tree of the translation unit.
func dump(clang, tu string, includes []string) ([]byte, error) {
	all := append([]string{"-fsyntax-only", "-Xclang", "-ast-dump=json"}, args(tu, includes)...)
	return run(clang, all...)
}

// measure returns the type widths the preprocessor reports for the target.
func measure(clang, tu string, includes []string) (widths, error) {
	all := append([]string{"-dM", "-E"}, args(tu, includes)...)
	text, err := run(clang, all...)
	if err != nil {
		return widths{}, err
	}
	w := widths{char: 1, short: 2, int: 4, long: 4, longLong: 8, pointer: 8}
	for _, line := range strings.Split(string(text), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "#define" {
			continue
		}
		n, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}
		switch fields[1] {
		case "__SIZEOF_CHAR":
			w.char = n
		case "__SIZEOF_SHORT":
			w.short = n
		case "__SIZEOF_INT":
			w.int = n
		case "__SIZEOF_LONG":
			w.long = n
		case "__SIZEOF_LONG_LONG":
			w.longLong = n
		case "__SIZEOF_POINTER":
			w.pointer = n
		}
	}
	return w, nil
}
