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

// indentation is the width of one level of the tree.
const indentation = "  "

// header returns the four fixed lines that open a file.
func header(target, module, exports string) []string {
	return []string{
		"zyx " + strconv.Itoa(FormatVersion),
		"target " + target,
		"module " + module,
		"exports " + exports,
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
		writeNode(&b, decl, 0)
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

// writeNode renders a node and its children at a depth.
func writeNode(b *strings.Builder, n *Node, depth int) {
	if n == nil {
		return
	}
	for i := 0; i < depth; i++ {
		b.WriteString(indentation)
	}
	b.WriteString(n.String())
	b.WriteByte('\n')
	for _, child := range n.Children {
		writeNode(b, child, depth+1)
	}
}

// errf builds a format error.
func errf(format string, args ...any) error {
	return fmt.Errorf("zyxc: "+format, args...)
}
