// Expands memory marks into concrete operations.

package llvmgen

import "zyx/internal/zyxc"

// hasMark reports whether a node carries a mark.
func hasMark(n *zyxc.Node, name string) bool {
	for _, m := range n.Marks {
		if m.Name == name {
			return true
		}
	}
	return false
}

// markArg returns the argument of a mark, or the empty string.
func markArg(n *zyxc.Node, name string) string {
	for _, m := range n.Marks {
		if m.Name == name {
			return m.Arg
		}
	}
	return ""
}
