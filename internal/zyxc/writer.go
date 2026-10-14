// Canonical writer for .zyxc.

package zyxc

import "strings"

// Format renders a file in canonical form: the fixed four-line header, the
// tree indented two spaces per level, one space between tokens, no trailing
// space, and one final newline. The same file always renders the same
// bytes, which is what makes the format an audit layer and a cache key.
func Format(f *File) []byte {
	for _, decl := range f.Decls {
		decl.Sorted()
	}
	var b strings.Builder
	for _, line := range header(f.Target, f.Module, f.Exports) {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, decl := range f.Decls {
		writeNode(&b, decl, 0)
	}
	return []byte(b.String())
}
