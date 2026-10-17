// Canonical writer for .zyxc.

package zyxc

import (
	"strconv"
	"strings"
)

// Format renders a file in canonical form: the fixed header, the summary, and
// the tree indented two spaces per level, one space between tokens, no trailing
// space, and one final newline. Every node carries a number, which is the handle
// a reader uses to name one node in a diff or a conversation. The same file
// always renders the same bytes, which is what makes the format an audit layer
// and a cache key.
func Format(f *File) []byte {
	for _, decl := range f.Decls {
		decl.Sorted()
	}
	nodes := countNodes(f.Decls)
	var b strings.Builder
	for _, line := range header(f.Target, f.Module, f.Exports, nodes) {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	writeSummary(&b, f, nodes)
	b.WriteString("tree\n")
	id := 1
	for _, decl := range f.Decls {
		writeNode(&b, decl, 1, &id, origin{})
	}
	return []byte(b.String())
}

// writeSummary writes the counts that orient a reader before it reads the tree.
// They are counted from the tree itself, so they cannot drift from it.
func writeSummary(b *strings.Builder, f *File, nodes int) {
	b.WriteString("summary\n")
	writeCount(b, "nodes", nodes)
	writeCount(b, "regions", countMarks(f.Decls, "region:enter"))
	writeCount(b, "coroutines", countMarks(f.Decls, "coro:spawn"))
}

// writeCount writes one count line of the summary.
func writeCount(b *strings.Builder, name string, n int) {
	b.WriteString(indentation)
	b.WriteString(name)
	b.WriteByte(' ')
	b.WriteString(strconv.Itoa(n))
	b.WriteByte('\n')
}
