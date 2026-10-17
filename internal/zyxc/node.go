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
	// Pos is the source position. The builder writes it where the node came
	// from source; the writer fills it for a node the compiler generated, from
	// the node that generated it.
	Pos string
	// From names the node a generated node was derived from, so a reader can
	// walk back to the source in one step. It is empty for a source node.
	From string
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
// String renders the mark: its name, and its argument when it has one. The
// argument is one token, quoted when it holds a space. Name and argument are
// separated by a space rather than a colon, because the mark vocabulary
// itself uses colons, as in `coro:region` and `chan:send`.
func (m Mark) String() string {
	if m.Arg == "" {
		return m.Name
	}
	return m.Name + " " + quoteMarkArg(m.Arg)
}

// New returns a node of a kind with a payload.
func New(kind string, payload ...string) *Node {
	return &Node{Kind: kind, Payload: payload}
}

// Child appends children and returns the node. A nil child is dropped, so
// that a node the builder could not render does not reach the format.
func (n *Node) Child(children ...*Node) *Node {
	for _, c := range children {
		if c != nil {
			n.Children = append(n.Children, c)
		}
	}
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
	if n == nil {
		return nil
	}
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

// First returns the first child of a kind, or nil. It is the accessor the
// backend uses to read a tree it did not build.
func (n *Node) First(kind string) *Node { return n.child(kind) }

// All returns every child of a kind, in order.
func (n *Node) All(kind string) []*Node { return n.children(kind) }

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
	if n.From != "" {
		line += " from " + n.From
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

// quoteToken renders a payload token, quoting it when it holds a space, a
// control byte, or a character the format reserves. Tokens are separated by
// spaces, so a comma is left alone: a type text is one token.
func quoteToken(token string) string { return quote(token, "") }

// quoteMarkArg renders a mark argument. Inside a marks section a comma
// separates one mark from the next, so an argument that holds one is quoted
// even though a payload token with the same text would not be.
func quoteMarkArg(token string) string { return quote(token, ",{}") }

// quote renders a token, quoting it when it is empty or holds a reserved
// character.
func quote(token, reserved string) string {
	if token == "" {
		return `""`
	}
	if !needsQuoting(token, reserved) {
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
		case 0:
			b.WriteString(`\0`)
		default:
			if c < 0x20 {
				// A control byte has no escape of its own, so it is written
				// as the code point it is; a reader that does not know the
				// escape would otherwise keep a byte that breaks the line.
				fmt.Fprintf(&b, `\u{%x}`, c)
				continue
			}
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// needsQuoting reports whether a token is not a bare word: an empty token, a
// byte the format reserves, a control byte, a character that separates one token
// from another in the section the token stands in, or a word the reader takes for
// structure. A name a program wrote may be any word at all — a parameter named
// `from` is a parameter — so a token that reads as a section marker is quoted,
// and the reader then reads it as a payload token rather than as structure.
func needsQuoting(token, reserved string) bool {
	switch token {
	case "pos", "from", "marks":
		return true
	}
	for i := 0; i < len(token); i++ {
		c := token[i]
		if c <= ' ' || c == '"' || c == '\\' || c == 0x7f || strings.IndexByte(reserved, c) >= 0 {
			return true
		}
	}
	return false
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
