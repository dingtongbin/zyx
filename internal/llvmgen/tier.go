// The stack a coroutine starts on. A coroutine that can reach only the runtime's
// own C, and the operations the language itself performs, starts on a small
// stack that commits little and grows as it is used; one that can reach a C
// function of unknown depth starts on the full one. What a tier decides is how
// much a coroutine commits before it runs, not what it can do: a stack that turns
// out to be too small grows. A decision made the wrong way therefore costs
// memory or a little time and nothing else, which is why the analysis below can
// leave a call it cannot follow alone rather than give every coroutine the full
// stack.
//
// The decision is made here, over the whole program, because a call crosses
// packages and a coroutine may reach a library through a function it never
// names.

package llvmgen

import (
	"strings"

	"zyx/internal/zyxc"
)

// The tiers, in the order the runtime numbers them.
const (
	stackPacked = iota
	stackFull
)

// runtimeOwnsCFunction reports whether a C symbol is one the runtime defines. The
// C a program written against the standard library reaches is the runtime's own
// entries, whose frames are written to be small; every other C symbol belongs to
// a library the program brought, whose depth nothing here knows.
func runtimeOwnsCFunction(symbol string) bool {
	return strings.HasPrefix(symbol, "zyxrt_")
}

// bodies indexes every function the program holds by its symbol, so that the
// analysis can follow a call into the function it names, wherever it is defined.
func (g *generator) bodies() map[string]*zyxc.Node {
	if g.bodyIndex != nil {
		return g.bodyIndex
	}
	index := map[string]*zyxc.Node{}
	for _, path := range g.prog.Order {
		file := g.prog.Files[path]
		if file == nil {
			continue
		}
		for _, decl := range file.Decls {
			collectBodies(decl, index)
		}
	}
	g.bodyIndex = index
	return index
}

// collectBodies adds the functions of a declaration, and of the declarations it
// nests, to the index.
func collectBodies(n *zyxc.Node, index map[string]*zyxc.Node) {
	if n == nil {
		return
	}
	if n.Kind == "func" && len(n.Payload) >= 3 {
		index[n.Payload[2]] = n
	}
	for _, child := range n.Children {
		collectBodies(child, index)
	}
}

// stackTier answers the stack the coroutine a spawn starts should be given.
func (g *generator) stackTier(symbol string) int {
	if symbol != "" && g.reachesUnknownC(symbol, map[string]bool{}) {
		return stackFull
	}
	return stackPacked
}

// reachesUnknownC reports whether the function a symbol names can reach, through
// the functions it calls, a C function of unknown depth. A call that leaves the
// program through a value is not followed: what it ends up being is not known
// here, and a coroutine given too small a stack grows.
func (g *generator) reachesUnknownC(symbol string, seen map[string]bool) bool {
	var body *zyxc.Node
	var deep bool
	if symbol == "" {
		return false
	}
	if g.deepC == nil {
		g.deepC = map[string]bool{}
	}
	if known, ok := g.deepC[symbol]; ok {
		return known
	}
	if seen[symbol] {
		// A cycle of calls on its own is not depth.
		return false
	}
	body = g.bodies()[symbol]
	if body == nil {
		// A function the program does not hold is one from outside it, and how
		// deep it goes is not known here.
		return true
	}
	seen[symbol] = true
	deep = g.nodeReachesUnknownC(body, seen)
	delete(seen, symbol)
	g.deepC[symbol] = deep
	return deep
}

// nodeReachesUnknownC walks one body for the calls that decide the tier. A
// coroutine start within the body is left alone, because the function it names
// runs on a stack of its own, which the spawn site gives it.
func (g *generator) nodeReachesUnknownC(n *zyxc.Node, seen map[string]bool) bool {
	deep := false
	switch n.Kind {
	case "spawn":
		return false
	case "c-call":
		if len(n.Payload) > 0 && !runtimeOwnsCFunction(n.Payload[0]) {
			deep = true
		}
	case "call":
		symbol := g.calleeSymbol(g.wrapped(n, "callee"))
		if symbol != "" {
			if g.cABIFunc(symbol) {
				deep = !runtimeOwnsCFunction(g.linkName(symbol))
			} else {
				deep = g.reachesUnknownC(symbol, seen)
			}
		}
	}
	if deep {
		return true
	}
	for _, child := range n.Children {
		if g.nodeReachesUnknownC(child, seen) {
			return true
		}
	}
	return false
}
