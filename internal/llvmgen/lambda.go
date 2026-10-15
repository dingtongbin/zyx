// Lambdas: an anonymous function that captures nothing, so its value is the
// address of the function itself.

package llvmgen

import (
	"fmt"

	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// lambda lowers a lambda into a function of its own and yields its address.
func (g *generator) lambda(module string, n *zyxc.Node) *value {
	sym := ""
	if len(n.Payload) > 0 {
		sym = n.Payload[0]
	}
	if sym == "" {
		g.fail(fmt.Errorf("llvmgen: a lambda has no symbol"))
		return nil
	}
	typ := g.typeOfNode(n.First("type"), module)
	if typ == nil || typ.Kind != types.KindFunc {
		g.fail(fmt.Errorf("llvmgen: the type of the lambda is not settled"))
		return nil
	}
	// A lambda is an ordinary function under a symbol of its own. Its type
	// annotation is not part of the function's shape, so it is left behind.
	fn := zyxc.New("func", "lambda", "plain", sym)
	for _, child := range n.Children {
		if child.Kind != "type" {
			fn.Child(child)
		}
	}
	// The lambda is emitted while the enclosing function is being emitted,
	// so its state is put back afterwards.
	saved := g.fn
	g.function(module, fn)
	g.fn = saved
	return &value{text: "@" + llvmName(sym), ll: "ptr", typ: typ}
}
