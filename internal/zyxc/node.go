// Node model of the intermediate representation.

package zyxc

import (
	"fmt"
	"strings"
)

// A Node is one line of the .zyxc tree: a kind, its payload tokens, its
// marks, and its children.
type Node struct {
	// Kind is the node kind, a lowercase ASCII word.
	Kind string
	// Payload holds the space-separated tokens after the kind.
	Payload []string
	// Pos is the source position, empty for a node the compiler generated.
	Pos string
	// Marks holds the marks of the node, sorted by name and argument.
	Marks []Mark
	// Children holds the child nodes in order.
	Children []*Node
}

// A Mark is one mark attached to a node.
type Mark struct {
	// Name is the mark name, such as "home" or "coro:spawn".
	Name string
	// Arg is the argument of a parameterized mark, empty when it takes
	// none.
	Arg string
}

// String renders the mark the way the format writes it.
func (m Mark) String() string {
	if m.Arg == "" {
		return m.Name
	}
	return m.Name + ":" + m.Arg
}

// New returns a node of a kind with a payload.
func New(kind string, payload ...string) *Node {
	return &Node{Kind: kind, Payload: payload}
}

// Child appends children and returns the node.
func (n *Node) Child(children ...*Node) *Node {
	n.Children = append(n.Children, children...)
	return n
}

// At sets the source position and returns the node.
func (n *Node) At(pos string) *Node {
	n.Pos = pos
	return n
}

// Mark attaches one mark and returns the node.
func (n *Node) Mark(name, arg string) *Node {
	n.Marks = append(n.Marks, Mark{Name: name, Arg: arg})
	return n
}

// Sorted sorts the marks of the node and every descendant by name and
// argument, which the format requires.
func (n *Node) Sorted() *Node {
	sortMarks(n.Marks)
	for _, child := range n.Children {
		child.Sorted()
	}
	return n
}

// sortMarks orders a mark list in place.
func sortMarks(marks []Mark) {
	for i := 1; i < len(marks); i++ {
		for j := i; j > 0 && marks[j-1].String() > marks[j].String(); j-- {
			marks[j-1], marks[j] = marks[j], marks[j-1]
		}
	}
}

// child returns the first child of a kind, or nil.
func (n *Node) child(kind string) *Node {
	for _, c := range n.Children {
		if c.Kind == kind {
			return c
		}
	}
	return nil
}

// children returns every child of a kind.
func (n *Node) children(kind string) []*Node {
	var out []*Node
	for _, c := range n.Children {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

// head renders the kind and payload of a node, without marks or position.
func (n *Node) head() string {
	var b strings.Builder
	b.WriteString(n.Kind)
	for _, token := range n.Payload {
		b.WriteByte(' ')
		b.WriteString(quoteToken(token))
	}
	return b.String()
}

// String renders one line of the node, without children.
func (n *Node) String() string {
	line := n.head()
	if n.Pos != "" {
		line += " pos " + n.Pos
	}
	if len(n.Marks) > 0 {
		parts := make([]string, 0, len(n.Marks))
		for _, m := range n.Marks {
			parts = append(parts, m.String())
		}
		line += " marks {" + strings.Join(parts, ", ") + "}"
	}
	return line
}

// quoteToken renders a payload token, quoting it when it holds a space or a
// character the format reserves.
func quoteToken(token string) string {
	if token == "" {
		return `""`
	}
	if !strings.ContainsAny(token, " \t\"\\\n") {
		return token
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(token); i++ {
		switch c := token[i]; c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// checkKind reports a kind that is not a lowercase ASCII word, which the
// format forbids.
func checkKind(kind string) error {
	if kind == "" {
		return fmt.Errorf("zyxc: a node needs a kind")
	}
	for i := 0; i < len(kind); i++ {
		c := kind[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			continue
		}
		return fmt.Errorf("zyxc: kind %q is not a lowercase word", kind)
	}
	return nil
}
