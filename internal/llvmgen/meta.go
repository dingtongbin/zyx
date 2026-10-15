// The compile-time namespaces that reach the backend: the size of an
// instantiated type, and a duration built from a unit and a count. Every
// other query is a compile-time value the front end already settled.

package llvmgen

import (
	"fmt"
	"strconv"

	"zyx/internal/layout"
	"zyx/internal/types"
	"zyx/internal/zyxc"
)

// metaCall lowers a query into the meta namespace.
func (g *generator) metaCall(module string, n *zyxc.Node) *value {
	name := ""
	if len(n.Payload) > 0 {
		name = n.Payload[0]
	}
	if name != "size_of" {
		g.fail(fmt.Errorf("llvmgen: meta.%s is not lowered", name))
		return nil
	}
	typ, err := g.queryType(module, n)
	if err != nil {
		g.fail(err)
		return nil
	}
	size, err := layout.Size(typ, g.tab, g.target.Layout())
	if err != nil {
		g.fail(fmt.Errorf("llvmgen: the size of %s: %w", typ, err))
		return nil
	}
	return &value{text: strconv.FormatUint(size, 10), ll: g.ptrInt(), typ: types.Usize}
}

// queryType returns the type a query names, which its argument writes as a
// type name.
func (g *generator) queryType(module string, n *zyxc.Node) (*types.Type, error) {
	arg := g.sole(n.First("arg"))
	if arg == nil {
		return nil, fmt.Errorf("llvmgen: the query has no argument")
	}
	text, err := typeTextOf(arg)
	if err != nil {
		return nil, err
	}
	return g.tab.parse(module, text)
}

// typeTextOf returns the type text a node writes. A query names its type the
// way source writes it, so the text is the payload of a name or of a
// qualifier.
func typeTextOf(n *zyxc.Node) (string, error) {
	switch n.Kind {
	case "name":
		if len(n.Payload) < 1 {
			return "", fmt.Errorf("llvmgen: a type name needs a spelling")
		}
		return n.Payload[0], nil
	case "qual":
		if len(n.Payload) < 2 {
			return "", fmt.Errorf("llvmgen: a qualified type needs two names")
		}
		return n.Payload[0] + "." + n.Payload[1], nil
	}
	return "", fmt.Errorf("llvmgen: %s does not name a type", n.Kind)
}

// durCall lowers a duration constructor. A Duration is a count of
// nanoseconds, so the unit settles the scale.
func (g *generator) durCall(module string, n *zyxc.Node) *value {
	unit := ""
	if len(n.Payload) > 0 {
		unit = n.Payload[0]
	}
	scale, ok := durationUnits[unit]
	if !ok {
		g.fail(fmt.Errorf("llvmgen: dur.%s is not a unit", unit))
		return nil
	}
	count := g.expr(module, g.sole(n.First("arg")), types.Uint64)
	if count == nil {
		return nil
	}
	count = g.load(count)
	reg := g.newReg()
	g.emit("  %s = mul %s %s, %d", reg, count.ll, count.text, scale)
	return &value{text: reg, ll: count.ll, typ: types.Duration}
}

// durationUnits maps a unit name onto the number of nanoseconds it holds.
var durationUnits = map[string]uint64{
	"ms":  1_000_000,
	"us":  1_000,
	"sec": 1_000_000_000,
}
