// Canonical ordering and formatting rules.

package zyxc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// FormatVersion is the version of the format this package writes. A reader
// must check the first line of a file against it.
const FormatVersion = 1

// BuiltinID is the symbol id of a method the language provides rather than a
// declaration. A built-in method has no declaration anywhere, so the backend
// pairs its name with the receiver's type through the fixed table instead of
// looking a symbol up.
const BuiltinID = "builtin"

// indentation is the width of one level of the tree.
const indentation = "  "

// header returns the fixed header lines that open a file. The node count is
// part of it so that a reader knows the size of the tree before reading it.
func header(target, module, exports string, nodes int) []string {
	return []string{
		"zyxc " + strconv.Itoa(FormatVersion),
		"target " + target,
		"module " + module,
		"exports " + exports,
		"node " + strconv.Itoa(nodes),
	}
}

// ExportsHash returns the hash of a package's export interface: the
// canonical text of the public declaration subtree, without positions or
// private declarations. A package that imports this one records the hash,
// so a change to the interface invalidates the importers.
func ExportsHash(decls []*Node) string {
	var b strings.Builder
	for _, decl := range decls {
		if !isExported(decl) {
			continue
		}
		writeCanonical(&b, decl, 0)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// isExported reports whether a declaration is part of the export interface:
// a public top-level declaration.
func isExported(decl *Node) bool {
	if decl == nil || len(decl.Payload) == 0 {
		return false
	}
	switch decl.Kind {
	case "import", "import-c":
		return false
	}
	name := decl.Payload[0]
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

// An origin is the node an enclosing node was written from: where it stands in
// the source and the number the format gave it.
type origin struct {
	pos string
	id  int
}

// writeNode renders a node and its children at a depth, numbering the nodes in
// the order the format writes them. A node the builder gave no position
// inherits the position of the node it was derived from and names that node, so
// every line of the tree has a place in the source and a reader reaches it in
// one step.
func writeNode(b *strings.Builder, n *Node, depth int, id *int, from origin) {
	if n == nil {
		return
	}
	if n.Pos == "" && from.id != 0 {
		n.Pos = from.pos
		n.From = "n" + strconv.Itoa(from.id)
	}
	mine := *id
	(*id)++
	for i := 0; i < depth; i++ {
		b.WriteString(indentation)
	}
	b.WriteString("n" + strconv.Itoa(mine))
	b.WriteByte(' ')
	b.WriteString(n.String())
	b.WriteByte('\n')
	child := origin{pos: n.Pos, id: mine}
	for _, c := range n.Children {
		writeNode(b, c, depth+1, id, child)
	}
}

// countNodes returns how many nodes a list of subtrees holds.
func countNodes(decls []*Node) int {
	total := 0
	for _, decl := range decls {
		total += countSubtree(decl)
	}
	return total
}

// countSubtree returns how many nodes one subtree holds, itself included.
func countSubtree(n *Node) int {
	if n == nil {
		return 0
	}
	total := 1
	for _, child := range n.Children {
		total += countSubtree(child)
	}
	return total
}

// countMarks returns how many nodes carry a mark of that name. It is how the
// summary counts the regions and the coroutines of a package, which are marks
// on the nodes rather than a list of their own.
func countMarks(decls []*Node, name string) int {
	total := 0
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		for _, m := range n.Marks {
			if m.Name == name {
				total++
				break
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	for _, decl := range decls {
		walk(decl)
	}
	return total
}

// writeCanonical writes a node without its source position, which is what
// the export hash covers. A position names a file inside one checkout, so
// hashing it would make the hash depend on where the tree happens to sit.
func writeCanonical(b *strings.Builder, n *Node, depth int) {
	if n == nil {
		return
	}
	for i := 0; i < depth; i++ {
		b.WriteString(indentation)
	}
	b.WriteString(n.head())
	if len(n.Marks) > 0 {
		parts := make([]string, 0, len(n.Marks))
		for _, m := range n.Marks {
			parts = append(parts, m.String())
		}
		b.WriteString(" marks {" + strings.Join(parts, ", ") + "}")
	}
	b.WriteByte('\n')
	for _, child := range n.Children {
		writeCanonical(b, child, depth+1)
	}
}

// errf builds a format error.
func errf(format string, args ...any) error {
	return fmt.Errorf("zyxc: "+format, args...)
}
