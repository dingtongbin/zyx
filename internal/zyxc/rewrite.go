// Rewriting the intermediate representation in place. The monomorphizer
// substitutes type arguments through these walks, and every later stage
// reads the rewritten tree.

package zyxc

import "strings"

// typeSlots gives, for a node kind, the payload positions that hold type
// text.
var typeSlots = map[string][]int{
	"new":         {0},
	"convert":     {0},
	"struct-lit":  {0},
	"map-lit":     {0},
	"set-lit":     {0},
	"variant-lit": {0},
	"is":          {0},
	"impl":        {0},
	"callee-type": {0},
}

// symbolSlots gives, for a node kind, the payload position that holds the
// symbol id of a reference.
var symbolSlots = map[string]int{
	"name":         1,
	"qual":         2,
	"mcall":        1,
	"field":        1,
	"func-ref":     0,
	"method-value": 1,
}

// EachType applies f to every type text of a subtree and writes the result
// back. f returns the replacement text; the empty string leaves the text
// alone.
func EachType(n *Node, f func(text string) string) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "type", "result":
		rewriteSlot(n, 0, f)
	case "field":
		// A field of a type body carries its type; a field access carries
		// the symbol id of the member it reaches.
		if n.First("recv") == nil {
			rewriteSlot(n, 1, f)
		}
	case "param":
		// A type parameter carries a symbol id rather than a type; its
		// constraints are type texts.
		if isTypeParam(n) {
			for i := 2; i < len(n.Payload); i++ {
				rewriteSlot(n, i, f)
			}
		} else {
			rewriteSlot(n, 1, f)
		}
	default:
		for _, slot := range typeSlots[n.Kind] {
			rewriteSlot(n, slot, f)
		}
	}
	for _, child := range n.Children {
		EachType(child, f)
	}
}

// rewriteSlot replaces one payload token when f returns a replacement.
func rewriteSlot(n *Node, slot int, f func(string) string) {
	if slot >= len(n.Payload) {
		return
	}
	if text := f(n.Payload[slot]); text != "" {
		n.Payload[slot] = text
	}
}

// SymbolSlot returns the payload position of a node kind that holds the
// symbol id of a reference, and whether the kind carries one.
func SymbolSlot(kind string) (int, bool) {
	slot, ok := symbolSlots[kind]
	return slot, ok
}

// EachSymbol applies f to every symbol id a subtree references and writes
// the result back. f returns the replacement id; the empty string leaves
// the id alone.
func EachSymbol(n *Node, f func(id string) string) {
	if n == nil {
		return
	}
	if slot, ok := symbolSlots[n.Kind]; ok && slot < len(n.Payload) {
		if id := f(n.Payload[slot]); id != "" {
			n.Payload[slot] = id
		}
	}
	if n.Kind == "func" && len(n.Payload) >= 3 {
		if id := f(n.Payload[2]); id != "" {
			n.Payload[2] = id
		}
	}
	for _, child := range n.Children {
		EachSymbol(child, f)
	}
}

// EachDecl visits every declaration node of a tree, including the members
// of a type body.
func EachDecl(n *Node, f func(*Node)) {
	if n == nil {
		return
	}
	f(n)
	for _, child := range n.Children {
		EachDecl(child, f)
	}
}

// isTypeParam reports whether a `param` node declares a generic parameter.
func isTypeParam(n *Node) bool {
	return len(n.Payload) >= 2 && strings.Contains(n.Payload[1], "$")
}
